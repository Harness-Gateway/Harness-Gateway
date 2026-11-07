package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/pflag"
)

func serviceLogs(ctx context.Context, args []string, output io.Writer) error {
	flags := pflag.NewFlagSet("harness-gateway service logs", pflag.ContinueOnError)
	flags.SetOutput(output)
	var follow, raw, help bool
	var lines int
	flags.BoolVarP(&help, "help", "h", false, "show this help")
	flags.BoolVarP(&follow, "follow", "f", false, "follow new entries until Ctrl+C")
	flags.IntVarP(&lines, "lines", "n", 50, "number of recent journal entries (0 to 10000)")
	flags.BoolVar(&raw, "raw", false, "print original journal messages without formatting")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: harness-gateway service logs [options]\n       harness-gateway logs [options]\n\nOptions:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if help {
		flags.Usage()
		return pflag.ErrHelp
	}
	if flags.NArg() != 0 || lines < 0 || lines > 10000 {
		return errors.New("logs accepts no positional arguments; --lines must be between 0 and 10000")
	}
	if err := requireManagedService(); err != nil {
		return err
	}
	if !follow {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	format := "json"
	if raw {
		format = "cat"
	}
	arguments := []string{"--user", "--unit=" + serviceName, "--no-pager", "--all", "--output=" + format, "--lines=" + strconv.Itoa(lines)}
	if follow {
		arguments = append(arguments, "--follow")
	}
	command := exec.CommandContext(ctx, "journalctl", arguments...)
	command.WaitDelay = 2 * time.Second
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	finish := func(err error) error {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil // Ctrl+C ends a log viewer normally.
		}
		if err != nil {
			return fmt.Errorf("journalctl: %w\n%s", errors.Join(err, ctx.Err()), terminalValue(diagnostics.String()))
		}
		if diagnostics.Len() > 0 {
			_, err = fmt.Fprintln(output, terminalValue(strings.TrimSpace(diagnostics.String())))
		}
		return err
	}
	if raw {
		command.Stdout = output
		return finish(command.Run())
	}
	reader, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := command.Start(); err != nil {
		return finish(err)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		if _, err := fmt.Fprintln(output, formatJournalLine(scanner.Text())); err != nil {
			cancel()
			_ = reader.Close()
			_ = command.Wait()
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		cancel()
		_ = reader.Close()
		_ = command.Wait()
		return fmt.Errorf("read journal entry: %w", err)
	}
	return finish(command.Wait())
}

// JSON and slog text records share the same display and startup decoding path.
func decodeLogRecord(line string) map[string]any {
	var record map[string]any
	if strings.HasPrefix(strings.TrimSpace(line), "{") {
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		if decoder.Decode(&record) != nil || decoder.Decode(new(any)) != io.EOF {
			return nil
		}
		if _, ok := record["msg"].(string); !ok {
			return nil
		}
		return record
	}
	record = make(map[string]any)
	for line = strings.TrimSpace(line); line != ""; line = strings.TrimSpace(line) {
		equals := strings.IndexByte(line, '=')
		space := strings.IndexAny(line, " \t")
		if equals <= 0 || space >= 0 && space < equals {
			return nil
		}
		key := line[:equals]
		line = line[equals+1:]
		var value string
		if strings.HasPrefix(line, `"`) {
			end := 1
			for ; end < len(line); end++ {
				if line[end] == '\\' {
					end++
				} else if line[end] == '"' {
					break
				}
			}
			if end >= len(line) {
				return nil
			}
			decoded, err := strconv.Unquote(line[:end+1])
			if err != nil {
				return nil
			}
			value, line = decoded, line[end+1:]
		} else if end := strings.IndexAny(line, " \t"); end >= 0 {
			value, line = line[:end], line[end:]
		} else {
			value, line = line, ""
		}
		record[key] = value
		if key == "duration" || key == "timeout" {
			if duration, err := time.ParseDuration(value); err == nil {
				record[key] = json.Number(strconv.FormatInt(int64(duration), 10))
			}
		} else if key == "concurrency" {
			if _, err := strconv.Atoi(value); err == nil {
				record[key] = json.Number(value)
			}
		}
	}
	if _, ok := record["msg"].(string); !ok {
		return nil
	}
	return record
}

func formatJournalLine(line string) string {
	message, timestamp, level := line, "", "INFO"
	var entry struct {
		Message   json.RawMessage `json:"MESSAGE"`
		Timestamp string          `json:"__REALTIME_TIMESTAMP"`
		Priority  string          `json:"PRIORITY"`
	}
	if json.Unmarshal([]byte(line), &entry) == nil && len(entry.Message) > 0 {
		if string(entry.Message) == "null" {
			message = "[journal message unavailable]"
		} else if json.Unmarshal(entry.Message, &message) != nil {
			var binary []byte
			if json.Unmarshal(entry.Message, &binary) == nil {
				message = string(binary)
			} else {
				message = "[unreadable journal message]"
			}
		}
		if micros, err := strconv.ParseInt(entry.Timestamp, 10, 64); err == nil {
			timestamp = time.UnixMicro(micros).Local().Format("2006-01-02 15:04:05")
		}
		switch entry.Priority {
		case "0", "1", "2", "3":
			level = "ERROR"
		case "4":
			level = "WARN"
		case "7":
			level = "DEBUG"
		}
	}
	var fields []string
	if record := decodeLogRecord(message); record != nil {
		if value, ok := record["time"].(string); ok {
			timestamp = logTime(value)
		}
		if value, ok := record["level"].(string); ok {
			level = value
		}
		if value, ok := record["msg"].(string); ok {
			message = value
		}
		keys := make([]string, 0, len(record))
		if message == "Harness-Gateway listening" {
			keys = []string{"version", "url", "docs", "harness_versions"}
		} else {
			for key := range record {
				if key != "msg" && key != "time" && key != "level" {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
		}
		for _, key := range keys {
			value, exists := record[key]
			if !exists {
				continue
			}
			text, ok := value.(string)
			if !ok {
				if encoded, err := json.Marshal(value); err == nil {
					text = string(encoded)
				} else {
					text = fmt.Sprint(value)
				}
			}
			if key == "duration" || key == "timeout" {
				if nanos, err := strconv.ParseInt(text, 10, 64); err == nil {
					text = time.Duration(nanos).String()
				}
			}
			if strings.ContainsAny(text, " \t\r\n") {
				text = strconv.QuoteToGraphic(text)
			}
			fields = append(fields, terminalValue(key)+"="+terminalValue(text))
		}
	}
	result := fmt.Sprintf("%-19s %-5s %s", timestamp, terminalValue(level), terminalValue(message))
	if len(fields) > 0 {
		result += "  " + strings.Join(fields, " ")
	}
	return strings.TrimSpace(result)
}

func logTime(value string) string {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.Local().Format("2006-01-02 15:04:05")
	}
	return terminalValue(value)
}

func terminalValue(value string) string {
	var safe strings.Builder
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			quoted := strconv.QuoteRuneToGraphic(character)
			safe.WriteString(quoted[1 : len(quoted)-1])
		} else {
			safe.WriteRune(character)
		}
	}
	return safe.String()
}
