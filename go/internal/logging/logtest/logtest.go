// Package logtest captures log records in tests, the Go counterpart of
// unittest's assertLogs.
package logtest

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// Record is one captured log record, with every attribute rendered as
// a string.
type Record struct {
	Level   slog.Level
	Message string
	Attrs   map[string]string
}

// String renders the record roughly like a text log line, for
// substring assertions.
func (r Record) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", r.Level, r.Message)
	for k, v := range r.Attrs {
		fmt.Fprintf(&b, " %s=%s", k, v)
	}
	return b.String()
}

// Recorder is a slog.Handler that keeps every record (at every level).
type Recorder struct {
	mu      sync.Mutex
	records []Record
	attrs   []slog.Attr
}

// Capture installs a fresh Recorder as slog's default for the rest of
// the test, restoring the previous default afterwards. Tests using it
// must not run in parallel.
func Capture(t testing.TB) *Recorder {
	t.Helper()
	previous := slog.Default()
	recorder := &Recorder{}
	slog.SetDefault(slog.New(recorder))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return recorder
}

func (r *Recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *Recorder) Handle(_ context.Context, record slog.Record) error {
	attrs := map[string]string{}
	for _, a := range r.attrs {
		attrs[a.Key] = a.Value.String()
	}
	record.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, Record{Level: record.Level, Message: record.Message, Attrs: attrs})
	return nil
}

func (r *Recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Records are shared with the parent so Records() sees everything.
	return &childRecorder{parent: r, attrs: attrs}
}

func (r *Recorder) WithGroup(string) slog.Handler { return r }

// Records returns the records logged by the named logger (see
// logging.For), or every record if logger is "".
func (r *Recorder) Records(logger string) []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Record
	for _, rec := range r.records {
		if logger == "" || rec.Attrs["logger"] == logger {
			out = append(out, rec)
		}
	}
	return out
}

// Messages returns the Message of each of logger's records, in order.
func (r *Recorder) Messages(logger string) []string {
	var out []string
	for _, rec := range r.Records(logger) {
		out = append(out, rec.Message)
	}
	return out
}

// Find returns logger's first record with this message, if any.
func (r *Recorder) Find(logger, message string) (Record, bool) {
	for _, rec := range r.Records(logger) {
		if rec.Message == message {
			return rec, true
		}
	}
	return Record{}, false
}

type childRecorder struct {
	parent *Recorder
	attrs  []slog.Attr
}

func (c *childRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (c *childRecorder) Handle(ctx context.Context, record slog.Record) error {
	record = record.Clone()
	record.AddAttrs(c.attrs...)
	return c.parent.Handle(ctx, record)
}

func (c *childRecorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &childRecorder{parent: c.parent, attrs: append(append([]slog.Attr{}, c.attrs...), attrs...)}
}

func (c *childRecorder) WithGroup(string) slog.Handler { return c }
