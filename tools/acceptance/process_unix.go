//go:build unix

package main

import (
	"os"
	"syscall"
	"time"

	"github.com/harness-gateway/harness-gateway/internal/process"
)

func stopCommand(child *process.Child) error {
	// Java-owned gateways share the command group, while their CLIs have their
	// own groups. Keep the leader unreaped through TERM grace and final cleanup.
	return child.StopGracefully(syscall.SIGTERM, 6*time.Second)
}

func waitCommandExit(child *process.Child) error {
	// The leader can exit independently while Java-owned gateways still need
	// to retire separately grouped CLIs. Keep ownership through that grace too.
	return child.WaitGracefully(syscall.SIGTERM, 6*time.Second)
}

func interruptGateway(child *process.Child) error { return child.Signal(os.Interrupt) }
