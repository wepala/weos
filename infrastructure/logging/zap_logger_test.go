package logging

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestZapLogger_DebugRespectsLevel proves that LOG_LEVEL=info silences Debug
// calls (the KG path uses Debug heavily; operators rely on this contract to
// keep production logs quiet) while LOG_LEVEL=debug surfaces them.
func TestZapLogger_DebugRespectsLevel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		level     zapcore.Level
		wantDebug int
		wantInfo  int
	}{
		{"info level drops debug", zapcore.InfoLevel, 0, 1},
		{"debug level keeps debug", zapcore.DebugLevel, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core, recorded := observer.New(tc.level)
			zl := NewZapLogger(zap.New(core))

			ctx := context.Background()
			zl.Debug(ctx, "kg projecting", "id", "urn:product:1")
			zl.Info(ctx, "kg backfill complete", "loaded", 42)

			debugCount, infoCount := 0, 0
			for _, e := range recorded.All() {
				switch e.Level {
				case zapcore.DebugLevel:
					debugCount++
				case zapcore.InfoLevel:
					infoCount++
				}
			}
			if debugCount != tc.wantDebug {
				t.Errorf("debug count = %d, want %d", debugCount, tc.wantDebug)
			}
			if infoCount != tc.wantInfo {
				t.Errorf("info count = %d, want %d", infoCount, tc.wantInfo)
			}
		})
	}
}

// TestZapLogger_KvFieldsParsed confirms structured kv pairs survive into
// the recorded log fields — the KG code logs structured key/value pairs
// (predicate, count, elapsed_ms) and a regression here would silently
// turn structured logs into unsearchable strings.
func TestZapLogger_KvFieldsParsed(t *testing.T) {
	t.Parallel()
	core, recorded := observer.New(zapcore.DebugLevel)
	zl := NewZapLogger(zap.New(core))

	zl.Debug(context.Background(), "oxigraph query",
		"form", "select", "elapsed_ms", int64(42))

	entries := recorded.All()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if got := fields["form"]; got != "select" {
		t.Errorf("form field = %v, want \"select\"", got)
	}
	if got := fields["elapsed_ms"]; got != int64(42) {
		t.Errorf("elapsed_ms field = %v, want 42", got)
	}
}

// TestZapLevelForIsCaseInsensitive: comparing the raw string would leave
// LOG_LEVEL=ERROR falling through to info, which is the bug this mapping
// exists to remove.
func TestZapLevelForIsCaseInsensitive(t *testing.T) {
	// The padded " Debug " key is deliberate: it proves the mapping trims
	// whitespace as well as folding case.
	cases := map[string]zapcore.Level{
		"debug": zapcore.DebugLevel, "DEBUG": zapcore.DebugLevel, " Debug ": zapcore.DebugLevel, //nolint:gocritic // mapKey: the padding is the input under test
		"warn": zapcore.WarnLevel, "WARN": zapcore.WarnLevel, "Warning": zapcore.WarnLevel,
		"error": zapcore.ErrorLevel, "ERROR": zapcore.ErrorLevel, "Error": zapcore.ErrorLevel,
		"fatal": zapcore.FatalLevel, "panic": zapcore.PanicLevel,
		"info": zapcore.InfoLevel, "": zapcore.InfoLevel,
		// An unrecognized value falls back to info rather than failing the
		// boot: a typo in an environment variable must not stop a server.
		"chatty": zapcore.InfoLevel,
	}
	for in, want := range cases {
		if got := zapLevelFor(in); got != want {
			t.Errorf("zapLevelFor(%q) = %v, want %v", in, got, want)
		}
	}
}
