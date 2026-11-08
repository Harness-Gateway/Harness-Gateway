package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/harness-gateway/harness-gateway/internal/buildinfo"
)

// CI installs the candidate into a private directory before enabling this
// cross-platform check. It runs the same HTTP checks as public-tag verification.
func TestInstalledGatewaySmoke(t *testing.T) {
	if os.Getenv("HARNESS_GATEWAY_TEST_INSTALLED_GATEWAY") != "1" {
		t.Skip("requires a private installed candidate")
	}
	binary := os.Getenv("HARNESS_GATEWAY_INSTALLED_GATEWAY")
	if !filepath.IsAbs(binary) {
		t.Fatal("provide an absolute installed gateway path")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	output, err := runCommand(ctx, root, os.Environ(), binary, "--version")
	if err != nil || !strings.HasPrefix(string(output), "Harness-Gateway v"+buildinfo.Version+" (") {
		t.Fatalf("wrong installed candidate: %s %v", output, err)
	}
	if revision := os.Getenv("HARNESS_GATEWAY_EXPECT_REVISION"); revision != "" && (!strings.Contains(string(output), revision) || strings.Contains(string(output), "+dirty")) {
		t.Fatalf("installed candidate does not match the clean commit: %s", output)
	}
	fixtures, err := buildFixtures(ctx, repo, root)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := smoke(ctx, root, binary, "v"+buildinfo.Version, fixtures)
	if err != nil || checks != 24 {
		t.Fatalf("installed gateway HTTP checks=%d: %v", checks, err)
	}
	t.Logf("installed gateway passed %d HTTP checks: %s", checks, strings.TrimSpace(string(output)))
}

const commandFixtureOutput = "{\"Error\":\"fixture module download failed\"}\nsecond line Ω\n"

func TestMain(m *testing.M) {
	switch os.Getenv("HARNESS_GATEWAY_RELEASEVERIFY_COMMAND_FIXTURE") {
	case "failed-output":
		fmt.Print(commandFixtureOutput)
		os.Exit(23)
	case "failed-empty":
		os.Exit(23)
	case "success":
		fmt.Print(commandFixtureOutput)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCommandFailurePreservesOutputAndExitStatus(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"failed-output", "failed-empty", "success"} {
		t.Run(mode, func(t *testing.T) {
			env := append(os.Environ(), "HARNESS_GATEWAY_RELEASEVERIFY_COMMAND_FIXTURE="+mode)
			output, err := runCommand(t.Context(), t.TempDir(), env, executable, "-test.run=^$")
			if mode == "success" {
				if err != nil || string(output) != commandFixtureOutput {
					t.Fatalf("successful output changed: %v\n%s", err, output)
				}
				return
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
				t.Fatalf("failed command lost its exit status: %v", err)
			}
			if len(output) != 0 {
				t.Fatal("failed command output was returned as successful output")
			}
			if mode == "failed-output" && !strings.Contains(err.Error(), commandFixtureOutput) {
				t.Fatalf("failed command lost its stdout diagnostics: %v", err)
			}
			if mode == "failed-empty" && strings.Contains(err.Error(), "stdout:") {
				t.Fatalf("empty stdout produced a misleading diagnostic section: %v", err)
			}
		})
	}
}

func TestInvalidReleaseIdentityFailsBeforeInstalling(t *testing.T) {
	for _, version := range []string{"", "latest", "main", "v1.2.3/../../main", "v01.2.3", "v1.2.3 --help", "v1.2.3-rc.1"} {
		if _, err := verify(t.Context(), version, strings.Repeat("a", 40), ""); err == nil {
			t.Fatalf("accepted invalid version %q", version)
		}
	}
	if _, err := verify(t.Context(), "v1.2.3", "short", ""); err == nil {
		t.Fatal("accepted incomplete tag revision")
	}
}

func TestPublicModuleMustMatchTagAndChecksum(t *testing.T) {
	valid := moduleInfo{Path: modulePath, Version: "v1.2.3", Dir: "/module", Sum: "h1:checksum"}
	valid.Origin.Hash, valid.Origin.Ref = strings.Repeat("a", 40), "refs/tags/v1.2.3"
	if err := validateModule(valid, valid.Version, valid.Origin.Hash); err != nil {
		t.Fatal(err)
	}
	for _, corrupt := range []func(*moduleInfo){
		func(m *moduleInfo) { m.Path = "example.com/fork" },
		func(m *moduleInfo) { m.Version = "v1.2.4" },
		func(m *moduleInfo) { m.Origin.Hash = strings.Repeat("b", 40) },
		func(m *moduleInfo) { m.Origin.Ref = "refs/heads/main" },
		func(m *moduleInfo) { m.Sum = "" },
		func(m *moduleInfo) { m.Dir = "" },
	} {
		broken := valid
		corrupt(&broken)
		if err := validateModule(broken, valid.Version, valid.Origin.Hash); err == nil {
			t.Fatalf("accepted corrupt module %+v", broken)
		}
	}
}

func TestPublicInstallCannotInheritProxyOrPrivateSettings(t *testing.T) {
	work := t.TempDir()
	env := publicEnvironment([]string{"PATH=/bin", "GOPROXY=file:///fake", "GOSUMDB=off", "GOPRIVATE=*", "GONOSUMDB=*", "GONOPROXY=*", "GOENV=/custom", "GOFLAGS=-ldflags=bad", "HOME=/real-home", "GOBIN=/real-bin", "GOMODCACHE=/real-cache"}, work)
	values := make(map[string]string)
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if _, exists := values[key]; exists {
			t.Fatalf("duplicate environment setting %s", key)
		}
		values[key] = value
	}
	for key, expected := range map[string]string{"PATH": "/bin", "GOPROXY": "https://proxy.golang.org", "GOSUMDB": "sum.golang.org", "GOPRIVATE": "", "GONOSUMDB": "", "GONOPROXY": "", "GOENV": "off", "GOFLAGS": "", "GOBIN": "", "HOME": filepath.Join(work, "home"), "GOMODCACHE": filepath.Join(work, "module-cache")} {
		if values[key] != expected {
			t.Fatalf("%s = %q, expected %q", key, values[key], expected)
		}
	}
}

func TestStreamSmokeRejectsTruncationErrorsAndWrongModels(t *testing.T) {
	text := `data: {"model":"codex/native-one","choices":[{"delta":{"content":"hello world"}}]}` + "\n\n"
	finish := `data: {"model":"codex/native-one","choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n"
	if err := checkStream([]byte(text+finish+"data: [DONE]\n\n"), "codex/native-one"); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{text, text + "data: [DONE]\n\n", text + "data: {\"error\":{}}\n\ndata: [DONE]\n\n", text + finish + "data: [DONE]\n\n" + text, text + finish + text, "data: [DONE]\n\n"} {
		if err := checkStream([]byte(body), "codex/native-one"); err == nil {
			t.Fatalf("accepted broken stream %q", body)
		}
	}
	if err := checkStream([]byte(text+finish+"data: [DONE]\n\n"), "copilot/native-one"); err == nil {
		t.Fatal("accepted another model's stream")
	}
}

func TestSmokeRejectsWrongPublishedAPIVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/":
			w.Header().Set("Location", "/docs")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "/docs":
			_, _ = w.Write([]byte(`<script src="/docs/assets/scalar-1.0.0.js"></script>`))
		case "/openapi.json":
			_, _ = w.Write([]byte(`{"info":{"version":"0.0.0"}}`))
		default:
			_, _ = w.Write([]byte("javascript"))
		}
	}))
	defer server.Close()
	if _, err := smokeHTTP(context.Background(), server.URL, "v1.2.3"); err == nil || !strings.Contains(err.Error(), "OpenAPI release version") {
		t.Fatalf("wrong version was not rejected: %v", err)
	}
}
