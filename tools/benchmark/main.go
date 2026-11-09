// Command benchmark compares authenticated CLI completions with and without
// the gateway's loopback HTTP/SSE transport. It consumes provider allowance.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"time"

	api "github.com/harness-gateway/harness-gateway/internal/adapters/inbound/openai"
	"github.com/harness-gateway/harness-gateway/internal/adapters/outbound/codex"
	"github.com/harness-gateway/harness-gateway/internal/adapters/outbound/copilot"
	"github.com/harness-gateway/harness-gateway/internal/application"
	"github.com/harness-gateway/harness-gateway/internal/domain"
	"github.com/harness-gateway/harness-gateway/internal/process"
)

const benchmarkPrompt = "Write exactly 40 numbered lines explaining basic Go programming concepts. Each line must contain 8 to 12 words after its number. Output only those lines, without headings, code fences, tool calls, or introductory text."

type sample struct {
	Provider     string        `json:"provider"`
	Route        string        `json:"route"`
	Round        int           `json:"round"`
	Warmup       bool          `json:"warmup"`
	Started      time.Time     `json:"started"`
	Duration     float64       `json:"duration_seconds"`
	FirstText    *float64      `json:"first_text_seconds,omitempty"`
	LastText     *float64      `json:"last_text_seconds,omitempty"`
	Usage        *domain.Usage `json:"usage,omitempty"`
	EndToEndTPS  *float64      `json:"end_to_end_output_tps,omitempty"`
	TextBytes    int           `json:"text_bytes"`
	TextChunks   int           `json:"text_chunks"`
	TextSHA256   string        `json:"text_sha256"`
	FinishReason string        `json:"finish_reason,omitempty"`
	Error        string        `json:"error,omitempty"`
	started      time.Time
	hash         hash.Hash
}

func (s *sample) emit(text string) error {
	if text == "" {
		return nil
	}
	seconds := time.Since(s.started).Seconds()
	if s.FirstText == nil {
		s.FirstText = &seconds
	}
	s.LastText = &seconds
	s.TextBytes += len(text)
	s.TextChunks++
	_, err := io.WriteString(s.hash, text)
	return err
}

type distribution struct {
	Median float64 `json:"median"`
	P95    float64 `json:"p95_nearest_rank"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

type group struct {
	Provider     string       `json:"provider"`
	Route        string       `json:"route"`
	Success      int          `json:"success"`
	Failures     int          `json:"failures"`
	Duration     distribution `json:"duration_seconds"`
	FirstText    distribution `json:"first_text_seconds"`
	TPS          distribution `json:"end_to_end_output_tps"`
	AggregateTPS float64      `json:"aggregate_output_tokens_per_second"`
	OutputTokens int          `json:"output_tokens"`
}

func describe(values []float64) distribution {
	if len(values) == 0 {
		return distribution{}
	}
	v := slices.Clone(values)
	slices.Sort(v)
	mid := v[len(v)/2]
	if len(v)%2 == 0 {
		mid = (v[len(v)/2-1] + mid) / 2
	}
	return distribution{Median: mid, P95: v[int(math.Ceil(.95*float64(len(v))))-1], Min: v[0], Max: v[len(v)-1]}
}

func summarize(samples []sample) []group {
	var groups []group
	for _, provider := range []string{"codex", "copilot"} {
		for _, route := range []string{"direct", "http"} {
			g := group{Provider: provider, Route: route}
			var durations, first, tps []float64
			var seconds float64
			for _, s := range samples {
				if s.Warmup || s.Provider != provider || s.Route != route {
					continue
				}
				if s.Error != "" {
					g.Failures++
					continue
				}
				g.Success++
				durations = append(durations, s.Duration)
				first = append(first, *s.FirstText)
				tps = append(tps, *s.EndToEndTPS)
				g.OutputTokens += s.Usage.OutputTokens
				seconds += s.Duration
			}
			g.Duration, g.FirstText, g.TPS = describe(durations), describe(first), describe(tps)
			if seconds > 0 {
				g.AggregateTPS = float64(g.OutputTokens) / seconds
			}
			groups = append(groups, g)
		}
	}
	return groups
}

// A role-only SSE event is not text. Require both a successful finish and DONE;
// EOF, missing usage, and stream errors must not become fast benchmark results.
func readStream(body io.Reader, emit func(string) error) (domain.Result, error) {
	var result domain.Result
	scanner := bufio.NewScanner(io.LimitReader(body, 128<<20))
	scanner.Buffer(make([]byte, 64<<10), 100<<20)
	var data []string
	finished := false
	dispatch := func() (bool, error) {
		if len(data) == 0 {
			return false, nil
		}
		payload := strings.Join(data, "\n")
		data = nil
		if payload == "[DONE]" {
			if !finished {
				return false, errors.New("DONE without successful finish")
			}
			return true, nil
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				Input  int `json:"prompt_tokens"`
				Output int `json:"completion_tokens"`
				Total  int `json:"total_tokens"`
			} `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return false, err
		}
		if len(event.Error) != 0 && string(event.Error) != "null" {
			return false, fmt.Errorf("SSE error: %s", event.Error)
		}
		for _, choice := range event.Choices {
			if err := emit(choice.Delta.Content); err != nil {
				return false, err
			}
			if choice.FinishReason != nil {
				result.FinishReason = *choice.FinishReason
				finished = true
			}
		}
		if event.Usage != nil {
			result.Usage = &domain.Usage{InputTokens: event.Usage.Input, OutputTokens: event.Usage.Output, TotalTokens: event.Usage.Total}
		}
		return false, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			done, err := dispatch()
			if err != nil || done {
				return result, err
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	return result, io.ErrUnexpectedEOF
}

func httpComplete(ctx context.Context, client *http.Client, url string, payload []byte, emit func(string) error) (domain.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return domain.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return domain.Result{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return domain.Result{}, fmt.Errorf("HTTP %d: %s", res.StatusCode, body)
	}
	return readStream(res.Body, emit)
}

func measure(ctx context.Context, provider, route string, round int, warmup bool, complete func(context.Context, func(string) error) (domain.Result, error)) sample {
	s := sample{Provider: provider, Route: route, Round: round, Warmup: warmup, Started: time.Now().UTC(), started: time.Now(), hash: sha256.New()}
	result, err := complete(ctx, s.emit)
	s.Duration = time.Since(s.started).Seconds()
	s.Usage, s.FinishReason = result.Usage, result.FinishReason
	s.TextSHA256 = hex.EncodeToString(s.hash.Sum(nil))
	if err == nil {
		switch {
		case s.FirstText == nil:
			err = errors.New("no assistant text")
		case result.Usage == nil || result.Usage.OutputTokens <= 0:
			err = errors.New("missing positive provider output-token usage; TPS unavailable")
		case result.FinishReason != "" && result.FinishReason != "stop":
			err = fmt.Errorf("unsuccessful finish: %s", result.FinishReason)
		}
	}
	if err != nil {
		s.Error = err.Error()
	} else {
		tps := float64(result.Usage.OutputTokens) / s.Duration
		s.EndToEndTPS = &tps
	}
	return s
}

func saveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

type versionOutput struct{ data []byte }

func (v *versionOutput) Write(data []byte) (int, error) {
	if len(v.data)+len(data) > 4096 {
		return 0, errors.New("CLI version output exceeds 4 KiB")
	}
	v.data = append(v.data, data...)
	return len(data), nil
}

func cliVersion(ctx context.Context, path, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	args := []string{"--version"}
	if name == "copilot" {
		// The adapter uses the bundled runtime, not an automatically selected
		// update. Query that exact runtime for meaningful benchmark provenance.
		args = []string{"--no-auto-update", "--version"}
	}
	cmd := exec.Command(path, args...)
	cmd.WaitDelay = 200 * time.Millisecond
	var output versionOutput
	cmd.Stdout, cmd.Stderr = &output, io.Discard
	child, err := process.Start(cmd)
	if err != nil {
		return "", err
	}
	stopOnCancel := context.AfterFunc(ctx, func() { _ = child.Stop() })
	waitErr := child.Wait()
	stopOnCancel()
	if err := errors.Join(ctx.Err(), waitErr, child.Stop()); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output.data)), nil
}

func main() {
	live := flag.Bool("live", false, "required: use authenticated providers and account allowance")
	count := flag.Int("samples", 10, "measured requests per provider and route (1..100)")
	warmups := flag.Int("warmups", 1, "excluded warmup requests per provider and route (0..10)")
	model := flag.String("model", "gpt-6.1-sol", "explicit native model supported by both providers")
	effort := flag.String("reasoning-effort", "low", "reasoning effort used on both routes")
	timeout := flag.Duration("timeout", 3*time.Minute, "timeout per request")
	output := flag.String("output-dir", "", "required: new directory for metadata, samples and summaries")
	flag.Parse()
	if !*live || *output == "" || *model == "" || *count < 1 || *count > 100 || *warmups < 0 || *warmups > 10 || *timeout <= 0 || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "require -live, -output-dir NEW_DIRECTORY, nonempty -model, samples 1..100, warmups 0..10, positive timeout, and no positional arguments")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, *output, *model, *effort, *count, *warmups, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, output, model, effort string, count, warmups int, timeout time.Duration) (runErr error) {
	paths := map[string]string{}
	versions := map[string]string{}
	for _, name := range []string{"codex", "copilot"} {
		path, err := exec.LookPath(name)
		if err != nil {
			return err
		}
		paths[name] = path
		vctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		version, err := cliVersion(vctx, path, name)
		cancel()
		if err != nil {
			return fmt.Errorf("%s version: %w", name, err)
		}
		versions[name] = version
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return err
	}
	defer func() {
		status := map[string]any{"finished": time.Now().UTC(), "passed": runErr == nil}
		if runErr != nil {
			status["error"] = runErr.Error()
		}
		runErr = errors.Join(runErr, saveJSON(filepath.Join(output, "result.json"), status))
	}()
	directory, err := os.MkdirTemp("", "harness-gateway-benchmark-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	build, _ := debug.ReadBuildInfo()
	metadata := map[string]any{"started": time.Now().UTC(), "go_version": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "logical_cpus": runtime.NumCPU(), "build": build, "cli_paths": paths, "cli_versions": versions, "model": model, "reasoning_effort": effort, "prompt": benchmarkPrompt, "samples_per_group": count, "warmups_per_group": warmups, "timeout_seconds": timeout.Seconds(), "concurrency": 1, "baseline": "same provider adapters and per-request CLI protocols, bypassing application/HTTP; not interactive CLI mode", "http": "in-process production handler over numeric loopback TCP/SSE; service and installed binary not used", "tps_definition": "provider output-token usage / full request wall seconds, including CLI startup, inference and cleanup; may include reasoning tokens; not visible-text decoding TPS", "p95_definition": "nearest rank, descriptive only; small sample sizes have weak tail precision"}
	if err := saveJSON(filepath.Join(output, "metadata.json"), metadata); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(output, "gateway.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	harnesses := map[string]domain.Harness{"codex": &codex.Harness{Path: paths["codex"], Directory: directory}, "copilot": &copilot.Harness{Path: paths["copilot"], Directory: directory}}
	var backends []application.Backend
	for _, name := range []string{"codex", "copilot"} {
		backends = append(backends, application.Backend{Model: application.Model{ID: name, Provider: name}, Harness: harnesses[name]})
	}
	server := httptest.NewServer(api.New(application.New(backends, 1), timeout, slog.New(slog.NewJSONHandler(log, nil))))
	defer server.Close()
	client := server.Client()
	client.Transport.(*http.Transport).Proxy = nil
	defer client.CloseIdleConnections()
	f, err := os.OpenFile(filepath.Join(output, "samples.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	encoder := json.NewEncoder(f)
	var samples []sample
	for round := 0; round < warmups+count; round++ {
		// Interleave all four groups, reversing order on alternating rounds.
		// Both providers are interleaved; each route runs first equally often.
		order := []int{0, 1, 2, 3}
		if round%2 != 0 {
			slices.Reverse(order)
		}
		for _, index := range order {
			if err := ctx.Err(); err != nil {
				return err
			}
			provider := []string{"codex", "copilot"}[index%2]
			route := []string{"direct", "http"}[index/2]
			request := domain.Request{Model: model, ReasoningEffort: effort, Messages: []domain.Message{{Role: "user", Text: benchmarkPrompt}}}
			payload, err := json.Marshal(map[string]any{"model": provider + "/" + model, "reasoning_effort": effort, "messages": []map[string]string{{"role": "user", "content": benchmarkPrompt}}, "stream": true, "stream_options": map[string]bool{"include_usage": true}})
			if err != nil {
				return err
			}
			complete := func(ctx context.Context, emit func(string) error) (domain.Result, error) {
				return harnesses[provider].Complete(ctx, request, emit)
			}
			if route == "http" {
				complete = func(ctx context.Context, emit func(string) error) (domain.Result, error) {
					return httpComplete(ctx, client, server.URL, payload, emit)
				}
			}
			rctx, cancel := context.WithTimeout(ctx, timeout)
			s := measure(rctx, provider, route, round-warmups+1, round < warmups, complete)
			cancel()
			samples = append(samples, s)
			if err := encoder.Encode(s); err != nil {
				return err
			}
			if err := f.Sync(); err != nil {
				return err
			}
			if err := saveJSON(filepath.Join(output, "summary.json"), summarize(samples)); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "%s %-6s round=%d warmup=%t elapsed=%.3fs tokens=%v error=%q\n", provider, route, s.Round, s.Warmup, s.Duration, s.Usage, s.Error)
			if s.Error != "" {
				return errors.New("benchmark request failed; see retained samples (no automatic retries)")
			}
		}
	}
	return nil
}
