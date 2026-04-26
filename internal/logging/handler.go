package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type patternHandler struct {
	opts   slog.HandlerOptions
	writer io.Writer
	mu     *sync.Mutex
	attrs  []slog.Attr
	groups []string
}

func newPatternHandler(w io.Writer, opts *slog.HandlerOptions) *patternHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	opts.AddSource = true
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
	gid := goid()
	level := fmt.Sprintf("%-5s", r.Level.String())

	source := ""
	if r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		source = fmt.Sprintf("%s:%d", f.File, f.Line)
		if len(source) > 36 {
			source = source[len(source)-36:]
		}
	}

	var attrsBuf strings.Builder
	for _, a := range h.attrs {
		attrsBuf.WriteString(" ")
		attrsBuf.WriteString(a.String())
	}
	r.Attrs(func(a slog.Attr) bool {
		attrsBuf.WriteString(" ")
		attrsBuf.WriteString(a.String())
		return true
	})

	if source != "" {
		_, err := fmt.Fprintf(h.writer, "%s [%d] %-5s %36s - %s%s\n",
			ts, gid, level, source, r.Message, attrsBuf.String())
		return err
	}
	_, err := fmt.Fprintf(h.writer, "%s [%d] %-5s - %s%s\n",
		ts, gid, level, r.Message, attrsBuf.String())
	return err
}

func (h *patternHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := *h
	h2.attrs = append(h2.attrs, attrs...)
	return &h2
}

func (h *patternHandler) WithGroup(name string) slog.Handler {
	h2 := *h
	h2.groups = append(h2.groups, name)
	return &h2
}

func goid() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	id, _ := strconv.ParseUint(idField, 10, 64)
	return id
}
