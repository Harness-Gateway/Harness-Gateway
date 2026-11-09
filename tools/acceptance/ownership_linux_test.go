package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCancellationRetainsExitedLeaderThroughGrace(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command("/bin/sh", "-c", `trap 'exit 0' TERM; : > "$1"; while :; do sleep 0.02; done`, "sh", ready)
	child, err := startCommand(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- waitCommand(ctx, child) }()
	t.Cleanup(func() { cancel(); _ = child.Stop(); _ = child.Wait() })
	waitAcceptance(t, func() bool { _, err := os.Stat(ready); return err == nil })
	cancel()
	// Observe exit without reaping. The command's Wait must keep this exited
	// leader owned until every cancellation group signal has finished.
	exited := make(chan error, 1)
	go func() {
		var info unix.Siginfo
		exited <- unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
	}()
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("leader reaped during TERM grace: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TERM did not reach the owned leader")
	}
	select {
	case err := <-done:
		t.Fatalf("grace ended before cancellation cleanup: %v", err)
	default:
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(9 * time.Second):
		t.Fatal("graceful cancellation exceeded its bound")
	}
	if err := child.Signal(syscall.SIGTERM); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("late group signal accepted: %v", err)
	}
}
