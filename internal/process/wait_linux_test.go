package process

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestWaitPreservesKernelIdentityUntilGroupTermination(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	child, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	stopping, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); _ = child.Stop(); _ = child.Wait() }()
	stop := child.stop
	child.stop = sync.OnceValue(func() error {
		close(stopping)
		<-release
		return stop()
	})
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case <-stopping:
	case <-time.After(5 * time.Second):
		t.Fatal("exit did not reach group termination")
	}
	// A successful WNOWAIT proves the exited child's kernel PID is still
	// reserved at the signal boundary, even with arbitrarily delayed cleanup.
	var info unix.Siginfo
	if err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil); err != nil {
		t.Fatalf("leader identity released before termination: %v", err)
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil); !errors.Is(err, unix.ECHILD) {
		t.Fatalf("Wait did not reap its child: %v", err)
	}
	if err := child.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestExternallyReapedChildDoesNotSignalReusedIdentity(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	child, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	// Deliberate owner-contract violation.
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	signals := 0
	child.stop = func() error { signals++; return nil }
	child.signal = func(os.Signal) error { signals++; return nil }
	if err := child.Wait(); !errors.Is(err, errOwnershipLost) {
		t.Fatalf("lost identity accepted: %v", err)
	}
	if err := child.Stop(); !errors.Is(err, errOwnershipLost) {
		t.Fatalf("late stop accepted: %v", err)
	}
	if err := child.Signal(unix.SIGTERM); !errors.Is(err, errOwnershipLost) {
		t.Fatalf("late signal accepted: %v", err)
	}
	if signals != 0 {
		t.Fatal("signaled a numeric identity after external reaping")
	}
}
