package copilot

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Charge wire bytes before reading a frame or letting the SDK allocate it.
// Session notifications remain charged across both SDK and gateway queues,
// until the gateway takes the event for processing. This is a backlog budget,
// not an exact Go heap/RSS limit; one consumed event may also remain live.
const maxQueuedEventBytes = 128 * 1024 * 1024

var errEventBudget = errors.New("copilot queued event byte budget exceeded")

type eventBudget struct {
	mu         sync.Mutex
	limit      int
	used, peak int
	pending    map[string][]int
}

func newEventBudget(limit int) *eventBudget {
	return &eventBudget{limit: limit, pending: make(map[string][]int)}
}

func (b *eventBudget) reserve(size int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if size > b.limit-b.used {
		return errEventBudget
	}
	b.used += size
	b.peak = max(b.peak, b.used)
	return nil
}

func (b *eventBudget) release(size int) {
	b.mu.Lock()
	b.used -= size
	b.mu.Unlock()
}

func (b *eventBudget) track(id string, size int) {
	b.mu.Lock()
	b.pending[id] = append(b.pending[id], size)
	b.mu.Unlock()
}

func (b *eventBudget) consumed(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sizes := b.pending[id]
	if len(sizes) == 0 {
		return
	}
	b.used -= sizes[0]
	if len(sizes) == 1 {
		delete(b.pending, id)
	} else {
		b.pending[id] = sizes[1:]
	}
}

func copyFrames(dst io.Writer, src *bufio.Reader, budget *eventBudget) error {
	for {
		length, err := frameLength(src, maxProtocolFrameBytes)
		if err != nil {
			return err
		}
		if err := budget.reserve(length); err != nil {
			return err
		}
		// Grow only as bytes arrive: a truncated frame must not cause an
		// allocation of its entire advertised length.
		frame, err := io.ReadAll(io.LimitReader(src, int64(length)))
		if err != nil || len(frame) != length {
			budget.release(length)
			if err != nil {
				return err
			}
			return io.ErrUnexpectedEOF
		}
		var envelope struct{ Method string }
		// Only inspect routing metadata. The official SDK still decodes the
		// event payload; ignoring it here avoids a second object graph.
		if err := json.Unmarshal(frame, &envelope); err != nil {
			budget.release(length)
			return fmt.Errorf("invalid copilot protocol JSON: %w", err)
		}
		sessionEvent := envelope.Method == "session.event"
		if sessionEvent {
			var notification struct {
				Params struct{ Event struct{ ID string } }
			}
			if err := json.Unmarshal(frame, &notification); err != nil {
				budget.release(length)
				return fmt.Errorf("invalid copilot session event: %w", err)
			}
			// Register before writing: SDK delivery can race completion of Write.
			budget.track(notification.Params.Event.ID, length)
		}
		_, err = fmt.Fprintf(dst, "Content-Length: %d\r\n\r\n", length)
		if err == nil {
			var written int
			written, err = dst.Write(frame)
			if err == nil && written != len(frame) {
				err = io.ErrShortWrite
			}
		}
		if !sessionEvent {
			budget.release(length)
		}
		if err != nil {
			return err
		}
	}
}
