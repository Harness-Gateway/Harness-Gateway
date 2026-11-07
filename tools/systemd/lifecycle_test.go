//go:build linux

package systemd_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Run only in an isolated user account with a real systemd user manager.
// The CI wrapper creates that account; no provider credentials are used.
func TestSystemdUserService(t *testing.T) {
	if os.Getenv("HARNESS_GATEWAY_TEST_SYSTEMD") != "1" {
		t.Skip("requires an isolated systemd user manager; see ci.sh")
	}
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit-directory=%t", explicit), func(t *testing.T) {
			testSystemdUserService(t, explicit)
		})
	}
}

func testSystemdUserService(t *testing.T, explicitDirectory bool) {
	runtimeRoot := os.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(runtimeRoot) {
		t.Fatal("test requires an absolute XDG_RUNTIME_DIR")
	}
	runtimeDirectory := filepath.Join(runtimeRoot, "harness-gateway")
	config, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(config, "systemd", "user", "harness-gateway.service")
	for _, path := range []string{unit, unit + ".d"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("test refuses an existing service/configuration: %s (%v)", path, err)
		}
	}
	directory := filepath.Join(t.TempDir(), `space %u $HOME "quote" \backslash Ω `)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]string)
	for _, name := range []string{"gateway", "codex", "copilot"} {
		source := os.Getenv("HARNESS_GATEWAY_SYSTEMD_" + strings.ToUpper(name))
		if source == "" {
			t.Fatalf("missing prebuilt %s executable", name)
		}
		contents, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		paths[name] = filepath.Join(directory, name)
		if err := os.WriteFile(paths[name], contents, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Mimic npm launchers: their interpreter is found only through the PATH
	// captured by installation, not the systemd manager's original environment.
	runner := filepath.Join(directory, "harness-fixture-runner")
	if err := os.WriteFile(runner, []byte("#!/bin/sh\ntarget=$1\nshift\nexec \"$target-real\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"codex", "copilot"} {
		if err := os.Rename(paths[name], paths[name]+"-real"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths[name], []byte("#!/usr/bin/env harness-fixture-runner\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory+":"+os.Getenv("PATH"))
	command := func(name string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = directory
		cmd.WaitDelay = 2 * time.Second
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	ctl := func(args ...string) string { return command("systemctl", append([]string{"--user"}, args...)...) }
	installArgs := []string{"service", "install", "--listen=127.0.0.1:0",
		"--codex-path=" + paths["codex"], "--copilot-path=" + paths["copilot"]}
	workspace := filepath.Join(runtimeDirectory, "work")
	if explicitDirectory {
		installArgs = append(installArgs, "--directory="+directory)
		workspace = directory
	}
	command(paths["gateway"], installArgs...)
	if _, err := os.Stat(runtimeDirectory); !os.IsNotExist(err) {
		t.Fatalf("installation created runtime state before service start: %v", err)
	}
	t.Cleanup(func() {
		if _, err := os.Lstat(unit); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if output, err := exec.CommandContext(ctx, paths["gateway"], "service", "uninstall").CombinedOutput(); err != nil {
				t.Errorf("uninstall cleanup: %v\n%s", err, output)
			}
		}
		if err := os.RemoveAll(unit + ".d"); err != nil {
			t.Error(err)
		}
	})
	if ctl("show", "--property=ActiveState", "--value", "harness-gateway.service") != "inactive" {
		t.Fatal("install started the service")
	}
	pids := filepath.Join(directory, "pids")
	if err := os.Mkdir(pids, 0700); err != nil {
		t.Fatal(err)
	}
	// These variables belong only to the test unit, never the generated user unit.
	dropin := "[Service]\nEnvironment=HARNESS_GATEWAY_CODEX_TEST=acceptance\nEnvironment=HARNESS_GATEWAY_COPILOT_TEST=acceptance\n"
	for key, value := range map[string]string{"HARNESS_GATEWAY_ACCEPTANCE_PIDS": pids, "HARNESS_GATEWAY_COPILOT_PID": filepath.Join(directory, "copilot-pid"), "HARNESS_GATEWAY_COPILOT_LOG": filepath.Join(directory, "copilot.log")} {
		// Paths contain '$' and '%': Environment values expand specifiers only.
		value = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(key + "=" + value)
		dropin += "Environment=\"" + value + "\"\n"
	}
	if err := os.Mkdir(unit+".d", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unit+".d", "fixture.conf"), []byte(dropin), 0600); err != nil {
		t.Fatal(err)
	}
	ctl("daemon-reload")
	ctl("enable", "--now", "harness-gateway.service")
	link := filepath.Join(filepath.Dir(unit), "default.target.wants", filepath.Base(unit))
	if target, err := filepath.EvalSymlinks(link); err != nil || target != unit {
		t.Fatalf("autostart target link: %s %v", target, err)
	}
	client := &http.Client{Timeout: 25 * time.Second}
	defer client.CloseIdleConnections()
	urlPattern := regexp.MustCompile(`"url":"(http://127\.0\.0\.1:[0-9]+)"`)
	ready := func() (string, string) {
		t.Helper()
		var base, pid string
		poll(t, 20*time.Second, func() bool {
			pid = ctl("show", "--property=MainPID", "--value", "harness-gateway.service")
			invocation := ctl("show", "--property=InvocationID", "--value", "harness-gateway.service")
			if pid == "0" || invocation == "" {
				return false
			}
			logs := command("journalctl", "--user", "--no-pager", "--output=cat", "_SYSTEMD_INVOCATION_ID="+invocation)
			match := urlPattern.FindStringSubmatch(logs)
			if len(match) == 0 {
				return false
			}
			base = match[1]
			response, err := client.Get(base + "/health")
			if err != nil {
				return false
			}
			defer response.Body.Close()
			return response.StatusCode == 200
		})
		cwd, err := os.Readlink(filepath.Join("/proc", pid, "cwd"))
		if err != nil || cwd != workspace {
			t.Fatalf("service working directory: got %q, want %q (%v)", cwd, workspace, err)
		}
		for _, path := range []string{runtimeDirectory, workspace} {
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
				t.Fatalf("workspace privacy: %s %v %v", path, info, err)
			}
		}
		return base, pid
	}
	base, pid := ready()
	catalog, err := client.Get(base + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	models, readErr := io.ReadAll(catalog.Body)
	_ = catalog.Body.Close()
	if readErr != nil || catalog.StatusCode != 200 || !strings.Contains(string(models), "codex/native-one") || !strings.Contains(string(models), "copilot/native-one") {
		t.Fatalf("service discovery: %v HTTP %d %s", readErr, catalog.StatusCode, models)
	}
	for _, model := range []string{"codex", "copilot"} {
		for _, stream := range []bool{false, true} {
			request, err := http.NewRequestWithContext(t.Context(), "POST", base+"/v1/chat/completions", strings.NewReader(chatBody(model, "normal", stream)))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil || response.StatusCode != 200 {
				t.Fatalf("%s stream=%t: %v HTTP %d %s", model, stream, readErr, response.StatusCode, data)
			}
			if stream && !strings.Contains(string(data), "data: [DONE]") {
				t.Fatalf("incomplete SSE: %s", data)
			}
			if !stream && !strings.Contains(string(data), "hello world") {
				t.Fatalf("missing buffered text: %s", data)
			}
		}
	}
	for _, action := range []string{"restart", "crash", "stop"} {
		t.Run(action, func(t *testing.T) {
			marker := filepath.Join(workspace, "workspace-state")
			for _, path := range []string{marker, filepath.Join(runtimeDirectory, "temporary-state")} {
				if err := os.WriteFile(path, []byte("previous invocation"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			type held struct {
				body   io.ReadCloser
				reader *bufio.Reader
			}
			var streams []held
			for _, model := range []string{"codex", "copilot"} {
				request, err := http.NewRequestWithContext(t.Context(), "POST", base+"/v1/chat/completions", strings.NewReader(chatBody(model, "hold", true)))
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("Content-Type", "application/json")
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = response.Body.Close() })
				if response.StatusCode != 200 {
					t.Fatalf("hold request: HTTP %d", response.StatusCode)
				}
				reader := bufio.NewReader(response.Body)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(line, `"content":"ready"`) {
						break
					}
				}
				streams = append(streams, held{response.Body, reader})
			}
			if action == "crash" {
				ctl("kill", "--signal=SIGKILL", "--kill-whom=main", "harness-gateway.service")
			} else {
				ctl(action, "harness-gateway.service")
			}
			for _, stream := range streams {
				data, err := io.ReadAll(stream.reader)
				_ = stream.body.Close()
				if action != "crash" && (err != nil || !strings.Contains(string(data), "gateway_shutting_down") || strings.Contains(string(data), "[DONE]")) {
					t.Fatalf("graceful service stop lost interruption: %v %s", err, data)
				}
			}
			poll(t, 20*time.Second, func() bool { return allExited(t, pids) })
			if action == "stop" {
				time.Sleep(5500 * time.Millisecond)
				if ctl("show", "--property=ActiveState", "--value", "harness-gateway.service") != "inactive" {
					t.Fatal("explicit stop was automatically restarted")
				}
				if _, err := os.Stat(runtimeDirectory); !os.IsNotExist(err) {
					t.Fatal("service temporary directory remains after stop")
				}
			} else {
				previous := pid
				poll(t, 25*time.Second, func() bool {
					value := ctl("show", "--property=MainPID", "--value", "harness-gateway.service")
					return value != "0" && value != previous
				})
				base, pid = ready()
				entries, err := os.ReadDir(runtimeDirectory)
				if err != nil {
					t.Fatal(err)
				}
				if explicitDirectory && len(entries) != 0 || !explicitDirectory && (len(entries) != 1 || entries[0].Name() != "work") {
					t.Fatalf("previous invocation left temporary state: %v", entries)
				}
			}
			_, err := os.Stat(marker)
			if explicitDirectory && err != nil || !explicitDirectory && !os.IsNotExist(err) {
				t.Fatalf("workspace persistence: explicit=%t, marker error=%v", explicitDirectory, err)
			}
		})
	}
	command(paths["gateway"], "service", "uninstall")
	if _, err := os.Lstat(unit); !os.IsNotExist(err) {
		t.Fatal("uninstall left unit file")
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("uninstall left autostart link")
	}
	if _, err := os.Stat(filepath.Join(unit+".d", "fixture.conf")); err != nil {
		t.Fatal("uninstall removed custom drop-ins")
	}
}

func chatBody(model, text string, stream bool) string {
	return fmt.Sprintf(`{"model":%q,"messages":[{"role":"system","content":"Be brief"},{"role":"user","content":%q}],"stream":%t}`, model, text, stream)
}

func poll(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for service/process state")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func allExited(t *testing.T, directory string) bool {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(pid, 0); err == nil {
			return false
		} else if err != syscall.ESRCH {
			t.Fatalf("probe child PID %d: %v", pid, err)
		}
	}
	return true
}
