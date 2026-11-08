package openai

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// The observer stays outside origin protection so denied requests and invalid
// methods receive the same correlation ID and final record as admitted work.
func observeRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event string
		switch r.URL.Path {
		case "/v1/chat/completions":
			event = "completion finished"
		case "/v1/models":
			event = "model discovery finished"
		default:
			next.ServeHTTP(w, r)
			return
		}
		observed := &observedResponse{ResponseWriter: w, id: newID(), started: time.Now(), outcome: "success"}
		w.Header().Set("X-Request-ID", observed.id)
		returned := false
		defer func() {
			if !returned {
				observed.outcome, observed.code = "backend_error", "handler_panic"
			} else if errors.Is(context.Cause(r.Context()), ErrShuttingDown) {
				observed.outcome, observed.code = "shutdown", "gateway_shutting_down"
			} else if errors.Is(r.Context().Err(), context.DeadlineExceeded) {
				observed.outcome = "timeout"
			} else if r.Context().Err() != nil {
				observed.outcome = "canceled"
			}
			args := []any{"request_id", observed.id, "method", r.Method, "path", r.URL.Path,
				"http_status", observed.status, "outcome", observed.outcome, "duration", time.Since(observed.started)}
			if observed.code != "" {
				args = append(args, "code", observed.code)
			}
			if observed.model != "" {
				args = append(args, "model", observed.model, "stream", observed.stream)
			}
			if observed.firstText > 0 {
				args = append(args, "time_to_first_text", observed.firstText)
			}
			logger.InfoContext(r.Context(), event, args...)
		}()
		next.ServeHTTP(observed, r)
		returned = true
	})
}

type observedResponse struct {
	http.ResponseWriter
	id, model, outcome, code string
	stream                   bool
	status                   int
	started                  time.Time
	firstText                time.Duration
}

// Unwrap preserves ResponseController's socket write deadlines. FlushError
// records streaming flush failures without hiding the underlying controller.
func (w *observedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *observedResponse) WriteHeader(status int) {
	if status >= 200 {
		if w.status != 0 {
			return
		}
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *observedResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.recordWriteError(err)
	return n, err
}

func (w *observedResponse) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	err := http.NewResponseController(w.ResponseWriter).Flush()
	w.recordWriteError(err)
	return err
}

func (w *observedResponse) recordWriteError(err error) {
	if err != nil {
		w.outcome, w.code = "write_error", "response_write_failed"
	}
}

func observeFailure(w http.ResponseWriter, status int, code string) {
	if observed, ok := w.(*observedResponse); ok {
		observed.code = code
		switch {
		case status == http.StatusGatewayTimeout:
			observed.outcome = "timeout"
		case code == "gateway_shutting_down":
			observed.outcome = "shutdown"
		case status >= 500:
			observed.outcome = "backend_error"
		default:
			observed.outcome = "rejected"
		}
	}
}
