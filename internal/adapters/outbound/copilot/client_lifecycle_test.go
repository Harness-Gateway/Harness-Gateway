package copilot

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCancellationDuringActiveSDKCalls(t *testing.T) {
	for _, mode := range []string{"create-hold", "models-hold"} {
		t.Run(mode, func(t *testing.T) {
			h, log, _ := testHarness(t, mode)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			baseline, _ := eventConsumers(t)
			done := make(chan error, 1)
			finished := make(chan struct{})
			t.Cleanup(func() {
				cancel()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("SDK cancellation fixture did not finish cleanup")
				}
			})
			go func() {
				defer close(finished)
				if mode == "models-hold" {
					_, err := h.Models(ctx)
					done <- err
				} else {
					_, err := h.Complete(ctx, testRequest(), func(string) error { return nil })
					done <- err
				}
			}()
			method := "session.create\n"
			if mode == "models-hold" {
				method = "models.list\n"
			}
			for {
				data, err := os.ReadFile(log)
				if err == nil && strings.Contains(string(data), method) {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("fixture did not reach active SDK operation")
				case <-time.After(5 * time.Millisecond):
				}
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("SDK operation retained its transport after cancellation")
			}
			assertEventConsumersReleased(t, baseline)
		})
	}
}
