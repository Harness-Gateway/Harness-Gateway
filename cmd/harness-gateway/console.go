package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/harness-gateway/harness-gateway/internal/buildinfo"
)

type harnessStatus struct {
	name, executable, model string
	version                 string
	versionError            error
}

func (h harnessStatus) installedVersion() string {
	if h.version == "" {
		return "unavailable"
	}
	return h.version
}

type startupStatus struct {
	baseURL, directory string
	timeout            time.Duration
	concurrency        int
	harnesses          []harnessStatus
}

type harnessSnapshot struct {
	Name       string `json:"name"`
	Executable string `json:"executable"`
	Model      string `json:"model"`
	Version    string `json:"version"`
}

type panelRow struct{ label, value string }

type panelDetails struct {
	version, build, repository, platform, state, stateColor, footer string
	rows                                                            []panelRow
}

func newLogger(output io.Writer, format string) *slog.Logger {
	if format == "json" {
		return slog.New(slog.NewJSONHandler(output, nil))
	}
	return slog.New(slog.NewTextHandler(output, nil))
}

func reportStartup(output *os.File, logger *slog.Logger, format string, status startupStatus) error {
	if format == "text" && term.IsTerminal(int(output.Fd())) {
		color := os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
		width, _, err := term.GetSize(int(output.Fd()))
		if err != nil || width <= 0 {
			width = 80
		}
		_, err = io.WriteString(output, startupPanel(status, color, width))
		return err
	}
	var harnesses []string
	versions := make(map[string]string, len(status.harnesses))
	details := make([]harnessSnapshot, 0, len(status.harnesses))
	for _, harness := range status.harnesses {
		harnesses = append(harnesses, harness.name)
		versions[harness.name] = harness.installedVersion()
		details = append(details, harnessSnapshot{harness.name, harness.executable, harness.model, harness.installedVersion()})
	}
	var detailValue any = details
	if format == "text" {
		// Preserve structured details in slog text output as well as JSON output.
		encoded, err := json.Marshal(details)
		if err != nil {
			return err
		}
		detailValue = string(encoded)
	}
	logger.Info("Harness-Gateway listening",
		"version", buildinfo.Version, "build", buildinfo.Revision(), "repository", buildinfo.RepositoryURL,
		"url", status.baseURL, "api", status.baseURL+"/v1", "docs", status.baseURL+"/docs",
		"openapi", status.baseURL+"/openapi.json", "health", status.baseURL+"/health",
		"harnesses", strings.Join(harnesses, ","), "harness_versions", versions,
		"harness_details", detailValue, "platform", runtime.GOOS+"/"+runtime.GOARCH, "directory", status.directory,
		"concurrency", status.concurrency, "timeout", status.timeout)
	return nil
}

func startupPanel(status startupStatus, color bool, width int) string {
	return gatewayPanel(status, panelDetails{
		version: buildinfo.Version, build: buildinfo.Revision(), repository: buildinfo.RepositoryURL,
		platform: runtime.GOOS + "/" + runtime.GOARCH, state: "LISTENING", stateColor: "1;32", footer: "Press Ctrl+C to stop.",
	}, color, width)
}

func gatewayPanel(status startupStatus, details panelDetails, color bool, width int) string {
	style := func(value, code string) string {
		if !color {
			return value
		}
		return "\x1b[" + code + "m" + value + "\x1b[0m"
	}
	var panel strings.Builder
	// Pad labels before applying ANSI styles so color codes never affect columns.
	row := func(label, value string) {
		fmt.Fprintf(&panel, "  %s %s\n", style(fmt.Sprintf("%-12s", label), "2"), value)
	}
	title := "Harness-Gateway"
	if details.version != "" {
		title += " v" + details.version
	}
	fmt.Fprintf(&panel, "\n  %s\n", style(title, "1;36"))
	fmt.Fprintf(&panel, "  Local OpenAI-compatible API  %s\n\n", style(details.state, details.stateColor))
	for _, extra := range details.rows {
		row(extra.label, extra.value)
	}
	if len(details.rows) > 0 {
		fmt.Fprintln(&panel)
	}
	for _, link := range []struct{ label, suffix string }{
		{"Gateway", ""}, {"API", "/v1"}, {"Docs", "/docs"}, {"OpenAPI", "/openapi.json"}, {"Health", "/health"},
	} {
		if status.baseURL != "" {
			row(link.label, style(status.baseURL+link.suffix, "36"))
		}
	}
	row("Repository", style(details.repository, "36"))
	if len(status.harnesses) > 0 {
		fmt.Fprintln(&panel)
		fmt.Fprintf(&panel, "  %s\n", style("Local harnesses", "1"))
		for i, line := range harnessTable(status.harnesses, width-2) {
			if i == 0 {
				line = style(line, "2")
			}
			fmt.Fprintf(&panel, "  %s\n", line)
		}
	}
	fmt.Fprintln(&panel)
	if status.directory != "" {
		row("Directory", status.directory)
	}
	if status.concurrency > 0 && status.timeout > 0 {
		row("Limits", fmt.Sprintf("%d concurrent requests / %s timeout", status.concurrency, status.timeout))
	}
	if details.build != "" {
		build := details.build
		if details.platform != "" {
			build += " (" + details.platform + ")"
		}
		row("Build", build)
	}
	fmt.Fprintf(&panel, "\n  %s\n\n", style(details.footer, "2"))
	return panel.String()
}

// Long executable paths move below their rows when the full table would exceed
// the available terminal width. Values stay complete rather than being clipped.
func harnessTable(harnesses []harnessStatus, width int) []string {
	render := func(includePaths bool) []string {
		var buffer strings.Builder
		table := tabwriter.NewWriter(&buffer, 0, 0, 2, ' ', 0)
		fmt.Fprint(table, "HARNESS\tVERSION\tDEFAULT MODEL")
		if includePaths {
			fmt.Fprint(table, "\tEXECUTABLE")
		}
		fmt.Fprintln(table)
		for _, harness := range harnesses {
			model := harness.model
			if model == "" {
				model = "CLI default"
			}
			fmt.Fprintf(table, "%s\t%s\t%s", harness.name, harness.installedVersion(), model)
			if includePaths {
				fmt.Fprintf(table, "\t%s", harness.executable)
			}
			fmt.Fprintln(table)
		}
		table.Flush()
		return strings.Split(strings.TrimSuffix(buffer.String(), "\n"), "\n")
	}
	lines := render(true)
	for _, line := range lines {
		if utf8.RuneCountInString(line) > width {
			compact := render(false)
			lines = []string{compact[0]}
			for i, harness := range harnesses {
				lines = append(lines, compact[i+1], "  "+harness.executable)
			}
			break
		}
	}
	return lines
}
