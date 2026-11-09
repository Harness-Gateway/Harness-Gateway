package process

import (
	"errors"

	"golang.org/x/sys/unix"
)

func waitForExit(pid int) error {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func zombieOnlyGroup(int) bool { return false }
