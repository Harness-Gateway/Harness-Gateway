package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestServiceHelpAndPlatform(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"install", "--help"}, {"uninstall", "--help"}} {
		var output bytes.Buffer
		_ = serviceCommand(t.Context(), args, &output)
		if !strings.Contains(output.String(), "harness-gateway service") {
			t.Fatalf("missing service help: %s", &output)
		}
	}
	if runtime.GOOS != "linux" {
		if err := serviceCommand(t.Context(), []string{"install"}, io.Discard); err == nil || !strings.Contains(err.Error(), "only on Linux") {
			t.Fatalf("unsupported service platform: %v", err)
		}
	}
}

func TestServiceDryRunDoesNotContactManagerOrWriteFiles(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd user unit generation command targets Linux")
	}
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("PATH", "")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	args := []string{"install", "--dry-run", "--harnesses=codex", "--codex-path=" + executable, "--directory=" + config}
	if err := serviceCommand(t.Context(), args, &output); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{serviceMarker, "ExecStart=:", `"--log-format" "json"`, "WantedBy=default.target", "KillMode=mixed"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("unit missing %q: %s", expected, &output)
		}
	}
	entries, err := os.ReadDir(config)
	if err != nil || len(entries) != 0 {
		t.Fatalf("dry-run changed files: %v %v", entries, err)
	}
	args = append(args, "--listen=0.0.0.0:8080")
	if err := serviceCommand(t.Context(), args, io.Discard); err == nil {
		t.Fatal("service accepted a non-loopback address")
	}
}

func TestServiceUnitEscapesLiteralPathsAndRejectsInjectedSettings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path quoting fixture")
	}
	directory := filepath.Join(t.TempDir(), `with space %u $HOME "quote" \backslash Ω`)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	options, err := parseOptions(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	harnesses := []harnessStatus{{name: "codex", executable: executable, model: `native/$HOME%u`}}
	unit, err := renderService(executable, directory, "/bin:"+directory, options, harnesses)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`%%u $HOME \"quote\" \\backslash Ω`, `"native/$HOME%%u"`, "ExecStart=:", "Restart=on-failure", "TimeoutStopSec=15s"} {
		if !bytes.Contains(unit, []byte(expected)) {
			t.Fatalf("missing quoted value %q in %s", expected, unit)
		}
	}
	for _, invalid := range []string{"bad\nExecStart=/bin/false", "bad\rvalue", "bad\x00value", string([]byte{0xff})} {
		if _, err := renderService(executable, directory, invalid, options, harnesses); err == nil {
			t.Fatalf("accepted injected/invalid PATH %q", invalid)
		}
	}
}

func TestServiceUnitPassesSystemdVerification(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd-analyze is Linux-only")
	}
	analyze, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze is not installed")
	}
	directory := filepath.Join(t.TempDir(), "literal $HOME %u space")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(directory, "gateway")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	options, err := parseOptions(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := renderService(executable, directory, os.Getenv("PATH"), options, nil)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), serviceName)
	if err := os.WriteFile(file, unit, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), analyze, "--user", "verify", "--man=no", file)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("systemd rejected the unit: %v\n%s\n%s", err, output, unit)
	}
}

func serviceControllerFixture(t *testing.T) (string, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("systemd service installation targets Linux")
	}
	directory, log := t.TempDir(), filepath.Join(t.TempDir(), "systemctl.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$SERVICE_TEST_LOG\"\nif [ \"${SERVICE_TEST_FAIL_RELOAD:-}\" = 1 ] && [ \"$2\" = daemon-reload ]; then echo 'fixture reload failure' >&2; exit 17; fi\n"
	if err := os.WriteFile(filepath.Join(directory, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SERVICE_TEST_LOG", log)
	t.Setenv("SERVICE_TEST_FAIL_RELOAD", "")
	return filepath.Join(t.TempDir(), "systemd", "user", serviceName), log
}

func TestServiceInstallUpdateAndUninstall(t *testing.T) {
	path, log := serviceControllerFixture(t)
	unit := []byte(serviceMarker + "[Service]\nExecStart=/bin/true\n")
	if err := installService(t.Context(), path, unit, io.Discard); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("unit permissions: %v %v", info, err)
	}
	output, err := os.ReadFile(log)
	if err != nil || string(output) != "--user daemon-reload\n" {
		t.Fatalf("installation enabled/started a unit: %v %s", err, output)
	}
	unit = append(unit, []byte("Restart=on-failure\n")...)
	if err := installService(t.Context(), path, unit, io.Discard); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(unit, contents) {
		t.Fatalf("unit update: %v %s", err, contents)
	}
	dropin := filepath.Join(path+".d", "custom.conf")
	if err := os.MkdirAll(filepath.Dir(dropin), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropin, []byte("# user settings"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := uninstallService(t.Context(), path, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unit remains installed")
	}
	if _, err := os.Stat(dropin); err != nil {
		t.Fatalf("uninstall removed user drop-ins: %v", err)
	}
	output, err = os.ReadFile(log)
	if err != nil || !strings.HasSuffix(string(output), "--user disable --now harness-gateway.service\n--user daemon-reload\n") {
		t.Fatalf("uninstall did not stop/disable/reload: %v %s", err, output)
	}
}

func TestServiceReloadFailureRestoresPreviousUnit(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(strconv.FormatBool(existing), func(t *testing.T) {
			path, _ := serviceControllerFixture(t)
			original := []byte(serviceMarker + "# original\n")
			if existing {
				if err := writeService(path, original); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("SERVICE_TEST_FAIL_RELOAD", "1")
			if err := installService(t.Context(), path, []byte(serviceMarker+"# replacement\n"), io.Discard); err == nil || !strings.Contains(err.Error(), "fixture reload failure") {
				t.Fatalf("reload error lost: %v", err)
			}
			contents, err := os.ReadFile(path)
			if existing {
				if err != nil || !bytes.Equal(contents, original) {
					t.Fatalf("original not restored: %v %s", err, contents)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("failed installation left a unit")
			}
		})
	}
}

func TestServiceRefusesForeignFilesAndSymlinks(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(strconv.FormatBool(symlink), func(t *testing.T) {
			path, log := serviceControllerFixture(t)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte("# unrelated user unit\n")
			if symlink {
				target := filepath.Join(t.TempDir(), "original")
				if err := os.WriteFile(target, []byte(serviceMarker), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := installService(t.Context(), path, []byte(serviceMarker), io.Discard); err == nil {
				t.Fatal("replaced foreign file")
			}
			if err := uninstallService(t.Context(), path, io.Discard); err == nil {
				t.Fatal("removed foreign file")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal("foreign file was removed")
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatal("foreign unit caused systemctl calls")
			}
		})
	}
}
