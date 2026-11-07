package main

import (
	"bytes"
	"context"
	"errors"
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
	for _, args := range [][]string{nil, {"--help"}, {"install", "--help"}, {"uninstall", "--help"}, {"start", "--help"}, {"stop", "-h"}, {"restart", "--help"}, {"status", "--help"}, {"enable", "--help"}, {"disable", "--help"}} {
		var output bytes.Buffer
		_ = serviceCommand(t.Context(), args, &output)
		if !strings.Contains(output.String(), "harness-gateway service") {
			t.Fatalf("missing service help: %s", &output)
		}
		if len(args) == 2 && args[0] == "install" && (!strings.Contains(output.String(), "private systemd runtime workspace") || strings.Contains(output.String(), `(default ".")`)) {
			t.Fatalf("incorrect service directory default in help: %s", &output)
		}
	}
	if runtime.GOOS != "linux" {
		for _, action := range []string{"install", "start", "stop", "restart", "status", "enable", "disable"} {
			if err := serviceCommand(t.Context(), []string{action}, io.Discard); err == nil || !strings.Contains(err.Error(), "only on Linux") {
				t.Fatalf("unsupported service platform (%s): %v", action, err)
			}
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

func TestServiceWorkingDirectoryDefaultsAndOverrides(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd user units target Linux")
	}
	config, cwd := t.TempDir(), t.TempDir()
	t.Chdir(cwd)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(config, "not-created"))
	t.Setenv("PATH", "") // No systemctl: dry-run must not contact the manager.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"runtime", nil, ""},
		{"explicit-dot", []string{"--directory", "."}, cwd},
		{"short-dot", []string{"-d", "."}, cwd},
		{"explicit-absolute", []string{"--directory=" + config}, config},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			args := append([]string{"install", "--dry-run", "--harnesses=codex", "--codex-path=" + executable}, test.args...)
			if err := serviceCommand(t.Context(), args, &output); err != nil {
				t.Fatal(err)
			}
			unit := output.String()
			if test.want == "" {
				for _, expected := range []string{"WorkingDirectory=%t/harness-gateway/work\n", "RuntimeDirectory=harness-gateway harness-gateway/work\n", "RuntimeDirectoryMode=0700\n"} {
					if !strings.Contains(unit, expected) {
						t.Fatalf("missing %q: %s", expected, unit)
					}
				}
				if strings.Contains(unit, `"--directory"`) || strings.Contains(unit, cwd) || strings.Contains(unit, config) {
					t.Fatalf("default workspace captured installer environment: %s", unit)
				}
			} else {
				quoted, err := quoteUnitValue(test.want)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(unit, "WorkingDirectory="+test.want+"/.\n") || !strings.Contains(unit, `"--directory" `+quoted) || !strings.Contains(unit, "RuntimeDirectory=harness-gateway\n") {
					t.Fatalf("explicit directory not preserved: %s", unit)
				}
			}
		})
	}
	for _, args := range [][]string{{"--directory=" + filepath.Join(config, "missing")}, {"--concurrency=0"}, {"--timeout=0s"}, {"--harnesses=unknown"}} {
		args = append([]string{"install", "--dry-run", "--harnesses=codex", "--codex-path=" + executable}, args...)
		if err := serviceCommand(t.Context(), args, io.Discard); err == nil {
			t.Fatalf("invalid service settings accepted: %v", args)
		}
	}
	entries, err := os.ReadDir(config)
	if err != nil || len(entries) != 0 {
		t.Fatalf("dry-run created service/runtime state: %v %v", entries, err)
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
	directory := filepath.Join(t.TempDir(), `literal $HOME %u space "quote" \backslash = Ω `)
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
	for _, workspace := range []string{directory, ""} {
		unit, err := renderService(executable, workspace, os.Getenv("PATH"), options, nil)
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
}

func serviceControllerFixture(t *testing.T) (string, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("systemd service installation targets Linux")
	}
	directory, log := t.TempDir(), filepath.Join(t.TempDir(), "systemctl.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$SERVICE_TEST_LOG\"\nif [ \"${SERVICE_TEST_FAIL_RELOAD:-}\" = 1 ] && [ \"$2\" = daemon-reload ]; then echo 'fixture reload failure' >&2; exit 17; fi\nprintf '%s' \"$SERVICE_TEST_OUTPUT\"\nprintf '%s' \"$SERVICE_TEST_ERROR_OUTPUT\" >&2\nexit \"${SERVICE_TEST_EXIT_CODE:-0}\"\n"
	if err := os.WriteFile(filepath.Join(directory, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SERVICE_TEST_LOG", log)
	t.Setenv("SERVICE_TEST_FAIL_RELOAD", "")
	t.Setenv("SERVICE_TEST_OUTPUT", "")
	t.Setenv("SERVICE_TEST_ERROR_OUTPUT", "")
	t.Setenv("SERVICE_TEST_EXIT_CODE", "0")
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	return filepath.Join(config, "systemd", "user", serviceName), log
}

func TestServiceControlCommands(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"start"}, "start"},
		{[]string{"stop"}, "stop"},
		{[]string{"restart"}, "restart"},
		{[]string{"status"}, "status --no-pager --full"},
		{[]string{"enable"}, "enable"},
		{[]string{"disable"}, "disable"},
		{[]string{"enable", "--now"}, "enable --now"},
		{[]string{"disable", "--now"}, "disable --now"},
		{[]string{"enable", "--now=false"}, "enable"},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			path, log := serviceControllerFixture(t)
			unit := []byte(serviceMarker + "# user settings\n")
			if err := writeService(path, unit); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SERVICE_TEST_OUTPUT", "controller output\n")
			var output bytes.Buffer
			if err := serviceCommand(t.Context(), test.args, &output); err != nil {
				t.Fatal(err)
			}
			calls, err := os.ReadFile(log)
			if err != nil || string(calls) != "--user "+test.want+" "+serviceName+"\n" {
				t.Fatalf("wrong controller invocation: %s %v", calls, err)
			}
			if !strings.Contains(output.String(), "controller output") {
				t.Fatalf("lost controller output: %s", &output)
			}
			contents, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(contents, unit) {
				t.Fatalf("service control rewrote the unit: %s %v", contents, err)
			}
		})
	}
}

func TestServiceControlRefusesUnmanagedUnitsAndInvalidArguments(t *testing.T) {
	for _, state := range []string{"missing", "foreign", "symlink", "invalid-arguments"} {
		t.Run(state, func(t *testing.T) {
			path, log := serviceControllerFixture(t)
			if state != "missing" {
				unit := []byte(serviceMarker)
				if state == "foreign" {
					unit = []byte("# unrelated unit\n")
				}
				if err := writeService(path, unit); err != nil {
					t.Fatal(err)
				}
				if state == "symlink" {
					if err := os.Rename(path, path+".real"); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(path+".real", path); err != nil {
						t.Fatal(err)
					}
				}
			}
			args := [][]string{{"start"}, {"stop"}, {"restart"}, {"status"}, {"enable"}, {"disable", "--now"}}
			if state == "invalid-arguments" {
				args = [][]string{{"start", "other.service"}, {"status", "--now"}, {"restart", "--now"}, {"enable", "--system"}, {"disable", "--now", "other.service"}, {"enable", "--now=invalid"}, {"unknown"}}
			}
			for _, arguments := range args {
				if err := serviceCommand(t.Context(), arguments, io.Discard); err == nil {
					t.Fatalf("accepted %v for %s", arguments, state)
				}
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatal("invalid control request invoked systemctl")
			}
		})
	}
}

func TestServiceControlPreservesFailureOutputAndCancellation(t *testing.T) {
	path, _ := serviceControllerFixture(t)
	if err := writeService(path, []byte(serviceMarker)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ action, code, message string }{
		{"status", "3", "Active: inactive (dead)\n"},
		{"restart", "1", "Failed to connect to bus\n"},
	} {
		t.Setenv("SERVICE_TEST_EXIT_CODE", test.code)
		t.Setenv("SERVICE_TEST_OUTPUT", "")
		t.Setenv("SERVICE_TEST_ERROR_OUTPUT", "")
		if test.action == "status" {
			t.Setenv("SERVICE_TEST_OUTPUT", test.message)
		} else {
			t.Setenv("SERVICE_TEST_ERROR_OUTPUT", test.message)
		}
		var output bytes.Buffer
		err := serviceCommand(t.Context(), []string{test.action}, &output)
		var exit *exec.ExitError
		if !errors.As(err, &exit) || strconv.Itoa(exit.ExitCode()) != test.code || output.String() != test.message {
			t.Fatalf("lost failure details: %v %q", err, &output)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := serviceCommand(ctx, []string{"restart"}, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}

func TestServiceInstallUpdateAndUninstall(t *testing.T) {
	path, log := serviceControllerFixture(t)
	unit := []byte(serviceMarker + "[Service]\nExecStart=/bin/true\n")
	var instructions bytes.Buffer
	if err := installService(t.Context(), path, unit, &instructions); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(instructions.String(), "harness-gateway service enable --now") || !strings.Contains(instructions.String(), "harness-gateway service restart") {
		t.Fatalf("missing native service instructions: %s", &instructions)
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
