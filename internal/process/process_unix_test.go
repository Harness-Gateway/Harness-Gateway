//go:build unix

package process

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/harness-gateway/harness-gateway/internal/testutil"
)

func TestUnixContainmentFixture(t *testing.T) {
	mode := os.Getenv("HARNESS_GATEWAY_UNIX_TREE_FIXTURE")
	if mode == "" {
		return
	}
	if mode == "child" {
		marker := os.Getenv("HARNESS_GATEWAY_UNIX_TREE_PID")
		if err := os.WriteFile(marker+".tmp", []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(marker+".tmp", marker); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Minute)
		return
	}
	child := exec.Command(os.Args[0], "-test.run=^TestUnixContainmentFixture$")
	child.Env = append(os.Environ(), "HARNESS_GATEWAY_UNIX_TREE_FIXTURE=child")
	if mode == "group" {
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	} else if mode == "session" {
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	if err := child.Run(); err != nil {
		t.Fatal(err)
	}
}

func TestUnixCleanupContainmentBoundary(t *testing.T) {
	for _, mode := range []string{"inherit", "group", "session"} {
		t.Run(mode, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "child-pid")
			cmd := exec.Command(os.Args[0], "-test.run=^TestUnixContainmentFixture$")
			cmd.Env = append(os.Environ(), "HARNESS_GATEWAY_UNIX_TREE_FIXTURE="+mode,
				"HARNESS_GATEWAY_UNIX_TREE_PID="+marker, "GORACE=atexit_sleep_ms=0")
			owned, err := Start(cmd)
			if err != nil {
				t.Fatal(err)
			}
			stop := owned.Stop
			done := make(chan struct{})
			go func() { _ = owned.Wait(); close(done) }()
			t.Cleanup(func() { _ = stop(); <-done })
			var child int
			waitUnixProcess(t, func() bool {
				data, _ := os.ReadFile(marker)
				child, _ = strconv.Atoi(string(data))
				return child > 0
			})
			// Escaped fixtures are deliberately outside Start's guarantee. This
			// test owns their identities and explicitly retires them afterward.
			t.Cleanup(func() {
				if running, _ := testutil.ProcessRunning(child); running {
					if err := stopUnixFixture(child); err != nil {
						t.Errorf("retire detached fixture: %v", err)
					}
				}
				waitUnixProcess(t, func() bool { running, err := testutil.ProcessRunning(child); return err == nil && !running })
			})
			group, err := syscall.Getpgid(child)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "inherit" && group != cmd.Process.Pid || mode != "inherit" && group != child {
				t.Fatalf("wrong fixture group: mode=%s child=%d group=%d owner=%d", mode, child, group, cmd.Process.Pid)
			}
			if err := stop(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("owned group leader was not reaped")
			}
			if mode == "inherit" {
				waitUnixProcess(t, func() bool { running, err := testutil.ProcessRunning(child); return err == nil && !running })
			} else if running, err := testutil.ProcessRunning(child); err != nil || !running {
				t.Fatalf("group cleanup crossed its containment boundary: running=%t error=%v", running, err)
			}
			if err := stop(); err != nil {
				t.Fatalf("repeated cleanup changed its result: %v", err)
			}
		})
	}
}

func stopUnixFixture(pid int) error {
	err := syscall.Kill(pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil // The child can exit after the liveness check.
	}
	return err
}

func TestUnixFixtureCleanupAllowsAlreadyExitedChild(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := stopUnixFixture(cmd.Process.Pid); err != nil {
		t.Fatalf("already reaped fixture was reported as cleanup failure: %v", err)
	}
}

func waitUnixProcess(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("owned fixture process condition did not arrive")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
