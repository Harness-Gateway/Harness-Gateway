package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/harness-gateway/harness-gateway/internal/buildinfo"
)

const testInvocation = "0123456789abcdef0123456789abcdef"

func journalFixture(t *testing.T) (string, string) {
	t.Helper()
	path, _ := serviceControllerFixture(t)
	if err := writeService(path, []byte(serviceMarker)); err != nil {
		t.Fatal(err)
	}
	controller, err := exec.LookPath("systemctl")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "journal.calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$JOURNAL_TEST_LOG"
printf '%s' "$$" > "$JOURNAL_TEST_PID"
if [ -n "$JOURNAL_TEST_FILE" ]; then exec cat "$JOURNAL_TEST_FILE"; fi
printf '%s' "$JOURNAL_TEST_OUTPUT"
printf '%s' "$JOURNAL_TEST_ERROR" >&2
if [ "$JOURNAL_TEST_WAIT" = 1 ]; then exec sleep 30; fi
exit "$JOURNAL_TEST_EXIT"
`
	journal := filepath.Join(filepath.Dir(controller), "journalctl")
	if err := os.WriteFile(journal, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JOURNAL_TEST_LOG", log)
	t.Setenv("JOURNAL_TEST_PID", filepath.Join(t.TempDir(), "journal.pid"))
	for _, name := range []string{"JOURNAL_TEST_OUTPUT", "JOURNAL_TEST_ERROR", "JOURNAL_TEST_WAIT", "JOURNAL_TEST_FILE"} {
		t.Setenv(name, "")
	}
	t.Setenv("JOURNAL_TEST_EXIT", "0")
	t.Setenv("SERVICE_TEST_OUTPUT", "LoadState=loaded\nActiveState=active\nSubState=running\nUnitFileState=enabled\nMainPID=12345\nInvocationID="+testInvocation+"\nActiveEnterTimestamp=Sat 2026-11-07 20:56:49 CET\nFragmentPath="+path+"\n")
	return journal, log
}

func testStartupRecord() string {
	return `{"time":"2026-11-07T20:56:50+01:00","msg":"Harness-Gateway listening","version":"9.8.7","build":"running-service-build","repository":"https://github.com/Harness-Gateway/Harness-Gateway","platform":"linux/arm64","url":"http://127.0.0.1:42123","harnesses":"codex,copilot","harness_versions":{"codex":"1.2.3","copilot":"4.5.6"},"directory":"/run/user/123/harness-gateway/work","concurrency":3,"timeout":45000000000,"harness_details":[{"name":"codex","executable":"/opt/codex with spaces","model":"native-example","version":"1.2.3"},{"name":"copilot","executable":"/opt/copilot","model":"","version":"4.5.6"}]}`
}

func TestServiceDashboardUsesRunningSnapshotAndInvocation(t *testing.T) {
	_, log := journalFixture(t)
	t.Setenv("JOURNAL_TEST_OUTPUT", testStartupRecord()+"\n")
	var output bytes.Buffer
	if err := serviceCommand(t.Context(), []string{"status"}, &output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Harness-Gateway v9.8.7", "RUNNING", "active (running)", "Autostart", "enabled", "12345", "http://127.0.0.1:42123/v1", "http://127.0.0.1:42123/docs", "http://127.0.0.1:42123/health", "codex", "1.2.3", "copilot", "4.5.6", "native-example", "CLI default", "/opt/codex with spaces", "3 concurrent requests / 45s timeout", "running-service-build (linux/arm64)", "harness-gateway logs --follow"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("dashboard missing %q:\n%s", want, &output)
		}
	}
	for _, unwanted := range []string{"Harness-Gateway v" + buildinfo.Version, "Press Ctrl+C", "\x1b", `"harness_versions":`, "Loaded:"} {
		if strings.Contains(output.String(), unwanted) {
			t.Errorf("dashboard included %q: %s", unwanted, &output)
		}
	}
	calls, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(calls), "_SYSTEMD_INVOCATION_ID="+testInvocation) || !strings.Contains(string(calls), "--lines=1 --grep="+startupJournalPattern) {
		t.Fatalf("startup query did not select this invocation's startup: %s %v", calls, err)
	}
}

func TestStartupSnapshotsRoundTripBothLogFormats(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			file, err := os.Create(filepath.Join(t.TempDir(), "startup.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			status := startupStatus{baseURL: "http://[::1]:43210", directory: `/work/space "quote" \backslash Ω`, concurrency: 3, timeout: 45 * time.Second,
				harnesses: []harnessStatus{{name: "codex", executable: `/opt/space "quote" \ Ω/codex`, model: "native example", version: "1.2.3"}}}
			if err := reportStartup(file, newLogger(file, format), format, status); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := decodeStartupSnapshot(strings.TrimSpace(string(data)))
			if err != nil || snapshot.Version != buildinfo.Version || snapshot.URL != status.baseURL || snapshot.Directory != status.directory || snapshot.Timeout != status.timeout || snapshot.Concurrency != status.concurrency || len(snapshot.HarnessDetails) != 1 || snapshot.HarnessDetails[0].Executable != status.harnesses[0].executable || snapshot.HarnessDetails[0].Model != status.harnesses[0].model {
				t.Fatalf("lost startup details: %+v %v\n%s", snapshot, err, data)
			}
		})
	}
}

func TestLegacyStartupSnapshotDoesNotInventHarnessSettings(t *testing.T) {
	for _, line := range []string{
		`{"msg":"Harness-Gateway listening","version":"9.8.7","build":"old-build","url":"http://127.0.0.1:8789","harnesses":"codex","harness_versions":{"codex":"1.2.3"},"concurrency":2,"timeout":180000000000}`,
		`time=2026-11-07T20:56:50+01:00 level=INFO msg="Harness-Gateway listening" version=9.8.7 build=old-build url=http://127.0.0.1:8789 harnesses=codex harness_versions=map[codex:1.2.3] concurrency=2 timeout=3m0s`,
		`time=2026-11-07T20:56:50+01:00 level=INFO msg="Harness-Gateway listening" version=9.8.7 build=old-build url=http://127.0.0.1:8789 harnesses=codex,copilot harness_versions="map[codex:1.2.3 copilot:4.5.6]" concurrency=2 timeout=3m0s`,
	} {
		snapshot, err := decodeStartupSnapshot(line)
		if err != nil {
			t.Fatal(err)
		}
		panel := servicePanel(map[string]string{"ActiveState": "active", "SubState": "running"}, snapshot, nil, false, 100)
		for _, want := range []string{"Harness-Gateway v9.8.7", "1.2.3", "not recorded", "2 concurrent requests / 3m0s timeout"} {
			if !strings.Contains(panel, want) {
				t.Fatalf("legacy snapshot lost %q: %s", want, panel)
			}
		}
		if strings.Contains(panel, "CLI default") {
			t.Fatalf("guessed the legacy harness configuration: %s", panel)
		}
	}
	for _, line := range []string{"", "not JSON", `{`, `{"msg":"another event"}`, `{"msg":"Harness-Gateway listening"}`, `msg="unterminated`, `msg="Harness-Gateway listening" version=9.8.7 url=http://127.0.0.1 timeout=bad`} {
		if _, err := decodeStartupSnapshot(line); err == nil {
			t.Fatalf("accepted invalid snapshot: %s", line)
		}
	}
}

func TestServiceDashboardPreservesControlFailureAndWriterErrors(t *testing.T) {
	journalFixture(t)
	t.Setenv("SERVICE_TEST_EXIT_CODE", "1")
	t.Setenv("SERVICE_TEST_ERROR_OUTPUT", "Failed to connect to bus")
	if err := serviceCommand(t.Context(), []string{"status"}, io.Discard); err == nil || !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Fatalf("lost manager failure: %v", err)
	}
	t.Setenv("SERVICE_TEST_EXIT_CODE", "0")
	t.Setenv("SERVICE_TEST_ERROR_OUTPUT", "")
	t.Setenv("JOURNAL_TEST_OUTPUT", testStartupRecord())
	broken := errors.New("status reader closed")
	if err := serviceCommand(t.Context(), []string{"status"}, failedLogWriter{broken}); !errors.Is(err, broken) {
		t.Fatalf("lost dashboard writer error: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := serviceCommand(ctx, []string{"status"}, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost dashboard cancellation: %v", err)
	}
}

func TestServiceDashboardReportsStoppedAndMissingStartup(t *testing.T) {
	for _, state := range []string{"inactive", "failed", "activating"} {
		t.Run(state, func(t *testing.T) {
			_, calls := journalFixture(t)
			t.Setenv("SERVICE_TEST_OUTPUT", "ActiveState="+state+"\nSubState=dead\nUnitFileState=disabled\nMainPID=0\n")
			t.Setenv("JOURNAL_TEST_OUTPUT", testStartupRecord())
			var output bytes.Buffer
			if err := serviceCommand(t.Context(), []string{"status"}, &output); err == nil {
				t.Fatal("inactive/non-active status succeeded")
			}
			if !strings.Contains(output.String(), strings.ToUpper(state)) || strings.Contains(output.String(), "LISTENING") || strings.Contains(output.String(), "Press Ctrl+C") {
				t.Fatalf("misleading non-active dashboard: %s", &output)
			}
			if state == "activating" {
				if _, err := os.Stat(calls); !os.IsNotExist(err) {
					t.Fatal("starting service without an invocation read an old startup")
				}
				if strings.Contains(output.String(), "v9.8.7") {
					t.Fatal("starting service showed an old version")
				}
			} else if !strings.Contains(output.String(), "Last startup") {
				t.Fatalf("stopped status did not identify historical metadata: %s", &output)
			}
		})
	}
	for _, failure := range []string{"empty", "malformed", "missing-journalctl", "permission"} {
		t.Run(failure, func(t *testing.T) {
			journal, _ := journalFixture(t)
			switch failure {
			case "malformed":
				t.Setenv("JOURNAL_TEST_OUTPUT", "{broken JSON")
			case "missing-journalctl":
				if err := os.Remove(journal); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", filepath.Dir(journal))
			case "permission":
				t.Setenv("JOURNAL_TEST_EXIT", "1")
				t.Setenv("JOURNAL_TEST_ERROR", "Permission denied")
			}
			var output bytes.Buffer
			if err := serviceCommand(t.Context(), []string{"status"}, &output); err != nil {
				t.Fatalf("lost known active state when startup is unavailable: %v", err)
			}
			if !strings.Contains(output.String(), "Unavailable") || !strings.Contains(output.String(), "RUNNING") || strings.Contains(output.String(), "Harness-Gateway v"+buildinfo.Version) {
				t.Fatalf("invented startup metadata: %s", &output)
			}
		})
	}
}

func TestJournalFormattingPreservesEventsAndReadableDurations(t *testing.T) {
	timestamp := time.Date(2026, 11, 7, 20, 56, 50, 0, time.UTC)
	for _, test := range []struct{ message, priority, want string }{
		{`{"time":"2026-11-07T20:56:50Z","level":"INFO","msg":"completion finished","request_id":"req-123","duration":1500000000}`, "6", "duration=1.5s"},
		{`time=2026-11-07T20:56:50Z level=ERROR msg="completion failed" error="CLI unavailable" duration=12ms`, "6", `error="CLI unavailable"`},
		{"Starting Harness-Gateway...", "6", "INFO  Starting Harness-Gateway..."},
		{"unexpected CLI diagnostic", "3", "ERROR unexpected CLI diagnostic"},
		{"bad\nline\x1b[31m", "4", `WARN  bad\nline\x1b[31m`},
		{"{malformed JSON", "6", "{malformed JSON"},
	} {
		entry, err := json.Marshal(map[string]string{"MESSAGE": test.message, "__REALTIME_TIMESTAMP": strconv.FormatInt(timestamp.UnixMicro(), 10), "PRIORITY": test.priority})
		if err != nil {
			t.Fatal(err)
		}
		line := formatJournalLine(string(entry))
		if !strings.Contains(line, test.want) || !strings.Contains(line, timestamp.Local().Format("2006-01-02 15:04:05")) || strings.ContainsAny(line, "\x1b\n\r") {
			t.Fatalf("lost/unsafe journal event: %q (want %q)", line, test.want)
		}
	}
	startup := formatJournalLine(testStartupRecord())
	if !strings.Contains(startup, "version=9.8.7") || !strings.Contains(startup, "url=http://127.0.0.1:42123") || strings.Contains(startup, "harness_details") || strings.Contains(startup, "running-service-build") {
		t.Fatalf("startup event is not a compact summary: %s", startup)
	}
	for _, test := range []struct{ entry, want string }{
		{`{"MESSAGE":[98,97,100,10],"PRIORITY":"3"}`, `bad\n`},
		{`{"MESSAGE":null}`, "journal message unavailable"},
		{`{"MESSAGE":{"unsupported":true}}`, "unreadable journal message"},
		{`{"unrecognized":"record"}`, `{"unrecognized":"record"}`},
	} {
		if line := formatJournalLine(test.entry); !strings.Contains(line, test.want) || strings.ContainsAny(line, "\n\r\x1b") {
			t.Fatalf("lost unexpected journal content: %s", line)
		}
	}
}

func TestServiceLogsFormatsAndRawPassthrough(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(strconv.FormatBool(raw), func(t *testing.T) {
			_, calls := journalFixture(t)
			args := []string{"logs", "-n", "7"}
			message := `{"level":"INFO","msg":"completion finished","duration":5000000}` + "\n"
			if raw {
				args = append(args, "--raw")
			}
			t.Setenv("JOURNAL_TEST_OUTPUT", message)
			var output bytes.Buffer
			if err := serviceCommand(t.Context(), args, &output); err != nil {
				t.Fatal(err)
			}
			if raw && output.String() != message || !raw && (!strings.Contains(output.String(), "duration=5ms") || strings.Contains(output.String(), `"msg":`)) {
				t.Fatalf("wrong journal rendering: %s", &output)
			}
			invocation, err := os.ReadFile(calls)
			if err != nil || !strings.Contains(string(invocation), "--unit="+serviceName) || !strings.Contains(string(invocation), "--lines=7") || strings.Contains(string(invocation), "--follow") || raw && !strings.Contains(string(invocation), "--output=cat") || !raw && !strings.Contains(string(invocation), "--output=json") {
				t.Fatalf("wrong journal query: %s %v", invocation, err)
			}
		})
	}
}

type observedLogWriter struct {
	once  sync.Once
	ready chan struct{}
}

func (writer *observedLogWriter) Write(data []byte) (int, error) {
	writer.once.Do(func() { close(writer.ready) })
	return len(data), nil
}

func TestServiceLogsFollowCancellationReapsJournal(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(strconv.FormatBool(raw), func(t *testing.T) {
			journalFixture(t)
			t.Setenv("JOURNAL_TEST_OUTPUT", `{"msg":"ready"}`+"\n")
			t.Setenv("JOURNAL_TEST_WAIT", "1")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			output := &observedLogWriter{ready: make(chan struct{})}
			done := make(chan error, 1)
			args := []string{"logs", "--follow", "--lines=0"}
			if raw {
				args = append(args, "--raw")
			}
			go func() { done <- serviceCommand(ctx, args, output) }()
			select {
			case <-output.ready:
			case <-time.After(5 * time.Second):
				t.Fatal("log following did not produce incremental output")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("Ctrl+C was treated as a log failure: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("journal follower did not stop")
			}
			pid, err := os.ReadFile(os.Getenv("JOURNAL_TEST_PID"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join("/proc", string(pid))); !os.IsNotExist(err) {
				t.Fatalf("journal process remains after cancellation: %s %v", pid, err)
			}
		})
	}
}

type failedLogWriter struct{ err error }

func (writer failedLogWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestServiceLogsValidationAndFailures(t *testing.T) {
	_, calls := journalFixture(t)
	for _, args := range [][]string{{"logs", "--lines=-1"}, {"logs", "--lines=10001"}, {"logs", "-n", "invalid"}, {"logs", "other.service"}, {"logs", "--system"}} {
		if err := serviceCommand(t.Context(), args, io.Discard); err == nil {
			t.Fatalf("invalid log options accepted: %v", args)
		}
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Fatal("invalid log options contacted the journal")
	}
	t.Setenv("JOURNAL_TEST_EXIT", "1")
	t.Setenv("JOURNAL_TEST_ERROR", "journal permission denied")
	if err := serviceCommand(t.Context(), []string{"logs"}, io.Discard); err == nil || !strings.Contains(err.Error(), "journal permission denied") {
		t.Fatalf("lost journal failure: %v", err)
	}
	t.Setenv("JOURNAL_TEST_EXIT", "0")
	t.Setenv("JOURNAL_TEST_ERROR", "")
	t.Setenv("JOURNAL_TEST_OUTPUT", "event\n")
	broken := errors.New("reader closed")
	if err := serviceCommand(t.Context(), []string{"logs"}, failedLogWriter{broken}); !errors.Is(err, broken) {
		t.Fatalf("lost reader failure: %v", err)
	}
	t.Setenv("JOURNAL_TEST_WAIT", "1")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := serviceCommand(ctx, []string{"logs"}, io.Discard); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost deadline: %v", err)
	}
}

func TestServiceLogsBoundsOversizedJournalEntries(t *testing.T) {
	journalFixture(t)
	file := filepath.Join(t.TempDir(), "large-entry")
	if err := os.WriteFile(file, bytes.Repeat([]byte("x"), 1024*1024+1), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JOURNAL_TEST_FILE", file)
	if err := serviceCommand(t.Context(), []string{"logs"}, io.Discard); err == nil || !strings.Contains(err.Error(), "read journal entry") {
		t.Fatalf("oversized journal entry was not bounded: %v", err)
	}
}

func TestServiceDashboardEscapesJournalControlCharacters(t *testing.T) {
	properties := map[string]string{"ActiveState": "active", "SubState": "running", "FragmentPath": "bad\x1b[31m\nunit"}
	snapshot := startupSnapshot{Version: "9.8.7\x1b[31m", URL: "http://127.0.0.1:8789\nforged", HarnessDetails: []harnessSnapshot{{Name: "codex", Version: "1\x1b[31m", Executable: "bad\npath"}}}
	panel := servicePanel(properties, snapshot, nil, false, 100)
	if strings.Contains(panel, "\x1b") || strings.Contains(panel, "\nforged") || !strings.Contains(panel, `\nforged`) {
		t.Fatalf("journal values injected terminal controls: %q", panel)
	}
}
