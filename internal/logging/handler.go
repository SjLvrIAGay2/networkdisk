package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

type patternHandler struct {
	opts   slog.HandlerOptions
	writer io.Writer
	mu     *sync.Mutex
	attrs  []slog.Attr
}

func newPatternHandler(w io.Writer, opts *slog.HandlerOptions) *patternHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &patternHandler{
		opts:   *opts,
		writer: w,
		mu:     &sync.Mutex{},
	}
}

func (h *patternHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

func (h *patternHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	ts := r.Time.Format("2006-01-02 15:04:05")
	level := strings.ToUpper(r.Level.String())

	var traceID, typ string
	var kvPairs []string

	for _, a := range h.attrs {
		switch a.Key {
		case "__trace_id__":
			traceID = a.Value.String()
		case "__type__":
			typ = a.Value.String()
		default:
			kvPairs = append(kvPairs, a.String())
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "__trace_id__":
			traceID = a.Value.String()
		case "__type__":
			typ = a.Value.String()
		default:
			kvPairs = append(kvPairs, a.String())
		}
		return true
	})

	var kv string
	if len(kvPairs) > 0 {
		kv = " " + strings.Join(kvPairs, " ")
	}

	if traceID == "" {
		_, err := fmt.Fprintf(h.writer, "%s [TraceID=] %s [%s] %s%s\n",
			ts, level, typ, r.Message, kv)
		return err
	}
	_, err := fmt.Fprintf(h.writer, "%s [TraceID=%s] %s [%s] %s%s\n",
		ts, traceID, level, typ, r.Message, kv)
	return err
}

func (h *patternHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := *h
	h2.attrs = append(h2.attrs, attrs...)
	return &h2
}

func (h *patternHandler) WithGroup(name string) slog.Handler {
	return h
}
