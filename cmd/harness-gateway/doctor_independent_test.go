package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDoctorChecksAvailableCLIsIndependently(t *testing.T) {
	for _, order := range []string{"codex,copilot", "copilot,codex"} {
		for _, available := range []string{"codex", "copilot"} {
			t.Run(order+"-available-"+available, func(t *testing.T) {
				var health, models atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/health" {
						health.Add(1)
						io.WriteString(w, `{"status":"ok"}`)
					} else {
						models.Add(1)
						io.WriteString(w, `{"object":"list","data":[{"id":"codex"}]}`)
					}
				}))
				defer server.Close()
				executable, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				pids := t.TempDir()
				t.Setenv("HARNESS_GATEWAY_VERSION_TEST", "bare")
				t.Setenv("HARNESS_GATEWAY_VERSION_PIDS", pids)
				args := []string{"--listen", server.Listener.Addr().String(), "--directory", t.TempDir(), "--harnesses", available, "--" + available + "-path", executable}
				var control bytes.Buffer
				if err := doctorCommand(t.Context(), args, &control); err != nil {
					t.Fatalf("valid CLI control failed: %v %s", err, &control)
				}
				entries, _ := os.ReadDir(pids)
				if len(entries) != 1 {
					t.Fatalf("control did not run exactly one version query: %d", len(entries))
				}
				for _, e := range entries {
					if err := os.Remove(filepath.Join(pids, e.Name())); err != nil {
						t.Fatal(err)
					}
				}
				missing := "copilot"
				if available == missing {
					missing = "codex"
				}
				args = []string{"--listen", server.Listener.Addr().String(), "--directory", t.TempDir(), "--harnesses", order, "--" + available + "-path", executable, "--" + missing + "-path", filepath.Join(t.TempDir(), "missing-cli")}
				var output bytes.Buffer
				err = doctorCommand(t.Context(), args, &output)
				entries, _ = os.ReadDir(pids)
				if err == nil || len(entries) != 1 || !strings.Contains(output.String(), "PASS  "+available+" CLI: 4.5.6") || !strings.Contains(output.String(), "FAIL  "+missing+" CLI") || health.Load() != 2 || models.Load() != 2 {
					t.Fatalf("unexpected probe result: err=%v queries=%d health=%d models=%d output=%s", err, len(entries), health.Load(), models.Load(), &output)
				}
			})
		}
	}
}
