package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/harness-gateway/harness-gateway/internal/buildinfo"
)

func TestDoctorRejectsForeignLoadedServiceBeforeProbes(t *testing.T) {
	for _, mode := range []string{"foreign", "transient", "unloaded"} {
		t.Run(mode, func(t *testing.T) {
			_, calls := journalFixture(t)
			path, pathErr := userServicePath()
			if pathErr != nil {
				t.Fatal(pathErr)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
			defer server.Close()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			var snapshot startupSnapshot
			if err := json.Unmarshal([]byte(testStartupRecord()), &snapshot); err != nil {
				t.Fatal(err)
			}
			snapshot.Version, snapshot.URL, snapshot.Directory = buildinfo.Version, server.URL, t.TempDir()
			snapshot.HarnessDetails = []harnessSnapshot{{Name: "codex", Executable: executable}}
			t.Setenv("HARNESS_GATEWAY_VERSION_TEST", "directory")
			t.Setenv("HARNESS_GATEWAY_VERSION_DIRECTORY", snapshot.Directory)
			record, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("JOURNAL_TEST_OUTPUT", string(record))
			fragment, state := filepath.Join(t.TempDir(), "foreign.service"), "loaded"
			if err := os.WriteFile(fragment, []byte("[Service]\nExecStart=/bin/true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "transient" {
				fragment = ""
			}
			if mode == "unloaded" {
				fragment, state = path, "not-found"
			}
			t.Setenv("SERVICE_TEST_OUTPUT", fmt.Sprintf("LoadState=%s\nFragmentPath=%s\nActiveState=active\nSubState=running\nInvocationID=%s\n", state, fragment, testInvocation))
			var output bytes.Buffer
			if err := doctorCommand(t.Context(), []string{"--service"}, &output); err == nil || !strings.Contains(output.String(), "FAIL  service:") {
				t.Fatalf("foreign ownership accepted: %v\n%s", err, &output)
			}
			if requests.Load() != 0 || strings.Contains(output.String(), " CLI:") || strings.Contains(output.String(), "gateway health") {
				t.Fatalf("unowned startup data reached probes: %s", &output)
			}
			query, err := os.ReadFile(calls)
			if err != nil || !strings.Contains(string(query), "_SYSTEMD_INVOCATION_ID="+testInvocation) {
				t.Fatalf("inspection lost its current-invocation scope: %s %v", query, err)
			}
		})
	}
}
