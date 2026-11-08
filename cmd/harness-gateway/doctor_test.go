package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harness-gateway/harness-gateway/internal/buildinfo"
	"github.com/spf13/pflag"
)

func TestDoctorChecksAndExplicitInference(t *testing.T) {
	for _, probe := range []bool{false, true} {
		t.Run(fmt.Sprint(probe), func(t *testing.T) {
			var health, models, completions atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/health":
					health.Add(1)
					io.WriteString(w, `{"status":"ok"}`)
				case "/v1/models":
					models.Add(1)
					io.WriteString(w, `{"object":"list","data":[{"id":"codex"}]}`)
				case "/v1/chat/completions":
					completions.Add(1)
					var request struct{ Model string }
					if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Model != "codex/native" {
						t.Error("probe did not use the explicitly requested model")
					}
					io.WriteString(w, `{"object":"chat.completion","choices":[{"message":{"content":"OK"}}]}`)
				default:
					t.Errorf("unexpected diagnostic request: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HARNESS_GATEWAY_VERSION_TEST", "ok")
			// Loopback diagnostics must bypass proxy settings.
			t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
			t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
			args := []string{"--listen", server.Listener.Addr().String(), "--harnesses", "codex", "--codex-path", executable, "--directory", t.TempDir()}
			if probe {
				args = append(args, "--probe", "codex/native")
			}
			var output bytes.Buffer
			if err := doctorCommand(t.Context(), args, &output); err != nil {
				t.Fatalf("%v\n%s", err, &output)
			}
			for _, want := range []string{"PASS  installed gateway", "PASS  configuration", "PASS  codex CLI: 1.2.3", "PASS  gateway health", "PASS  model discovery"} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("report lacks %q: %s", want, &output)
				}
			}
			if health.Load() != 1 || models.Load() != 1 || (probe && completions.Load() != 1) || (!probe && completions.Load() != 0) {
				t.Fatalf("unexpected calls: health=%d models=%d inference=%d", health.Load(), models.Load(), completions.Load())
			}
		})
	}
}

func TestDoctorFlagsAndOfflineHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}} {
		var output bytes.Buffer
		err := doctorCommand(t.Context(), args, &output)
		if err != nil && !errors.Is(err, pflag.ErrHelp) {
			t.Fatal(err)
		}
		if output.Len() == 0 {
			t.Fatal("empty offline help/version")
		}
	}
	for _, args := range [][]string{{"--timeout=0"}, {"--probe="}, {"--service", "--listen=127.0.0.1:1234"}, {"--service", "--harnesses=codex"}, {"unexpected"}, {"--unknown"}} {
		if err := doctorCommand(t.Context(), args, io.Discard); err == nil {
			t.Fatalf("accepted invalid options: %v", args)
		}
	}
}

func TestDoctorHTTPFailuresAndBounds(t *testing.T) {
	for _, test := range []struct {
		name, health, models, want string
		status                     int
	}{
		{"unhealthy", `{"status":"starting"}`, "", "health response", 200},
		{"redirect", "", "", "HTTP 302", 302},
		{"busy", `{"status":"ok"}`, "", "HTTP 429", 429},
		{"invalid-json", `{"status":"ok"}`, `not json`, "invalid character", 200},
		{"empty-models", `{"status":"ok"}`, `{"object":"list","data":[]}`, "no usable catalog", 200},
		{"oversized", `{"status":"ok"}`, strings.Repeat("x", 1024*1024+1), "exceeds 1 MiB", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					if test.name == "redirect" {
						w.Header().Set("Location", "https://must-not-follow.invalid")
						w.WriteHeader(302)
						return
					}
					io.WriteString(w, test.health)
					return
				}
				w.WriteHeader(test.status)
				io.WriteString(w, test.models)
			}))
			defer server.Close()
			var output bytes.Buffer
			checks := doctorHTTPChecks(t.Context(), server.URL, "")
			if printDoctorChecks(&output, checks) == nil || !strings.Contains(output.String(), test.want) {
				t.Fatalf("failure lost: %s", &output)
			}
		})
	}
	for _, target := range []string{"http://example.com:80", "https://127.0.0.1:1234", "http://user@127.0.0.1:1234", "http://127.0.0.1:1234/path", "http://127.0.0.1:1234?query=1"} {
		checks := doctorHTTPChecks(t.Context(), target, "")
		if len(checks) != 1 || checks[0].name != "gateway URL" || checks[0].err == nil {
			t.Fatalf("unsafe URL was accepted: %s", target)
		}
	}
}

func TestDoctorDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	checks := doctorHTTPChecks(ctx, server.URL, "")
	if len(checks) != 1 || !errors.Is(checks[0].err, context.DeadlineExceeded) {
		t.Fatalf("diagnostics did not honor deadline: %+v", checks)
	}
}

func TestDoctorServiceUsesCurrentInvocation(t *testing.T) {
	for _, test := range []struct {
		name, state, version string
		wantFailure          string
	}{
		{"running", "active", buildinfo.Version, ""},
		{"version-mismatch", "active", "0.0.1", "differs from running"},
		{"inactive", "inactive", buildinfo.Version, "historical startup settings will not be probed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, calls := journalFixture(t)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HARNESS_GATEWAY_VERSION_TEST", "ok")
			var httpCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httpCalls.Add(1)
				if r.URL.Path == "/health" {
					io.WriteString(w, `{"status":"ok"}`)
				} else {
					io.WriteString(w, `{"object":"list","data":[{"id":"codex"}]}`)
				}
			}))
			defer server.Close()
			var snapshot startupSnapshot
			if err := json.Unmarshal([]byte(testStartupRecord()), &snapshot); err != nil {
				t.Fatal(err)
			}
			snapshot.Version, snapshot.URL, snapshot.Directory = test.version, server.URL, t.TempDir()
			snapshot.HarnessDetails = []harnessSnapshot{{Name: "codex", Executable: executable}}
			data, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("JOURNAL_TEST_OUTPUT", string(data))
			t.Setenv("SERVICE_TEST_OUTPUT", "ActiveState="+test.state+"\nSubState=running\nInvocationID="+testInvocation+"\nUnitFileState=enabled\n")
			var output bytes.Buffer
			err = doctorCommand(t.Context(), []string{"--service"}, &output)
			if (err != nil) != (test.wantFailure != "") || !strings.Contains(output.String(), test.wantFailure) {
				t.Fatalf("%v\n%s", err, &output)
			}
			if test.state == "inactive" {
				if httpCalls.Load() != 0 {
					t.Fatal("probed historical listener")
				}
				return
			}
			query, err := os.ReadFile(calls)
			if err != nil || !strings.Contains(string(query), "_SYSTEMD_INVOCATION_ID="+testInvocation) {
				t.Fatalf("diagnostics used an unscoped journal: %s %v", query, err)
			}
			if httpCalls.Load() != 2 || !strings.Contains(output.String(), executable) {
				t.Fatalf("did not use running service settings: %s", &output)
			}
		})
	}
}

func TestDoctorReportsMissingConfiguration(t *testing.T) {
	var output bytes.Buffer
	err := doctorCommand(t.Context(), []string{"--harnesses=codex", "--codex-path=" + t.TempDir() + "/missing", "--listen=127.0.0.1:0"}, &output)
	if err == nil || !strings.Contains(output.String(), "FAIL  configuration") || !strings.Contains(output.String(), "FAIL  gateway health") {
		t.Fatalf("independent failures were not reported: %v %s", err, &output)
	}
}
