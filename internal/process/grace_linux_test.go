package process

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestGracefulWaitReservesExitedLeaderAndPreservesStatus(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 7")
	child, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	signaled, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); _ = child.Stop(); _ = child.Wait() })
	if child.WaitGracefully(nil, 0) == nil || child.WaitGracefully(syscall.SIGTERM, -1) == nil {
		t.Fatal("invalid wait policy accepted")
	}
	original := child.signal
	child.signal = func(sig os.Signal) error {
		err := original(sig)
		close(signaled)
		<-release
		return err
	}
	done := make(chan error, 1)
	go func() { done <- child.WaitGracefully(syscall.SIGTERM, 20*time.Millisecond) }()
	select {
	case <-signaled:
	case <-time.After(3 * time.Second):
		t.Fatal("exit did not enter graceful cleanup")
	}
	var info unix.Siginfo
	if err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT|unix.WNOHANG, nil); err != nil {
		t.Fatalf("leader reaped before exit cleanup: %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("wait skipped pending exit cleanup: %v", err)
	default:
	}
	unblock()
	err = <-done
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("lost exit status: %v", err)
	}
	if child.Wait() != err {
		t.Fatal("wait did not share the original result")
	}
	if err := child.Signal(syscall.SIGTERM); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("late signal accepted: %v", err)
	}
}

func TestGracefulStopReservesIdentityThroughPendingSignals(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command("/bin/sh", "-c", `trap 'exit 0' TERM; : > "$1"; while :; do sleep 0.02; done`, "sh", ready)
	child, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	signaled, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); _ = child.Stop(); _ = child.Wait() })
	waitUnixProcess(t, func() bool { _, err := os.Stat(ready); return err == nil })
	original := child.signal
	child.signal = func(sig os.Signal) error { err := original(sig); close(signaled); <-release; return err }
	waitDone, stopDone := make(chan error, 1), make(chan error, 1)
	go func() { waitDone <- child.Wait() }()
	go func() { stopDone <- child.StopGracefully(syscall.SIGTERM, time.Millisecond) }()
	select {
	case <-signaled:
	case <-time.After(5 * time.Second):
		t.Fatal("graceful signal not issued")
	}
	exited := make(chan error, 1)
	go func() {
		var info unix.Siginfo
		exited <- unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
	}()
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("released leader before pending cleanup: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TERM did not exit the leader")
	}
	select {
	case err := <-waitDone:
		t.Fatalf("Wait reaped during cleanup: %v", err)
	default:
	}
	unblock()
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if err := <-waitDone; err != nil {
		t.Fatal(err)
	}
	// Model a different group behind the now-reusable number. Public calls must
	// reject or reuse the cached cleanup result without reaching this signaler.
	foreignSignal := false
	child.signal = func(os.Signal) error { foreignSignal = true; return nil }
	if err := child.Signal(syscall.SIGTERM); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("late signal: %v", err)
	}
	if err := child.StopGracefully(syscall.SIGTERM, 0); err != nil {
		t.Fatal(err)
	}
	if err := child.Stop(); err != nil {
		t.Fatal(err)
	}
	if foreignSignal {
		t.Fatal("signaled a released process-group identity")
	}
}

func TestGracefulStopDoesNotSignalAfterObservedOwnershipLoss(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	child, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); _ = cmd.Process.Kill(); _ = child.Wait() })
	child.signal = func(os.Signal) error { close(entered); <-release; return nil }
	terminations := 0
	child.terminate = func() error { terminations++; return nil }
	done := make(chan error, 1)
	go func() { done <- child.StopGracefully(syscall.SIGTERM, 0) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("graceful signal did not begin")
	}
	child.lost.Store(true) // Model an external reaper detected during cancellation.
	unblock()
	if err := <-done; !errors.Is(err, errOwnershipLost) {
		t.Fatalf("ownership loss ignored: %v", err)
	}
	if terminations != 0 {
		t.Fatal("forced a signal after known ownership loss")
	}
}
