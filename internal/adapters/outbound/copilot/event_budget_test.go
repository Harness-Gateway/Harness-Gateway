package copilot

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
)

func budgetFrame(id string) string {
	body := fmt.Sprintf(`{"method":"session.event","params":{"event":{"id":%q,"data":{"ignored":"payload"}}}}`, id)
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
}

func TestQueuedEventBudgetRejectsBeforeReadingBody(t *testing.T) {
	first := budgetFrame("first")
	length := len(first) - strings.Index(first, "\r\n\r\n") - 4
	budget := newEventBudget(length)
	var output bytes.Buffer
	// No body follows the second header. Admission must fail with the budget
	// error, rather than reading/allocating another payload or forwarding it.
	wire := first + fmt.Sprintf("Content-Length: %d\r\n\r\n", length)
	err := copyFrames(&output, bufio.NewReader(strings.NewReader(wire)), budget)
	if !errors.Is(err, errEventBudget) || output.String() != first || budget.peak != length {
		t.Fatalf("backlog admission failed: error=%v forwarded=%d peak=%d", err, output.Len(), budget.peak)
	}
	budget.consumed("first")
	if budget.used != 0 || len(budget.pending) != 0 {
		t.Fatal("consumption retained the reservation")
	}
}

type budgetWriter func([]byte) (int, error)

func (f budgetWriter) Write(p []byte) (int, error) { return f(p) }

func TestQueuedEventBudgetRecyclesAcrossLongStream(t *testing.T) {
	frame := budgetFrame("repeated-id")
	budget := newEventBudget(len(frame))
	consumed := 0
	writer := budgetWriter(func(data []byte) (int, error) {
		if bytes.HasPrefix(data, []byte("{")) {
			budget.consumed("repeated-id")
			consumed++
		}
		return len(data), nil
	})
	err := copyFrames(writer, bufio.NewReader(strings.NewReader(strings.Repeat(frame, 256))), budget)
	if !errors.Is(err, io.EOF) || consumed != 256 || budget.used != 0 || budget.peak > budget.limit {
		t.Fatalf("budget became a cumulative stream cap: consumed=%d used=%d peak=%d error=%v", consumed, budget.used, budget.peak, err)
	}
}

func TestQueuedEventBudgetDuplicateIDsAndRPCFrames(t *testing.T) {
	budget := newEventBudget(256)
	for _, size := range []int{10, 20} {
		if err := budget.reserve(size); err != nil {
			t.Fatal(err)
		}
		budget.track("same", size)
	}
	budget.consumed("same")
	if budget.used != 20 {
		t.Fatal("duplicate event IDs lost FIFO accounting")
	}
	budget.consumed("same")
	budget.consumed("unknown")
	var output bytes.Buffer
	err := copyFrames(&output, bufio.NewReader(strings.NewReader(strings.Repeat("Content-Length: 2\r\n\r\n{}", 256))), budget)
	if !errors.Is(err, io.EOF) || budget.used != 0 {
		t.Fatalf("ordinary RPC frames retained reservations: used=%d error=%v", budget.used, err)
	}
}

func TestQueuedEventBudgetLeavesOtherRPCParamsToSDK(t *testing.T) {
	for _, body := range []string{
		`{"method":"other.notification","params":[1,2]}`,
		`{"method":"other.notification","params":{"event":[]}}`,
	} {
		budget := newEventBudget(256)
		wire := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
		var output bytes.Buffer
		err := copyFrames(&output, bufio.NewReader(strings.NewReader(wire)), budget)
		if !errors.Is(err, io.EOF) || output.String() != wire || budget.used != 0 {
			t.Fatalf("bridge interpreted unrelated RPC parameters: output=%q error=%v used=%d", output.String(), err, budget.used)
		}
	}
}

func TestQueuedEventBudgetMalformedAndTruncatedFrames(t *testing.T) {
	for _, wire := range []string{"Content-Length: 2\r\n\r\nxx", "Content-Length: 1000000\r\n\r\n{}"} {
		budget := newEventBudget(maxQueuedEventBytes)
		if err := copyFrames(io.Discard, bufio.NewReader(strings.NewReader(wire)), budget); err == nil || errors.Is(err, io.EOF) {
			t.Fatalf("invalid frame accepted: %v", err)
		}
		if budget.used != 0 {
			t.Fatal("failed frame retained its reservation")
		}
	}
}

func TestQueuedEventBudgetCoversBlockedSDKConsumer(t *testing.T) {
	h, _, _ := testHarness(t, "budget-burst")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client, _, bridge, stop, err := h.connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the real bridge/SDK with a modest budget, avoiding a large
	// deliberate allocation in CI. Change it under its lock before any event.
	bridge.events.mu.Lock()
	bridge.events.limit = 32 * 1024
	bridge.events.mu.Unlock()
	release := make(chan struct{})
	defer func() {
		close(release)
		if err := stop(); err != nil {
			// Budget exhaustion is the expected transport failure.
			if !errors.Is(err, errEventBudget) {
				t.Error(err)
			}
		}
	}()
	entered := make(chan struct{})
	var once sync.Once
	session, err := client.CreateSession(ctx, &sdk.SessionConfig{
		Streaming: sdk.Bool(true), AvailableTools: []string{}, EnableSessionStore: sdk.Bool(false),
		OnPermissionRequest: func(sdk.PermissionRequest, sdk.PermissionInvocation) (rpc.PermissionDecision, error) {
			return &rpc.PermissionDecisionReject{}, nil
		},
		OnEvent: func(event sdk.SessionEvent) {
			once.Do(func() { close(entered) })
			<-release
			bridge.events.consumed(event.ID)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, _ = session.Send(ctx, sdk.MessageOptions{Prompt: "budget"})
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("SDK consumer did not receive the first event")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !errors.Is(bridge.Err(), errEventBudget) {
		if time.Now().After(deadline) {
			t.Fatalf("blocked SDK consumer did not exhaust budget: %v", bridge.Err())
		}
		time.Sleep(time.Millisecond)
	}
	runtime.ReadMemStats(&after)
	bridge.events.mu.Lock()
	peak, limit := bridge.events.peak, bridge.events.limit
	bridge.events.mu.Unlock()
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("blocked SDK consumer: peak reserved wire bytes=%d limit=%d allocated Go bytes=%d", peak, limit, allocated)
	if peak > limit || peak < limit/2 || allocated > 16*1024*1024 || ctx.Err() != nil {
		t.Fatalf("unbounded/delayed admission: peak=%d limit=%d allocated=%d context=%v", peak, limit, allocated, ctx.Err())
	}
}

func FuzzBudgetedFrames(f *testing.F) {
	f.Add(budgetFrame("seed"))
	f.Add("Content-Length: 90000000\r\n\r\n{}")
	f.Fuzz(func(t *testing.T, wire string) {
		if len(wire) > maxConnectBytes {
			return
		}
		budget := newEventBudget(maxConnectBytes)
		_ = copyFrames(io.Discard, bufio.NewReader(strings.NewReader(wire)), budget)
		if budget.used < 0 || budget.used > budget.limit || budget.peak > budget.limit {
			t.Fatal("budget escaped its bounds")
		}
	})
}

func TestQueuedEventBudgetAllowsMaximumFrame(t *testing.T) {
	if maxQueuedEventBytes < maxProtocolFrameBytes {
		t.Fatal("queue budget rejects an otherwise valid maximum-size single frame")
	}
}
