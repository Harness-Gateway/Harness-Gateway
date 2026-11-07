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
)

const serviceName = "harness-gateway.service"
const serviceMarker = "# Managed by Harness-Gateway: systemd user service.\n"
const maxUnitBytes = 64 * 1024

func serviceCommand(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(output, "Usage: harness-gateway service install [gateway options] [--dry-run]\n       harness-gateway service uninstall\n\nInstall a Linux systemd user unit. Installation does not enable or start it.\nUse 'service install --help' for options. Run these commands without sudo.")
		return err
	}
	if args[0] != "install" && args[0] != "uninstall" {
		return fmt.Errorf("unknown service command %q; use 'harness-gateway service --help'", args[0])
	}
	var options options
	dryRun := false
	if args[0] == "install" {
		flags := gatewayFlags(&options, output, "harness-gateway service install", "json")
		flags.BoolVar(&dryRun, "dry-run", false, "print the unit without changing files or contacting systemd")
		if err := parseGatewayFlags(flags, &options, args[1:]); err != nil {
			return err
		}
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
	directory, _, harnesses, err := prepareGateway(options)
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

func renderService(executable, directory, path string, options options, harnesses []harnessStatus) ([]byte, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(directory) {
		return nil, errors.New("service executable and directory must be absolute")
	}
	arguments := []string{executable, "--listen", options.address, "--directory", directory,
		"--harnesses", options.harnesses, "--timeout", options.timeout.String(),
		"--concurrency", strconv.Itoa(options.concurrency), "--log-format", options.logFormat}
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
	// WorkingDirectory takes a literal path, not a quoted argument. The final
	// '/.' preserves trailing spaces/backslashes through the INI line parser.
	workingDirectory := strings.ReplaceAll(directory, "%", "%%") + "/."
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
RuntimeDirectory=harness-gateway
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, args...)...)
	command.WaitDelay = 2 * time.Second
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %w\n%s", strings.Join(args, " "), errors.Join(err, ctx.Err()), output)
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
	_, err = fmt.Fprintf(output, "Installed user service: %s\nEnable and start: systemctl --user enable --now %s\nApply updates to a running service: systemctl --user restart %s\nLogs: journalctl --user -u %s -f\nBoot before login (optional): loginctl enable-linger\n", path, serviceName, serviceName, serviceName)
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
