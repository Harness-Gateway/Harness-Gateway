package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/harness-gateway/harness-gateway/internal/application"
	"github.com/harness-gateway/harness-gateway/internal/domain"
)

func readRequestLogs(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(logs.Bytes()))
	for {
		var record map[string]any
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			return records
		}
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
}

func TestRequestCorrelationBeforeAdmission(t *testing.T) {
	backend := harnessFunc(func(context.Context, domain.Request, func(string) error) (domain.Result, error) {
		t.Fatal("rejected request started inference")
		return domain.Result{}, nil
	})
	var logs bytes.Buffer
	h := New(application.New([]application.Backend{{Model: application.Model{ID: "codex"}, Harness: backend}}, 1), time.Second, slog.New(slog.NewJSONHandler(&logs, nil)))
	seen := make(map[string]bool)
	for _, test := range []struct {
		method, path, body, origin, code string
		status                           int
	}{
		{"POST", "/v1/chat/completions", "{", "", "invalid_request", 400},
		{"POST", "/v1/chat/completions", `{}`, "", "unsupported_parameter", 400},
		{"DELETE", "/v1/chat/completions", "", "", "method_not_allowed", 405},
		{"GET", "/v1/models", "", "https://foreign.invalid", "forbidden_origin", 403},
	} {
		logs.Reset()
		r := localRequest(test.method, test.path, strings.NewReader(test.body))
		r.Header.Set("X-Request-ID", "untrusted-client-id")
		if test.origin != "" {
			r.Header.Set("Origin", test.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		id := w.Header().Get("X-Request-ID")
		if id == "" || id == "untrusted-client-id" || seen[id] || w.Code != test.status {
			t.Fatalf("invalid correlation/status: %q %d", id, w.Code)
		}
		seen[id] = true
		records := readRequestLogs(t, &logs)
		if len(records) != 1 || records[0]["request_id"] != id || records[0]["outcome"] != "rejected" || records[0]["code"] != test.code || records[0]["http_status"] != float64(test.status) {
			t.Fatalf("missing rejection outcome: %s", &logs)
		}
		if _, ok := records[0]["time_to_first_text"]; ok {
			t.Fatal("rejection recorded nonexistent text")
		}
	}
}

func TestCompletionOutcomesAndFirstText(t *testing.T) {
	for _, test := range []struct {
		name, outcome, code string
		stream, emit        bool
		failure             error
		cause               error
		timeout             bool
		status              int
	}{
		{name: "buffered-success", outcome: "success", emit: true, status: 200},
		{name: "stream-success", outcome: "success", stream: true, emit: true, status: 200},
		{name: "buffered-failure", outcome: "backend_error", failure: errors.New("private failure"), code: "harness_error", status: 502},
		{name: "stream-failure", outcome: "backend_error", stream: true, emit: true, failure: errors.New("private failure"), code: "harness_error", status: 200},
		{name: "timeout", outcome: "timeout", timeout: true, code: "harness_timeout", status: 504},
		{name: "stream-timeout", outcome: "timeout", stream: true, timeout: true, code: "harness_timeout", status: 200},
		{name: "canceled", outcome: "canceled", cause: context.Canceled, status: 0},
		{name: "shutdown", outcome: "shutdown", stream: true, cause: ErrShuttingDown, code: "gateway_shutting_down", status: 200},
		{name: "busy", outcome: "rejected", failure: application.ErrBusy, code: "gateway_busy", status: 429},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			backend := harnessFunc(func(ctx context.Context, _ domain.Request, emit func(string) error) (domain.Result, error) {
				if test.emit {
					if err := emit("private prompt response"); err != nil {
						return domain.Result{}, err
					}
				}
				if test.cause != nil {
					cancel(test.cause)
					return domain.Result{}, ctx.Err()
				}
				if test.timeout {
					<-ctx.Done()
					return domain.Result{}, ctx.Err()
				}
				return domain.Result{}, test.failure
			})
			var logs bytes.Buffer
			h := New(application.New([]application.Backend{{Model: application.Model{ID: "codex"}, Harness: backend}}, 1), 100*time.Millisecond, slog.New(slog.NewJSONHandler(&logs, nil)))
			w := httptest.NewRecorder()
			body := `{"model":"codex","messages":[{"role":"user","content":"private prompt"}],"stream":` + fmtBool(test.stream) + `}`
			h.ServeHTTP(w, localRequest("POST", "/v1/chat/completions", strings.NewReader(body)).WithContext(ctx))
			records := readRequestLogs(t, &logs)
			last := records[len(records)-1]
			if last["msg"] != "completion finished" || last["request_id"] != w.Header().Get("X-Request-ID") || last["outcome"] != test.outcome || last["http_status"] != float64(test.status) {
				t.Fatalf("incorrect outcome: %s", &logs)
			}
			if test.code != "" && last["code"] != test.code {
				t.Fatalf("missing error code: %v", last)
			}
			latency, hasText := last["time_to_first_text"].(float64)
			if hasText != test.emit || (hasText && (latency <= 0 || latency > last["duration"].(float64))) {
				t.Fatalf("incorrect text latency: %v", last)
			}
			if strings.Contains(logs.String(), "private prompt") {
				t.Fatal("request/response content leaked into logs")
			}
			if test.stream && test.outcome != "success" && strings.Contains(w.Body.String(), "[DONE]") {
				t.Fatal("failed stream reported success")
			}
		})
	}
}

type failingObservedWriter struct {
	*httptest.ResponseRecorder
	flushOnly bool
}

func (w failingObservedWriter) Write(data []byte) (int, error) {
	if w.flushOnly {
		return w.ResponseRecorder.Write(data)
	}
	return 0, errors.New("write failed")
}

func (w failingObservedWriter) FlushError() error { return errors.New("flush failed") }

func TestObservedResponseWriteFailures(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, flushOnly := range []bool{false, true} {
			if !stream && flushOnly {
				continue
			}
			var logs bytes.Buffer
			backend := harnessFunc(func(_ context.Context, _ domain.Request, emit func(string) error) (domain.Result, error) {
				return domain.Result{}, emit("text")
			})
			h := New(application.New([]application.Backend{{Model: application.Model{ID: "codex"}, Harness: backend}}, 1), time.Second, slog.New(slog.NewJSONHandler(&logs, nil)))
			w := failingObservedWriter{ResponseRecorder: httptest.NewRecorder(), flushOnly: flushOnly}
			h.ServeHTTP(w, localRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"codex","messages":[{"role":"user","content":"x"}],"stream":`+fmtBool(stream)+`}`)))
			records := readRequestLogs(t, &logs)
			last := records[len(records)-1]
			if last["outcome"] != "write_error" || last["code"] != "response_write_failed" {
				t.Fatalf("write/flush failure lost: %s", &logs)
			}
		}
	}
}
