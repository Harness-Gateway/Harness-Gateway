// Package process starts CLI processes with platform-specific containment.
package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Child owns termination and reaping of one command. Do not call Cmd.Wait or
// Process.Wait separately: Unix group identity must remain reserved until Stop.
type Child struct {
	stop      func() error
	wait      func(os.Signal, time.Duration) error
	lost      atomic.Bool
	mu        sync.Mutex
	retired   bool
	signal    func(os.Signal) error
	terminate func() error
	graceful  func(os.Signal, time.Duration) error
}

var errOwnershipLost = errors.New("CLI child was reaped outside its owner; refusing to signal its process group")

// Stop is idempotent and concurrency-safe, including after Wait. Concurrent
// cleanup calls join the first request, including its chosen grace period.
func (c *Child) Stop() error {
	if c.lost.Load() {
		return errOwnershipLost
	}
	return c.stop()
}

// Wait observes exit, retires owned descendants, and reaps the command. It must
// be called even when Stop was already called; repeated calls share one result.
func (c *Child) Wait() error { return c.wait(nil, 0) }

// WaitGracefully observes direct-child exit, then gives remaining descendants a
// bounded grace before forced cleanup and reaping. Concurrent Wait calls share
// the first caller's policy and result. Stop still requests immediate cleanup.
func (c *Child) WaitGracefully(sig os.Signal, grace time.Duration) error {
	if sig == nil || grace < 0 {
		return errors.New("graceful wait requires a signal and nonnegative grace")
	}
	return c.wait(sig, grace)
}

// Signal sends a platform-supported signal (Unix: the owned group) while its
// identity is reserved. After termination it returns ErrProcessDone instead.
func (c *Child) Signal(sig os.Signal) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lost.Load() {
		return errOwnershipLost
	}
	if c.retired {
		return os.ErrProcessDone
	}
	return c.signal(sig)
}

// StopGracefully signals the command (Unix: its group), allows descendants time
// to clean up, then terminates the tree. Wait cannot reap during cleanup.
// The caller must still call Wait. Ordinary Stop calls do not request a grace.
func (c *Child) StopGracefully(sig os.Signal, grace time.Duration) error {
	if grace < 0 || sig == nil {
		return errors.New("graceful stop requires a signal and nonnegative grace")
	}
	if c.lost.Load() {
		return errOwnershipLost
	}
	return c.graceful(sig, grace)
}

// Start starts cmd and returns its lifecycle owner.
// Unix cleanup kills the owned process group. Descendants that create another
// process group or session are outside that guarantee; use service/cgroup
// supervision when those descendants must also be contained. Windows cleanup
// closes an inherited Job Object, including descendants in that job.
// The caller must use Child.Wait to reap cmd. Stop may run concurrently with it.
// Pipe ownership, protocol handling and context policy remain with the caller.
func Start(cmd *exec.Cmd) (*Child, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	if strings.EqualFold(filepath.Ext(cmd.Path), ".js") {
		// JavaScript entry points need the same explicit interpreter for CLI
		// operations and version probes, including on Windows without shebangs.
		node, err := exec.LookPath("node")
		if err != nil {
			return nil, fmt.Errorf("find Node.js for CLI entry point: %w", err)
		}
		var args []string
		if len(cmd.Args) > 0 {
			args = cmd.Args[1:]
		}
		cmd.Args = append([]string{node, cmd.Path}, args...)
		cmd.Path = node
	}
	stop, err := start(cmd)
	if err != nil {
		return nil, err
	}
	child := &Child{signal: func(sig os.Signal) error { return signal(cmd, sig) }, terminate: stop}
	var once sync.Once
	var cleanupErr error
	finish := func(sig os.Signal, grace time.Duration) error {
		once.Do(func() {
			// Serialize every signal with final termination. A late caller must
			// never signal a numeric group after Wait releases its identity.
			child.mu.Lock()
			defer child.mu.Unlock()
			if child.lost.Load() {
				cleanupErr = errOwnershipLost
				return
			}
			if sig != nil {
				err := child.signal(sig)
				if err == nil && grace > 0 {
					timer := time.NewTimer(grace)
					<-timer.C
				} else if !errors.Is(err, os.ErrProcessDone) {
					cleanupErr = err
				}
			}
			// Wait can detect an external reaper while grace is pending. Never
			// issue a final group signal after that loss has been observed.
			if child.lost.Load() {
				cleanupErr = errors.Join(cleanupErr, errOwnershipLost)
			} else {
				cleanupErr = errors.Join(cleanupErr, child.terminate())
			}
			child.retired = true
		})
		return cleanupErr
	}
	child.stop = func() error { return finish(nil, 0) }
	child.graceful = finish
	var waitOnce sync.Once
	var waitErr error
	child.wait = func(sig os.Signal, grace time.Duration) error {
		waitOnce.Do(func() {
			cleanup := child.Stop
			if sig != nil {
				cleanup = func() error { return child.StopGracefully(sig, grace) }
			}
			waitErr = wait(cmd, child, cleanup)
		})
		return waitErr
	}
	return child, nil
}
