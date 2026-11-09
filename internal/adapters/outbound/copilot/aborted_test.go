package copilot

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/harness-gateway/harness-gateway/internal/adapters/inbound/openai"
	"github.com/harness-gateway/harness-gateway/internal/application"
	"github.com/harness-gateway/harness-gateway/internal/testutil"
)

func TestAbortedIdleFailsBothAcknowledgementOrders(t *testing.T) {
	for _, mode := range []string{"aborted-pre", "aborted-post"} {
		t.Run(mode, func(t *testing.T) {
			h, _, _ := testHarness(t, mode)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var text strings.Builder
			_, err := h.Complete(ctx, testRequest(), func(delta string) error { _, err := text.WriteString(delta); return err })
			if err == nil || !strings.Contains(err.Error(), "aborted before completion") || ctx.Err() != nil || text.String() != "partial" {
				t.Fatalf("abort lost: text=%q err=%v context=%v", text.String(), err, ctx.Err())
			}
		})
	}
	for _, mode := range []string{"normal", "idle-false"} {
		t.Run(mode, func(t *testing.T) {
			h, _, _ := testHarness(t, mode)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if _, err := h.Complete(ctx, testRequest(), func(string) error { return nil }); err != nil {
				t.Fatalf("false/omitted abort rejected: %v", err)
			}
		})
	}
}

func TestAbortedIdleHTTPFailureCleansUpAndRecovers(t *testing.T) {
	for _, mode := range []string{"aborted-pre", "aborted-post"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", mode, stream), func(t *testing.T) {
				h, _, pidFile := testHarness(t, mode)
				baseline, _ := eventConsumers(t)
				gateway := application.New([]application.Backend{{Model: application.Model{ID: "copilot", Provider: "copilot"}, Harness: h}}, 1)
				handler := openai.New(gateway, 5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
				request := func(stream bool) *httptest.ResponseRecorder {
					body := fmt.Sprintf(`{"model":"copilot","messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream)
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1/v1/chat/completions", strings.NewReader(body)))
					return w
				}
				w := request(stream)
				if !strings.Contains(w.Body.String(), `"code":"harness_error"`) || strings.Contains(w.Body.String(), `"finish_reason":"`) || strings.Contains(w.Body.String(), "[DONE]") {
					t.Fatalf("abort reported success: %d %s", w.Code, w.Body)
				}
				if stream {
					if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"content":"partial"`) {
						t.Fatalf("partial SSE failure lost: %d %s", w.Code, w.Body)
					}
				} else if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "partial") {
					t.Fatalf("buffered abort leaked partial success: %d %s", w.Code, w.Body)
				}
				assertEventConsumersReleased(t, baseline)
				data, err := os.ReadFile(pidFile)
				if err != nil {
					t.Fatal(err)
				}
				pid, err := strconv.Atoi(string(data))
				if err != nil {
					t.Fatal(err)
				}
				if running, err := testutil.ProcessRunning(pid); err != nil || running {
					t.Fatalf("aborted CLI retained: pid=%d running=%t err=%v", pid, running, err)
				}
				t.Setenv("HARNESS_GATEWAY_COPILOT_TEST", "normal")
				if recovered := request(false); recovered.Code != http.StatusOK || !strings.Contains(recovered.Body.String(), "hello world") {
					t.Fatalf("admission not recovered: %d %s", recovered.Code, recovered.Body)
				}
				assertEventConsumersReleased(t, baseline)
			})
		}
	}
}
