package process

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestZombieGroupConfirmationWaitsForExitTransition(t *testing.T) {
	member := func(state int8) unix.KinfoProc {
		var info unix.KinfoProc
		info.Proc.P_stat = state
		return info
	}
	for _, empty := range []bool{false, true} {
		calls := 0
		confirmed := waitForZombieGroup(func() ([]unix.KinfoProc, error) {
			calls++
			if calls < 3 {
				if empty {
					return nil, nil
				}
				return []unix.KinfoProc{member(2)}, nil
			}
			return []unix.KinfoProc{member(darwinZombie)}, nil
		}, time.Second)
		if !confirmed || calls != 3 {
			t.Fatalf("missed exit transition: empty=%t confirmed=%t calls=%d", empty, confirmed, calls)
		}
	}
	for _, states := range [][]unix.KinfoProc{nil, {member(2)}, {member(darwinZombie), member(2)}} {
		if waitForZombieGroup(func() ([]unix.KinfoProc, error) { return states, nil }, 2*time.Millisecond) {
			t.Fatal("unconfirmed or live group treated as zombie-only")
		}
	}
	if waitForZombieGroup(func() ([]unix.KinfoProc, error) { return []unix.KinfoProc{member(darwinZombie)}, syscall.EPERM }, time.Second) {
		t.Fatal("inspection failure was ignored")
	}
}

func TestStopRacingNaturalExitKeepsOwnedIdentity(t *testing.T) {
	for i := 0; i < 50; i++ {
		cmd := exec.Command("/bin/sh", "-c", "read line; exit 0")
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		child, err := Start(cmd)
		if err != nil {
			_ = stdin.Close()
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- child.Wait() }()
		_ = stdin.Close()
		// Let EOF-driven exit and explicit cleanup reach either side of the
		// kernel's transition, retaining the leader until all cleanup finishes.
		time.Sleep(time.Duration(i%4) * time.Millisecond)
		stopErr := child.Stop()
		waitErr := <-done // SIGKILL and natural exit are both valid outcomes.
		if stopErr != nil {
			t.Fatalf("iteration %d: exit cleanup failed: %v", i, stopErr)
		}
		var exit *exec.ExitError
		if waitErr != nil && !errors.As(waitErr, &exit) {
			t.Fatalf("iteration %d: unexpected wait failure: %v", i, waitErr)
		}
	}
}

func TestStopUnreapedZombieOnlyGroup(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	child, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Stop(); _ = child.Wait() }()
	if err := waitForExit(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("expected Darwin zombie-group EPERM, got %v", err)
	}
	if err := child.Stop(); err != nil {
		t.Fatalf("unreaped zombie group cleanup: %v", err)
	}
	if cmd.ProcessState != nil {
		t.Fatal("Stop reaped the leader before Wait")
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := child.Stop(); err != nil {
		t.Fatalf("repeated cleanup: %v", err)
	}
}

func TestStoppedChildIsNotTreatedAsExited(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "kill -STOP $$; exit 0")
	child, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	defer func() { _ = child.Stop(); _ = child.Wait() }()
	select {
	case err := <-done:
		t.Fatalf("stopped child was reaped: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := child.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("kqueue did not observe stopped-child termination")
	}
}
