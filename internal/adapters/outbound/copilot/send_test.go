package copilot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEventsBeforeCreateReply(t *testing.T) {
	for _, mode := range []string{"create-burst", "create-burst-error", "create-burst-hold"} {
		t.Run(mode, func(t *testing.T) {
			h, _, _ := testHarness(t, mode)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if mode == "create-burst-hold" {
				marker := filepath.Join(t.TempDir(), "creation-burst")
				t.Setenv("HARNESS_GATEWAY_COPILOT_CREATE_BURST", marker)
				done := make(chan struct{})
				defer func() { cancel(); <-done }()
				go func() {
					defer close(done)
					for {
						if _, err := os.Stat(marker); err == nil {
							cancel()
							return
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(time.Millisecond):
						}
					}
				}()
			}
			var text strings.Builder
			_, err := h.Complete(ctx, testRequest(), func(delta string) error { text.WriteString(delta); return nil })
			switch mode {
			case "create-burst":
				if err != nil || text.String() != "hello world" {
					t.Fatalf("startup burst blocked creation or reached completion output: text=%q error=%v", text.String(), err)
				}
			case "create-burst-error":
				if err == nil || !strings.Contains(err.Error(), "create copilot session") || ctx.Err() != nil {
					t.Fatalf("startup burst hid creation failure: %v context=%v", err, ctx.Err())
				}
			case "create-burst-hold":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("startup burst failed to reach cancellation marker: %v", err)
				}
			}
		})
	}
}

func TestEventsBeforeSendReply(t *testing.T) {
	for _, mode := range []string{"pre-reply-success", "pre-reply-error", "pre-reply-hold"} {
		t.Run(mode, func(t *testing.T) {
			h, _, _ := testHarness(t, mode)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var text strings.Builder
			_, err := h.Complete(ctx, testRequest(), func(delta string) error {
				text.WriteString(delta)
				if mode == "pre-reply-hold" && text.Len() == 256 {
					cancel()
				}
				return nil
			})
			if mode != "pre-reply-error" && text.String() != strings.Repeat("x", 256) {
				t.Fatalf("early events lost: bytes=%d error=%v", text.Len(), err)
			}
			switch mode {
			case "pre-reply-success":
				if err != nil {
					t.Fatal(err)
				}
			case "pre-reply-error":
				if err == nil || !strings.Contains(err.Error(), "send copilot request") {
					t.Fatalf("idle hid a failed send: %v", err)
				}
			case "pre-reply-hold":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("pending send lost cancellation: %v", err)
				}
			}
		})
	}
}

func TestConsumerFailureWhileSendPending(t *testing.T) {
	h, _, _ := testHarness(t, "pre-reply-hold")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	want := errors.New("consumer stopped")
	_, err := h.Complete(ctx, testRequest(), func(string) error { return want })
	if !errors.Is(err, want) || ctx.Err() != nil {
		t.Fatalf("consumer failure did not cancel and join Send promptly: %v (context=%v)", err, ctx.Err())
	}
}

func TestSessionErrorWhileSendWriteBlocked(t *testing.T) {
	h, _, _ := testHarness(t, "send-write-hold")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request := testRequest()
	// This text fits the HTTP body cap but JSON's HTML escaping expands the
	// outgoing SDK frame beyond socket buffers while the CLI stops reading.
	request.Messages[1].Text = strings.Repeat("<", 900*1024)
	_, err := h.Complete(ctx, request, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "send body not read") || ctx.Err() != nil {
		t.Fatalf("blocked Send delayed the session error until outer cancellation: error=%v context=%v", err, ctx.Err())
	}
}
