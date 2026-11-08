//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	if cmd.Cancel != nil {
		cmd.Cancel = func() error {
			// Java-owned gateways share this group, while their CLIs have their
			// own groups. Let gateways cancel requests and reap those CLIs before
			// forcibly retiring an unresponsive Maven/Java process tree.
			if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
				if errors.Is(err, syscall.ESRCH) {
					return os.ErrProcessDone
				}
				return err
			}
			deadline := time.NewTimer(6 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(25 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if errors.Is(syscall.Kill(-cmd.Process.Pid, 0), syscall.ESRCH) {
						return nil
					}
				case <-deadline.C:
					err := killCommand(cmd)
					if errors.Is(err, os.ErrProcessDone) {
						return nil
					}
					return err
				}
			}
		}
	}
}

func killCommand(cmd *exec.Cmd) error {
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
