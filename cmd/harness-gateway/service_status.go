package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/harness-gateway/harness-gateway/internal/buildinfo"
	"golang.org/x/term"
)

type startupSnapshot struct {
	Message        string            `json:"msg"`
	Time           string            `json:"time"`
	Version        string            `json:"version"`
	Build          string            `json:"build"`
	Repository     string            `json:"repository"`
	Platform       string            `json:"platform"`
	URL            string            `json:"url"`
	Directory      string            `json:"directory"`
	Concurrency    int               `json:"concurrency"`
	Timeout        time.Duration     `json:"timeout"`
	Harnesses      string            `json:"harnesses"`
	Versions       map[string]string `json:"harness_versions"`
	HarnessDetails []harnessSnapshot `json:"harness_details"`
}

const startupJournalPattern = `"msg"\s*:\s*"Harness-Gateway listening"|msg="Harness-Gateway listening"`

func serviceStatus(ctx context.Context, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var state bytes.Buffer
	if err := runSystemctl(ctx, &state, "show", "--property=LoadState,ActiveState,SubState,UnitFileState,MainPID,InvocationID,ActiveEnterTimestamp,FragmentPath", serviceName); err != nil {
		return fmt.Errorf("%w\n%s", err, &state)
	}
	properties := make(map[string]string)
	for line := range strings.SplitSeq(state.String(), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			properties[key] = value
		}
	}
	if properties["ActiveState"] == "" {
		return errors.New("systemd did not report the service state")
	}
	var snapshot startupSnapshot
	var snapshotErr error
	// Never substitute an older invocation's startup for a starting service.
	if properties["InvocationID"] != "" || properties["ActiveState"] == "inactive" || properties["ActiveState"] == "failed" {
		args := []string{"--user", "--unit=" + serviceName, "--boot", "--no-pager", "--output=cat", "--all", "--reverse", "--lines=1", "--grep=" + startupJournalPattern}
		if invocation := properties["InvocationID"]; invocation != "" {
			args = append(args, "_SYSTEMD_INVOCATION_ID="+invocation)
		}
		command := exec.CommandContext(ctx, "journalctl", args...)
		command.WaitDelay = 2 * time.Second
		data, err := command.Output()
		if err != nil {
			snapshotErr = fmt.Errorf("read startup journal: %w", err)
			var exit *exec.ExitError
			if errors.As(err, &exit) && len(exit.Stderr) > 0 {
				snapshotErr = fmt.Errorf("%w: %s", snapshotErr, strings.TrimSpace(string(exit.Stderr)))
			}
		} else {
			snapshot, snapshotErr = decodeStartupSnapshot(strings.TrimSpace(string(data)))
		}
	} else {
		snapshotErr = errors.New("no startup record for the current invocation yet")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	color, width := terminalPresentation(output)
	if _, err := io.WriteString(output, servicePanel(properties, snapshot, snapshotErr, color, width)); err != nil {
		return err
	}
	if properties["ActiveState"] != "active" {
		return fmt.Errorf("service is %s", properties["ActiveState"])
	}
	return nil
}

func decodeStartupSnapshot(line string) (startupSnapshot, error) {
	var snapshot startupSnapshot
	record := decodeLogRecord(line)
	if record == nil || record["msg"] != "Harness-Gateway listening" {
		return snapshot, errors.New("startup details are unavailable in the journal")
	}
	if versions, ok := record["harness_versions"].(string); ok && strings.HasPrefix(versions, "map[") && strings.HasSuffix(versions, "]") {
		parsed := make(map[string]string)
		for _, pair := range strings.Fields(strings.TrimSuffix(strings.TrimPrefix(versions, "map["), "]")) {
			if name, version, ok := strings.Cut(pair, ":"); ok {
				parsed[name] = version
			}
		}
		record["harness_versions"] = parsed
	}
	if details, ok := record["harness_details"].(string); ok {
		var decoded []harnessSnapshot
		if err := json.Unmarshal([]byte(details), &decoded); err != nil {
			return snapshot, fmt.Errorf("decode harness details: %w", err)
		}
		record["harness_details"] = decoded
	}
	data, err := json.Marshal(record)
	if err == nil {
		err = json.Unmarshal(data, &snapshot)
	}
	if err != nil {
		return snapshot, fmt.Errorf("decode startup details: %w", err)
	}
	if snapshot.URL == "" || snapshot.Version == "" {
		return snapshot, errors.New("startup journal record lacks version or listener URL")
	}
	return snapshot, nil
}

func servicePanel(properties map[string]string, snapshot startupSnapshot, snapshotErr error, color bool, width int) string {
	active, sub := properties["ActiveState"], properties["SubState"]
	state, stateColor := strings.ToUpper(active), "1;33"
	if active == "active" && sub == "running" {
		state, stateColor = "RUNNING", "1;32"
	} else if active == "failed" {
		stateColor = "1;31"
	}
	details := panelDetails{state: state, stateColor: stateColor, repository: buildinfo.RepositoryURL,
		footer: "Logs: harness-gateway logs --follow\n  Native details: harness-gateway service status --raw"}
	for _, property := range []struct{ label, key string }{
		{"Service", "ActiveState"}, {"Autostart", "UnitFileState"}, {"PID", "MainPID"}, {"Since", "ActiveEnterTimestamp"}, {"Unit", "FragmentPath"},
	} {
		value := properties[property.key]
		if property.key == "ActiveState" && sub != "" {
			value += " (" + sub + ")"
		}
		if value != "" {
			details.rows = append(details.rows, panelRow{property.label, terminalValue(value)})
		}
	}
	var status startupStatus
	if snapshotErr != nil {
		details.rows = append(details.rows, panelRow{"Startup", "Unavailable: " + terminalValue(snapshotErr.Error())})
	} else {
		details.version, details.build = terminalValue(snapshot.Version), terminalValue(snapshot.Build)
		details.platform = terminalValue(snapshot.Platform)
		if snapshot.Repository != "" {
			details.repository = terminalValue(snapshot.Repository)
		}
		status = startupStatus{baseURL: terminalValue(snapshot.URL), directory: terminalValue(snapshot.Directory), concurrency: snapshot.Concurrency, timeout: snapshot.Timeout}
		if snapshot.Time != "" {
			label := "Snapshot"
			if active != "active" {
				label = "Last startup"
			}
			details.rows = append(details.rows, panelRow{label, logTime(snapshot.Time)})
		}
		for _, harness := range snapshot.HarnessDetails {
			status.harnesses = append(status.harnesses, harnessStatus{name: terminalValue(harness.Name), executable: terminalValue(harness.Executable), model: terminalValue(harness.Model), version: terminalValue(harness.Version)})
		}
		if len(snapshot.HarnessDetails) == 0 {
			for _, name := range strings.Split(snapshot.Harnesses, ",") {
				if name = strings.TrimSpace(name); name != "" {
					status.harnesses = append(status.harnesses, harnessStatus{name: terminalValue(name), version: terminalValue(snapshot.Versions[name]), model: "not recorded", executable: "not recorded"})
				}
			}
			details.rows = append(details.rows, panelRow{"Details", "Harness paths/models were not recorded by this running version."})
		}
	}
	return gatewayPanel(status, details, color, width)
}

func terminalPresentation(output io.Writer) (bool, int) {
	width := 100
	file, ok := output.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return false, width
	}
	if columns, _, err := term.GetSize(int(file.Fd())); err == nil && columns > 0 {
		width = columns
	}
	return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb", width
}
