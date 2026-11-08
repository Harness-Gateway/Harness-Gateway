package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/harness-gateway/harness-gateway/internal/buildinfo"
	"github.com/spf13/pflag"
)

const serviceName = "harness-gateway.service"
const serviceMarker = "# Managed by Harness-Gateway: systemd user service.\n"
const maxUnitBytes = 64 * 1024

func serviceCommand(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(output, "Usage: harness-gateway service install [gateway options] [--dry-run]\n       harness-gateway service <start|stop|restart>\n       harness-gateway service status [--raw]\n       harness-gateway service logs [-f] [-n 50] [--raw]\n       harness-gateway service <enable|disable> [--now]\n       harness-gateway service uninstall\n\nManage the Linux systemd user service without sudo.\nInstallation does not enable or start it. Enable/disable changes autostart;\n--now also starts/stops the service. 'status' and 'logs' are also top-level commands.\nUse '<command> --help' for options.")
		return err
	}
	switch args[0] {
	case "logs":
		return serviceLogs(ctx, args[1:], output)
	case "start", "stop", "restart", "status", "enable", "disable":
		return controlService(ctx, args, output)
	}
	if args[0] != "install" && args[0] != "uninstall" {
		return fmt.Errorf("unknown service command %q; use 'harness-gateway service --help'", args[0])
	}
	var options options
	dryRun := false
	explicitDirectory := false
	if args[0] == "install" {
		flags := gatewayFlags(&options, output, "harness-gateway service install", "json")
		flags.Lookup("directory").Usage = "working directory for CLI requests (default: private systemd runtime workspace)"
		flags.Lookup("directory").DefValue = ""
		flags.BoolVar(&dryRun, "dry-run", false, "print the unit without changing files or contacting systemd")
		if err := parseGatewayFlags(flags, &options, args[1:]); err != nil {
			return err
		}
		explicitDirectory = flags.Changed("directory")
		if options.version {
			_, err := fmt.Fprintln(output, buildinfo.String())
			return err
		}
	} else if len(args) != 1 {
		if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
			_, err := fmt.Fprintln(output, "Usage: harness-gateway service uninstall\n\nStop, disable and remove only the managed user unit. Drop-ins are preserved.")
			return err
		}
		return errors.New("service uninstall does not accept gateway options")
	}
	if runtime.GOOS != "linux" {
		return errors.New("systemd user services are supported only on Linux")
	}
	if args[0] == "uninstall" {
		path, err := userServicePath()
		if err != nil {
			return err
		}
		return uninstallService(ctx, path, output)
	}
	// An empty directory selects the workspace created by systemd at startup;
	// installation must not depend on that directory already existing.
	directory := ""
	var harnesses []harnessStatus
	var err error
	if explicitDirectory {
		directory, _, harnesses, err = prepareGateway(options)
	} else {
		_, harnesses, err = prepareBackends(options, directory)
	}
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	unit, err := renderService(executable, directory, os.Getenv("PATH"), options, harnesses)
	if err != nil {
		return err
	}
	if dryRun {
		_, err := output.Write(unit)
		return err
	}
	path, err := userServicePath()
	if err != nil {
		return err
	}
	return installService(ctx, path, unit, output)
}

func controlService(ctx context.Context, args []string, output io.Writer) error {
	action := args[0]
	flags := pflag.NewFlagSet("harness-gateway service "+action, pflag.ContinueOnError)
	flags.SetOutput(output)
	var help, now, raw bool
	flags.BoolVarP(&help, "help", "h", false, "show this help")
	if action == "status" {
		flags.BoolVar(&raw, "raw", false, "show native systemd status instead of the gateway dashboard")
	}
	if action == "enable" || action == "disable" {
		flags.BoolVar(&now, "now", false, "also start (enable) or stop (disable) the service")
	}
	flags.Usage = func() {
		fmt.Fprintf(output, "Usage: harness-gateway service %s [options]\n\nOptions:\n", action)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if help {
		flags.Usage()
		return pflag.ErrHelp
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("service %s does not accept positional arguments", action)
	}
	if err := requireManagedService(); err != nil {
		return err
	}
	if action == "status" && !raw {
		return serviceStatus(ctx, output)
	}
	if action != "status" {
		path, err := userServicePath()
		if err != nil {
			return err
		}
		if err := requireLoadedManagedService(ctx, path); err != nil {
			return err
		}
	}
	var err error
	arguments := []string{action}
	if now {
		arguments = append(arguments, "--now")
	}
	if action == "status" {
		arguments = append(arguments, "--no-pager", "--full")
	}
	if err := runSystemctl(ctx, output, append(arguments, serviceName)...); err != nil {
		return err
	}
	if action != "status" {
		_, err = fmt.Fprintf(output, "Service %s completed: %s\n", action, serviceName)
	}
	return err
}

func requireManagedService() error {
	if runtime.GOOS != "linux" {
		return errors.New("systemd user services are supported only on Linux")
	}
	path, err := userServicePath()
	if err != nil {
		return err
	}
	if _, exists, err := managedService(path); err != nil {
		return err
	} else if !exists {
		return errors.New("no managed user service is installed; run 'harness-gateway service install' first")
	}
	return nil
}

func userServicePath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(directory) {
		return "", errors.New("the user configuration directory must be absolute")
	}
	return filepath.Join(directory, "systemd", "user", serviceName), nil
}

// An empty directory selects systemd's runtime workspace. Omitting --directory
// lets the gateway resolve its normal "." default after systemd changes cwd.
func renderService(executable, directory, path string, options options, harnesses []harnessStatus) ([]byte, error) {
	if !filepath.IsAbs(executable) || (directory != "" && !filepath.IsAbs(directory)) {
		return nil, errors.New("service executable and directory must be absolute")
	}
	// systemd rejects some characters in the executable position even when
	// quoted. A fixed POSIX exec trampoline accepts the path as an argument;
	// user-controlled paths/settings are never interpolated into shell code.
	arguments := []string{"/bin/sh", "-c", `exec "$@"`, "harness-gateway", executable, "--listen", options.address,
		"--harnesses", options.harnesses, "--timeout", options.timeout.String(),
		"--concurrency", strconv.Itoa(options.concurrency), "--log-format", options.logFormat}
	workingDirectory := "%t/harness-gateway/work"
	runtimeDirectories := "harness-gateway harness-gateway/work"
	if directory != "" {
		arguments = append(arguments, "--directory", directory)
		// WorkingDirectory takes a literal path, not a quoted argument. The
		// final '/.' preserves trailing spaces/backslashes in the INI parser.
		workingDirectory = strings.ReplaceAll(directory, "%", "%%") + "/."
		runtimeDirectories = "harness-gateway"
	}
	for _, harness := range harnesses {
		arguments = append(arguments, "--"+harness.name+"-path", harness.executable)
		if harness.model != "" {
			arguments = append(arguments, "--"+harness.name+"-model", harness.model)
		}
	}
	var command []string
	for _, argument := range arguments {
		quoted, err := quoteUnitValue(argument)
		if err != nil {
			return nil, err
		}
		command = append(command, quoted)
	}
	environment, err := quoteUnitValue("PATH=" + path)
	if err != nil {
		return nil, err
	}
	// ':' disables ExecStart variable expansion. All paths/arguments are literal;
	// '%' specifiers still require escaping in every systemd value.
	unit := serviceMarker + `[Unit]
Description=Harness-Gateway local OpenAI-compatible API
Documentation=https://github.com/Harness-Gateway/Harness-Gateway
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=exec
WorkingDirectory=` + workingDirectory + `
Environment=` + environment + `
RuntimeDirectory=` + runtimeDirectories + `
RuntimeDirectoryMode=0700
Environment=TMPDIR=%t/harness-gateway
ExecStart=:` + strings.Join(command, " ") + `
Restart=on-failure
RestartSec=5s
TimeoutStopSec=15s
KillMode=mixed
UMask=0077
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
`
	if len(unit) > maxUnitBytes {
		return nil, errors.New("generated service unit exceeds 64 KiB")
	}
	return []byte(unit), nil
}

func quoteUnitValue(value string) (string, error) {
	if !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return "", errors.New("service settings must be UTF-8 without control characters")
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(value) + `"`, nil
}

func managedService(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxUnitBytes {
		return nil, false, errors.New("refusing to replace or remove an unmanaged service file or symlink")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	if !bytes.HasPrefix(contents, []byte(serviceMarker)) {
		return nil, false, errors.New("refusing to replace or remove an unmanaged service file")
	}
	return contents, true, nil
}

func writeService(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".harness-gateway-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	_, writeErr := file.Write(contents)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func systemctl(ctx context.Context, args ...string) error {
	var output bytes.Buffer
	if err := runSystemctl(ctx, &output, args...); err != nil {
		return fmt.Errorf("%w\n%s", err, &output)
	}
	return nil
}

func runSystemctl(ctx context.Context, output io.Writer, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, args...)...)
	command.WaitDelay = 2 * time.Second
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		return fmt.Errorf("systemctl --user %s: %w", strings.Join(args, " "), errors.Join(err, ctx.Err()))
	}
	return nil
}

func installService(ctx context.Context, path string, unit []byte, output io.Writer) error {
	previous, exists, err := managedService(path)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("find systemctl: %w", err)
	}
	if err := writeService(path, unit); err != nil {
		return err
	}
	if err := systemctl(ctx, "daemon-reload"); err != nil {
		var restoreErr error
		if exists {
			restoreErr = writeService(path, previous)
		} else {
			restoreErr = os.Remove(path)
		}
		return errors.Join(err, restoreErr)
	}
	_, err = fmt.Fprintf(output, "Installed user service: %s\nEnable and start: harness-gateway service enable --now\nApply updates to a running service: harness-gateway service restart\nStatus: harness-gateway status\nLogs: harness-gateway logs --follow\nBoot before login (optional): loginctl enable-linger\n", path)
	return err
}

func uninstallService(ctx context.Context, path string, output io.Writer) error {
	_, exists, err := managedService(path)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("no managed user service is installed")
	}
	if err := requireLoadedManagedService(ctx, path); err != nil {
		return err
	}
	if err := systemctl(ctx, "disable", "--now", serviceName); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, "Removed user service:", path)
	return err
}

// A local marker does not identify the unit selected by the user manager: its
// configuration path can differ, and transient units can shadow disk files.
func requireLoadedManagedService(ctx context.Context, path string) error {
	var output bytes.Buffer
	if err := runSystemctl(ctx, &output, "show", "--property=LoadState,FragmentPath", serviceName); err != nil {
		return fmt.Errorf("%w\n%s", err, &output)
	}
	properties := make(map[string]string)
	for line := range strings.SplitSeq(output.String(), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			properties[key] = value
		}
	}
	if properties["LoadState"] != "loaded" || properties["FragmentPath"] == "" {
		return errors.New("refusing to modify a service without a loaded managed unit")
	}
	managed, err := os.Stat(path)
	if err != nil {
		return err
	}
	loaded, err := os.Stat(properties["FragmentPath"])
	if err != nil || !os.SameFile(managed, loaded) {
		return fmt.Errorf("refusing to modify service loaded from %q instead of managed unit %q", properties["FragmentPath"], path)
	}
	return nil
}
