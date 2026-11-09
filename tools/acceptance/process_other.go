//go:build !unix

package main

import "github.com/harness-gateway/harness-gateway/internal/process"

func stopCommand(child *process.Child) error      { return child.Stop() }
func waitCommandExit(child *process.Child) error  { return child.Wait() }
func interruptGateway(child *process.Child) error { return child.Stop() }
