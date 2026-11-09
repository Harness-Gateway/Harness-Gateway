package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	api "github.com/harness-gateway/harness-gateway/internal/adapters/inbound/openai"
	"github.com/harness-gateway/harness-gateway/internal/application"
	"github.com/harness-gateway/harness-gateway/internal/domain"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("HARNESS_GATEWAY_BENCHMARK_VERSION_FIXTURE"); mode != "" {
		if mode == "hang" {
			time.Sleep(time.Minute)
		} else if len(os.Args) != 3 || os.Args[1] != "--no-auto-update" || os.Args[2] != "--version" {
			fmt.Fprintln(os.Stderr, "version probe must match bundled runtime policy")
			os.Exit(2)
		}
		fmt.Println("GitHub Copilot CLI 1.2.3.")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestVersionProbeMatchesBundledRuntimeAndCancels(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HARNESS_GATEWAY_BENCHMARK_VERSION_FIXTURE", "version")
	version, err := cliVersion(context.Background(), path, "copilot")
	if err != nil || version != "GitHub Copilot CLI 1.2.3." {
		t.Fatalf("wrong runtime: %q %v", version, err)
	}
	t.Setenv("HARNESS_GATEWAY_BENCHMARK_VERSION_FIXTURE", "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := cliVersion(ctx, path, "copilot"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("version subprocess did not observe deadline: %v", err)
	}
}

func TestVersionOutputIsBounded(t *testing.T) {
	var output versionOutput
	if _, err := output.Write(make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("x")); err == nil || len(output.data) != 4096 {
		t.Fatal("version output exceeded budget")
	}
}

type fixture struct{ fail bool }

func (f fixture) Complete(_ context.Context, _ domain.Request, emit func(string) error) (domain.Result, error) {
	for range 32 {
		if err := emit("synthetic benchmark text chunk\n"); err != nil {
			return domain.Result{}, err
		}
	}
	if f.fail {
		return domain.Result{}, errors.New("fixture failure after text")
	}
	return domain.Result{Usage: &domain.Usage{InputTokens: 10, OutputTokens: 160, TotalTokens: 170}}, nil
}

func fixtureServer(fail bool) *httptest.Server {
	gateway := application.New([]application.Backend{{Model: application.Model{ID: "codex"}, Harness: fixture{fail}}}, 1)
	return httptest.NewServer(api.New(gateway, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil))))
}

var fixturePayload = []byte(`{"model":"codex/test","messages":[{"role":"user","content":"fixture"}],"stream":true,"stream_options":{"include_usage":true}}`)

func TestMeasurementThroughProductionSSE(t *testing.T) {
	server := fixtureServer(false)
	defer server.Close()
	s := measure(context.Background(), "codex", "http", 1, false, func(ctx context.Context, emit func(string) error) (domain.Result, error) {
		return httpComplete(ctx, server.Client(), server.URL, fixturePayload, emit)
	})
	if s.Error != "" || s.FirstText == nil || s.LastText == nil || s.Usage == nil || s.EndToEndTPS == nil {
		t.Fatalf("incomplete measurement: %+v", s)
	}
	if s.Usage.OutputTokens != 160 || s.TextChunks != 32 || s.TextBytes != 32*len("synthetic benchmark text chunk\n") {
		t.Fatalf("lost output or counted role/usage chunks as text: %+v", s)
	}
	if *s.FirstText > *s.LastText || *s.LastText > s.Duration || math.Abs(*s.EndToEndTPS*s.Duration-160) > 1e-9 {
		t.Fatalf("incorrect timing or TPS: %+v", s)
	}
}

func TestStreamFailuresCannotProduceTPS(t *testing.T) {
	for name, stream := range map[string]string{
		"truncated":           "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n",
		"error":               "data: {\"error\":{\"message\":\"backend failed\"}}\n\n",
		"done without finish": "data: [DONE]\n\n",
		"malformed":           "data: invalid JSON\n\n",
		"role only":           "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
		"text without usage":  "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			s := measure(context.Background(), "codex", "http", 1, false, func(_ context.Context, emit func(string) error) (domain.Result, error) {
				return readStream(strings.NewReader(stream), emit)
			})
			if s.Error == "" || s.EndToEndTPS != nil {
				t.Fatalf("failed/incomplete stream became a TPS result: %+v", s)
			}
		})
	}
	server := fixtureServer(true)
	defer server.Close()
	s := measure(context.Background(), "codex", "http", 1, false, func(ctx context.Context, emit func(string) error) (domain.Result, error) {
		return httpComplete(ctx, server.Client(), server.URL, fixturePayload, emit)
	})
	if s.Error == "" || s.EndToEndTPS != nil || s.FirstText == nil {
		t.Fatalf("error after text became a TPS result: %+v", s)
	}
}

func TestSummaryExcludesWarmupsAndFailures(t *testing.T) {
	f := 1.0
	var samples []sample
	for i := 1; i <= 10; i++ {
		tokens := i * 100
		tps := float64(tokens) / 2
		samples = append(samples, sample{Provider: "codex", Route: "http", Duration: 2, FirstText: &f, EndToEndTPS: &tps, Usage: &domain.Usage{OutputTokens: tokens}})
	}
	samples = append(samples, sample{Provider: "codex", Route: "http", Warmup: true}, sample{Provider: "codex", Route: "http", Error: "failure"})
	g := summarize(samples)[1]
	if g.Success != 10 || g.Failures != 1 || g.TPS.Median != 275 || g.TPS.P95 != 500 || g.AggregateTPS != 275 || g.OutputTokens != 5500 {
		t.Fatalf("incorrect population or quantiles: %+v", g)
	}
	if describe([]float64{3, 1, 2}).Median != 2 || describe(nil) != (distribution{}) {
		t.Fatal("incorrect odd or empty distribution")
	}
	values := make([]float64, 20)
	for i := range values {
		values[i] = float64(i + 1)
	}
	if describe(values).P95 != 19 {
		t.Fatal("p95 must use nearest rank rather than always return the maximum")
	}
	fast, slow := 100.0, 100.0/9
	weighted := summarize([]sample{
		{Provider: "codex", Route: "http", Duration: 1, FirstText: &f, EndToEndTPS: &fast, Usage: &domain.Usage{OutputTokens: 100}},
		{Provider: "codex", Route: "http", Duration: 9, FirstText: &f, EndToEndTPS: &slow, Usage: &domain.Usage{OutputTokens: 100}},
	})[1]
	if weighted.AggregateTPS != 20 {
		t.Fatalf("aggregate must weight by request time, not average request TPS: %+v", weighted)
	}
}

// This isolates the local transport cost using deterministic synthetic chunks.
// It does not measure model throughput or CLI startup.
func BenchmarkTransport(b *testing.B) {
	for _, route := range []string{"direct", "http"} {
		b.Run(route, func(b *testing.B) {
			server := fixtureServer(false)
			defer server.Close()
			complete := func(ctx context.Context, emit func(string) error) (domain.Result, error) {
				return (fixture{}).Complete(ctx, domain.Request{}, emit)
			}
			if route == "http" {
				complete = func(ctx context.Context, emit func(string) error) (domain.Result, error) {
					return httpComplete(ctx, server.Client(), server.URL, fixturePayload, emit)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				s := measure(context.Background(), "codex", route, 1, false, complete)
				if s.Error != "" {
					b.Fatal(fmt.Sprintf("benchmark failed: %s", s.Error))
				}
			}
		})
	}
}
