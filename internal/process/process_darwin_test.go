package process

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

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
