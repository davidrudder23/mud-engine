package logging

import (
	"context"
	"log/slog"
	"testing"

	"mudengine/internal/logging/logtest"
)

func TestParseLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"":            slog.LevelInfo,
		"INFO":        slog.LevelInfo,
		"DEBUG":       slog.LevelDebug,
		"warning":     slog.LevelWarn, // case-insensitive
		" Warn ":      slog.LevelWarn,
		"ERROR":       slog.LevelError,
		"critical":    slog.LevelError,
		"not-a-level": slog.LevelInfo, // unrecognized falls back to info
	}
	for value, want := range tests {
		if got := ParseLevel(value); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestConfigureSetsDefaultLevel(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	t.Setenv("MUD_LOG_LEVEL", "warning")
	Configure()

	ctx := context.Background()
	if slog.Default().Enabled(ctx, slog.LevelInfo) || !slog.Default().Enabled(ctx, slog.LevelWarn) {
		t.Error("default logger isn't at WARN")
	}
	// Loggers made by For follow the default's level too.
	if For("mud.test").Enabled(ctx, slog.LevelInfo) {
		t.Error("For() logger ignores the configured level")
	}
}

func TestForTagsRecordsAndBindsLate(t *testing.T) {
	logger := For("mud.test") // created before the recorder is installed
	logs := logtest.Capture(t)

	logger.With("session", "abc").Info("hello", "player", "alice")

	records := logs.Records("mud.test")
	if len(records) != 1 || records[0].Message != "hello" ||
		records[0].Attrs["player"] != "alice" || records[0].Attrs["session"] != "abc" {
		t.Errorf("records = %v", records)
	}
}
