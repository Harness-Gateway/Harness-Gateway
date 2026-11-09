package process

import (
	"errors"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Darwin waitid can report a stopped child even with WEXITED. Observe NOTE_EXIT
// instead, without consuming wait status or polling throughout an inference.
const darwinZombie = 5

func waitForExit(pid int) error {
	syscall.ForkLock.RLock()
	queue, err := unix.Kqueue()
	if err == nil {
		unix.CloseOnExec(queue)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return err
	}
	defer unix.Close(queue)
	change := unix.Kevent_t{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT}
	for {
		_, err = unix.Kevent(queue, []unix.Kevent_t{change}, nil, nil)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	// NOTE_EXIT is edge-triggered. An already exited child may precede filter
	// attachment; check its unreaped state after registration to cover that gap.
	info, stateErr := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if errors.Is(stateErr, unix.EIO) || errors.Is(stateErr, unix.ESRCH) {
		return syscall.ECHILD
	}
	if stateErr != nil {
		return stateErr
	}
	if info.Proc.P_stat == darwinZombie {
		return nil
	}
	if err != nil {
		return err
	}
	events := make([]unix.Kevent_t, 1)
	for {
		n, err := unix.Kevent(queue, nil, events, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n == 1 && events[0].Flags&unix.EV_ERROR != 0 {
			return syscall.Errno(events[0].Data)
		}
		if n == 1 && events[0].Fflags&unix.NOTE_EXIT != 0 {
			// NOTE_EXIT can precede the final zombie transition. Wait for it
			// before classifying Darwin's zombie-only killpg EPERM response.
			for {
				info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
				if errors.Is(err, unix.EIO) || errors.Is(err, unix.ESRCH) {
					return syscall.ECHILD
				}
				if err != nil {
					return err
				}
				if info.Proc.P_stat == darwinZombie {
					return nil
				}
				time.Sleep(time.Millisecond)
			}
		}
	}
}

// Darwin killpg may return EPERM when every remaining member is a zombie.
// The leader remains unreaped throughout this inspection, preventing reuse.
func zombieOnlyGroup(pid int) bool {
	members, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pid)
	if err != nil || len(members) == 0 {
		return false
	}
	for _, member := range members {
		if member.Proc.P_stat != darwinZombie {
			return false
		}
	}
	return true
}
