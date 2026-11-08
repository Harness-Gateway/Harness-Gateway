//go:build unix

package main

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/harness-gateway/harness-gateway/internal/testutil"
)

// This owner stands in for Java's ProcessBuilder: the gateway inherits its
// command's group, and the real gateway starts each CLI in a separate group.
func TestAcceptanceGatewayOwner(t *testing.T) {
	mode := os.Getenv("HARNESS_GATEWAY_ACCEPTANCE_OWNER")
	if mode == "" {
		return
	}
	if mode == "ignore-term" {
		signal.Ignore(syscall.SIGTERM)
	} else {
		cmd := exec.Command(os.Getenv("ACCEPTANCE_GATEWAY"), "--listen", "127.0.0.1:0", "--harnesses", mode,
			"--"+mode+"-path", os.Getenv("ACCEPTANCE_CLI"))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(os.Getenv("ACCEPTANCE_OWNER_READY"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Minute)
}

func TestInterruptedCommandStopsGatedCLIs(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	gateway := filepath.Join(directory, "gateway")
	build := func(t *testing.T, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "go", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build acceptance fixture: %v\n%s", err, output)
		}
	}
	build(t, "build", "-o", gateway, "./cmd/harness-gateway")
	for _, harness := range []string{"codex", "copilot"} {
		t.Run(harness, func(t *testing.T) {
			cli := filepath.Join(directory, harness+"-fixture")
			build(t, "test", "-c", "-o", cli, "./internal/adapters/outbound/"+harness)
			work := t.TempDir()
			pids := filepath.Join(work, "pids")
			if err := os.Mkdir(pids, 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			log, err := os.Create(filepath.Join(work, "owner.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAcceptanceGatewayOwner$")
			cmd.Env = append(os.Environ(), "HARNESS_GATEWAY_ACCEPTANCE_OWNER="+harness,
				"ACCEPTANCE_GATEWAY="+gateway, "ACCEPTANCE_CLI="+cli, "ACCEPTANCE_OWNER_READY="+filepath.Join(work, "ready"),
				"HARNESS_GATEWAY_CODEX_TEST=catalog-gate", "HARNESS_GATEWAY_COPILOT_TEST=catalog-gate",
				"HARNESS_GATEWAY_COPILOT_PID="+filepath.Join(work, "copilot-pid"), "HARNESS_GATEWAY_COPILOT_LOG="+filepath.Join(work, "methods"),
				"HARNESS_GATEWAY_ACCEPTANCE_PIDS="+pids, "HARNESS_GATEWAY_ACCEPTANCE_CATALOG="+filepath.Join(work, "catalog"),
				"HARNESS_GATEWAY_ACCEPTANCE_CATALOG_RELEASE="+filepath.Join(work, "release"))
			cmd.Stdout, cmd.Stderr = log, log
			configureCommand(cmd)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { _ = cmd.Wait(); close(done) }()
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(9 * time.Second):
					_ = killCommand(cmd)
					<-done
				}
				// Also retire separately grouped fixtures if this regression fails.
				entries, _ := os.ReadDir(pids)
				for _, entry := range entries {
					pid, _ := strconv.Atoi(entry.Name())
					if pid > 0 {
						if running, _ := testutil.ProcessRunning(pid); running {
							_ = syscall.Kill(-pid, syscall.SIGKILL)
						}
					}
				}
			})
			var url string
			waitAcceptance(t, func() bool {
				data, _ := os.ReadFile(log.Name())
				match := listeningURL.FindSubmatch(data)
				if len(match) > 0 {
					url = string(match[1])
				}
				return url != ""
			})
			request, err := http.NewRequestWithContext(ctx, "GET", url+"/v1/models", nil)
			if err != nil {
				t.Fatal(err)
			}
			responseDone := make(chan struct{})
			go func() {
				defer close(responseDone)
				if response, err := http.DefaultClient.Do(request); err == nil {
					_ = response.Body.Close()
				}
			}()
			waitAcceptance(t, func() bool { _, err := os.Stat(filepath.Join(work, "catalog")); return err == nil })
			entries, err := os.ReadDir(pids)
			if err != nil || len(entries) == 0 {
				t.Fatalf("no owned fixture PID: %v", err)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(9 * time.Second):
				t.Fatal("acceptance cancellation did not finish")
			}
			<-responseDone
			for _, entry := range entries {
				pid, err := strconv.Atoi(entry.Name())
				if err != nil {
					t.Fatal(err)
				}
				waitAcceptance(t, func() bool { running, err := testutil.ProcessRunning(pid); return err == nil && !running })
			}
		})
	}
}

func TestInterruptedCommandForcesUnresponsiveGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAcceptanceGatewayOwner$")
	cmd.Env = append(os.Environ(), "HARNESS_GATEWAY_ACCEPTANCE_OWNER=ignore-term", "ACCEPTANCE_OWNER_READY="+ready)
	configureCommand(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = killCommand(cmd) })
	waitAcceptance(t, func() bool { _, err := os.Stat(ready); return err == nil })
	cancel()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("unresponsive helper unexpectedly succeeded")
		}
	case <-time.After(9 * time.Second):
		t.Fatal("forced cancellation exceeded its bound")
	}
}

func waitAcceptance(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatalf("acceptance fixture condition did not arrive before %s", deadline)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
