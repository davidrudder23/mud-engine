// Package logging sets up the server's log/slog output.
//
// Every package gets its logger via For("mud.<package>"), which tags
// each record with a "logger" attribute and hands it to whatever
// slog.Default() is *at the time of the call* -- so Configure (from a
// real entrypoint) and test recorders (logtest.Capture) both take
// effect for loggers created earlier at package init.
//
// Level is controlled by MUD_LOG_LEVEL (default INFO): DEBUG, INFO,
// WARNING (or WARN), ERROR, or CRITICAL, case-insensitive; an
// unrecognized value falls back to INFO rather than failing, since a
// typo'd env var shouldn't stop the server from starting. Output goes
// to stderr -- never stdout, which the MCP server uses for JSON-RPC.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// ParseLevel maps a MUD_LOG_LEVEL value to a slog.Level, falling back
// to Info for anything unrecognized.
func ParseLevel(value string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARNING", "WARN":
		return slog.LevelWarn
	case "ERROR", "CRITICAL":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Configure installs a stderr text handler as slog's default, at the
// level MUD_LOG_LEVEL names. Call it once, from a real process
// entrypoint -- library code and tests should never call it.
func Configure() {
	level := ParseLevel(os.Getenv("MUD_LOG_LEVEL"))
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}

// For returns a logger that tags every record with logger=name.
func For(name string) *slog.Logger {
	return slog.New(&namedHandler{name: name})
}

// namedHandler resolves slog.Default()'s handler at log time rather
// than capturing it when the logger is created.
type namedHandler struct {
	name string
	// Applied, in order, to the default handler at log time -- how
	// WithAttrs/WithGroup are honored despite that late binding.
	wrap []func(slog.Handler) slog.Handler
}

func (h *namedHandler) target() slog.Handler {
	target := slog.Default().Handler()
	for _, wrap := range h.wrap {
		target = wrap(target)
	}
	return target
}

func (h *namedHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return slog.Default().Handler().Enabled(ctx, level)
}

func (h *namedHandler) Handle(ctx context.Context, r slog.Record) error {
	r = r.Clone()
	r.AddAttrs(slog.String("logger", h.name))
	return h.target().Handle(ctx, r)
}

func (h *namedHandler) with(wrap func(slog.Handler) slog.Handler) *namedHandler {
	wraps := append(append([]func(slog.Handler) slog.Handler{}, h.wrap...), wrap)
	return &namedHandler{name: h.name, wrap: wraps}
}

func (h *namedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.with(func(target slog.Handler) slog.Handler { return target.WithAttrs(attrs) })
}

func (h *namedHandler) WithGroup(name string) slog.Handler {
	return h.with(func(target slog.Handler) slog.Handler { return target.WithGroup(name) })
}
