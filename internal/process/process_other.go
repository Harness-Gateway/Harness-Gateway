//go:build !linux && !darwin && !windows

package process

import (
	"errors"
	"os"
	"os/exec"
)

func signal(*exec.Cmd, os.Signal) error { return errors.New("unsupported process ownership platform") }

func start(cmd *exec.Cmd) (func() error, error) {
	return nil, errors.New("CLI process-tree ownership requires Linux, macOS or Windows")
}

func wait(*exec.Cmd, *Child, func() error) error {
	return errors.New("CLI process-tree ownership requires Linux, macOS or Windows")
}
