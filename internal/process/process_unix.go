//go:build linux || darwin

package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func signal(cmd *exec.Cmd, sig os.Signal) error {
	value, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("unsupported process-group signal %T", sig)
	}
	err := syscall.Kill(-cmd.Process.Pid, value)
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM) && zombieOnlyGroup(cmd.Process.Pid) {
		return os.ErrProcessDone
	}
	return err
}

func start(cmd *exec.Cmd) (func() error, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pid := cmd.Process.Pid
	return func() error {
		err := syscall.Kill(-pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if errors.Is(err, syscall.EPERM) && zombieOnlyGroup(pid) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("terminate CLI process group %d: %w", pid, err)
		}
		return nil
	}, nil
}

func wait(cmd *exec.Cmd, child *Child, cleanup func() error) error {
	// Observe exit without reaping: the leader's PID still reserves the group
	// identity when Stop signals it, even when no live members remain.
	exitErr := waitForExit(cmd.Process.Pid)
	if errors.Is(exitErr, syscall.ECHILD) {
		child.lost.Store(true)
		return errors.Join(errOwnershipLost, exitErr, cmd.Wait())
	}
	stopErr := cleanup()
	return errors.Join(exitErr, stopErr, cmd.Wait())
}
