package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/harness-gateway/harness-gateway/internal/buildinfo"
	"github.com/spf13/pflag"
)

type doctorCheck struct {
	name, detail string
	err          error
	info         bool
}

func doctorCommand(parent context.Context, args []string, output io.Writer) error {
	var options options
	options.concurrency, options.logFormat = 2, "text"
	flags := pflag.NewFlagSet("harness-gateway doctor", pflag.ContinueOnError)
	flags.SetOutput(output)
	flags.BoolVarP(&options.help, "help", "h", false, "show this help")
	flags.BoolVarP(&options.version, "version", "v", false, "print version and build identity")
	flags.StringVarP(&options.address, "listen", "l", "127.0.0.1:8789", "running gateway's loopback listen address")
	flags.StringVarP(&options.harnesses, "harnesses", "H", "codex,copilot", "comma-separated enabled harnesses")
	flags.StringVarP(&options.directory, "directory", "d", ".", "CLI working directory")
	flags.StringVar(&options.codexPath, "codex-path", "codex", "Codex CLI executable")
	flags.StringVar(&options.copilotPath, "copilot-path", "copilot", "Copilot CLI executable")
	flags.DurationVarP(&options.timeout, "timeout", "t", 30*time.Second, "total diagnostic deadline (including an optional inference probe)")
	var service bool
	var probe string
	flags.BoolVar(&service, "service", false, "inspect the managed Linux service's current invocation instead of foreground settings")
	flags.StringVar(&probe, "probe", "", "also run one inference request using this model ID (uses account allowance)")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: harness-gateway doctor [options]\n\nCheck configuration, CLI versions, gateway liveness and model discovery.\nOnly --probe MODEL runs inference. --service reads current service settings.\n\nOptions:")
		flags.PrintDefaults()
	}
	if err := parseGatewayFlags(flags, &options, args); err != nil {
		return err
	}
	if options.version {
		_, err := fmt.Fprintln(output, buildinfo.String())
		return err
	}
	if options.timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	if flags.Changed("probe") && strings.TrimSpace(probe) == "" {
		return errors.New("probe requires a nonempty model ID")
	}
	if service {
		for _, name := range []string{"listen", "harnesses", "directory", "codex-path", "copilot-path"} {
			if flags.Changed(name) {
				return fmt.Errorf("--service reads running settings; --%s cannot override them", name)
			}
		}
	}
	ctx, cancel := context.WithTimeout(parent, options.timeout)
	defer cancel()
	executable, executableErr := os.Executable()
	checks := []doctorCheck{{name: "installed gateway", detail: buildinfo.String() + "; " + executable, err: executableErr}}
	baseURL := "http://" + options.address
	var harnesses []harnessStatus
	if service {
		serviceChecks, snapshot, err := doctorService(ctx)
		checks = append(checks, serviceChecks...)
		if err != nil {
			return printDoctorChecks(output, checks)
		}
		baseURL = snapshot.URL
		for _, harness := range snapshot.HarnessDetails {
			harnesses = append(harnesses, harnessStatus{name: harness.Name, executable: harness.Executable, model: harness.Model})
		}
	} else {
		directory, _, configured, err := prepareGateway(options)
		checks = append(checks, doctorCheck{name: "configuration", detail: directory, err: err})
		harnesses = configured
	}
	for _, harness := range harnesses {
		probeCtx, stop := context.WithTimeout(ctx, 20*time.Second)
		version, err := queryHarnessVersion(probeCtx, harness.name, harness.executable)
		stop()
		checks = append(checks, doctorCheck{name: harness.name + " CLI", detail: version + "; " + harness.executable, err: err})
		if service {
			path, err := exec.LookPath(harness.name)
			if err != nil || !sameExecutable(path, harness.executable) {
				// A pinned service path may intentionally differ from the shell.
				checks = append(checks, doctorCheck{name: harness.name + " PATH", detail: "shell lookup differs; service uses " + harness.executable, info: true})
			}
		}
	}
	checks = append(checks, doctorHTTPChecks(ctx, baseURL, probe)...)
	return printDoctorChecks(output, checks)
}

func doctorService(ctx context.Context) ([]doctorCheck, startupSnapshot, error) {
	var checks []doctorCheck
	if err := requireManagedService(); err != nil {
		return []doctorCheck{{name: "service", err: err}}, startupSnapshot{}, err
	}
	inspection, err := inspectService(ctx)
	if err != nil {
		return []doctorCheck{{name: "service", err: err}}, startupSnapshot{}, err
	}
	state := inspection.properties["ActiveState"] + "/" + inspection.properties["SubState"]
	if state != "active/running" {
		err = fmt.Errorf("service is %s; historical startup settings will not be probed", state)
	}
	checks = append(checks, doctorCheck{name: "service", detail: state + "; autostart " + inspection.properties["UnitFileState"], err: err})
	if err != nil {
		return checks, startupSnapshot{}, err
	}
	snapshot := inspection.snapshot
	err = inspection.snapshotErr
	if err == nil && (inspection.properties["InvocationID"] == "" || len(snapshot.HarnessDetails) == 0) {
		err = errors.New("current invocation lacks recorded harness settings; restart an updated gateway")
	}
	checks = append(checks, doctorCheck{name: "running gateway", detail: "v" + snapshot.Version + "; " + snapshot.Build + "; " + snapshot.URL, err: err})
	if err != nil {
		return checks, startupSnapshot{}, err
	}
	if snapshot.Version != buildinfo.Version {
		err = fmt.Errorf("installed v%s differs from running v%s; review the service executable and restart after upgrading", buildinfo.Version, snapshot.Version)
	}
	checks = append(checks, doctorCheck{name: "gateway version", detail: "installed and running release versions match (build identities shown above)", err: err})
	for _, harness := range snapshot.HarnessDetails {
		if (harness.Name != "codex" && harness.Name != "copilot") || !filepath.IsAbs(harness.Executable) {
			err = errors.New("current invocation has incomplete harness executable settings")
			checks = append(checks, doctorCheck{name: "service configuration", err: err})
			return checks, startupSnapshot{}, err
		}
	}
	info, directoryErr := os.Stat(snapshot.Directory)
	if directoryErr == nil && !info.IsDir() {
		directoryErr = errors.New("working directory is not a directory")
	}
	checks = append(checks, doctorCheck{name: "service directory", detail: snapshot.Directory, err: directoryErr})
	return checks, snapshot, nil
}

func sameExecutable(a, b string) bool {
	left, leftErr := os.Stat(a)
	right, rightErr := os.Stat(b)
	return leftErr == nil && rightErr == nil && os.SameFile(left, right)
}

// Never send diagnostic requests through environment proxies or redirects.
// Startup journal data and CLI flags must still pass the loopback boundary.
func doctorHTTPChecks(ctx context.Context, baseURL, probe string) []doctorCheck {
	u, err := url.Parse(baseURL)
	if err == nil && (u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/")) {
		err = errors.New("diagnostics require a plain loopback HTTP URL")
	}
	if err == nil {
		err = loopbackAddress(u.Host)
	}
	if err != nil {
		return []doctorCheck{{name: "gateway URL", err: err}}
	}
	baseURL = strings.TrimRight(baseURL, "/")
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var health struct{ Status string }
	err = doctorRequest(ctx, client, http.MethodGet, baseURL+"/health", nil, &health)
	if err == nil && health.Status != "ok" {
		err = errors.New("health response did not report ok")
	}
	checks := []doctorCheck{{name: "gateway health", detail: baseURL + "/health (liveness only)", err: err}}
	if err != nil {
		return checks
	}
	var models struct {
		Object string
		Data   []struct{ ID string }
	}
	err = doctorRequest(ctx, client, http.MethodGet, baseURL+"/v1/models", nil, &models)
	if err == nil && (models.Object != "list" || len(models.Data) == 0) {
		err = errors.New("model discovery returned no usable catalog")
	}
	if err == nil {
		for _, model := range models.Data {
			if strings.TrimSpace(model.ID) == "" {
				err = errors.New("model discovery returned an empty model ID")
				break
			}
		}
	}
	checks = append(checks, doctorCheck{name: "model discovery", detail: fmt.Sprintf("%d models/aliases; discovery does not prove inference access", len(models.Data)), err: err})
	if probe != "" {
		body, err := json.Marshal(map[string]any{"model": probe, "messages": []map[string]string{{"role": "user", "content": "Reply with OK."}}})
		var completion struct {
			Object  string
			Choices []struct{ Message struct{ Content *string } }
		}
		if err == nil {
			err = doctorRequest(ctx, client, http.MethodPost, baseURL+"/v1/chat/completions", body, &completion)
		}
		if err == nil && (completion.Object != "chat.completion" || len(completion.Choices) == 0 || completion.Choices[0].Message.Content == nil || *completion.Choices[0].Message.Content == "") {
			err = errors.New("inference returned no assistant text")
		}
		checks = append(checks, doctorCheck{name: "inference probe", detail: probe, err: err})
	}
	return checks
}

func doctorRequest(ctx context.Context, client *http.Client, method, target string, body []byte, result any) error {
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP %d; check gateway logs (request ID %s)", request.URL.Path, response.StatusCode, response.Header.Get("X-Request-ID"))
	}
	const maxDiagnosticBytes = 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(response.Body, maxDiagnosticBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxDiagnosticBytes {
		return errors.New("diagnostic response exceeds 1 MiB")
	}
	return json.Unmarshal(data, result)
}

func printDoctorChecks(output io.Writer, checks []doctorCheck) error {
	failed := false
	for _, check := range checks {
		status, detail := "PASS", check.detail
		if check.info {
			status = "INFO"
		}
		if check.err != nil {
			status, detail, failed = "FAIL", check.err.Error(), true
		}
		if _, err := fmt.Fprintf(output, "%s  %s: %s\n", status, check.name, terminalValue(detail)); err != nil {
			return err
		}
	}
	if failed {
		return errors.New("diagnostic checks failed; see the report above")
	}
	_, err := fmt.Fprintln(output, "Diagnostics passed. Inference is checked only with --probe MODEL.")
	return err
}
