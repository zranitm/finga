package logger_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/slogtest"
	"time"

	"go.uber.org/zap/zapcore"

	"finga/pkg/logger"
)

func TestTheHandlerPassesSlogtest(t *testing.T) {
	var buf *bytes.Buffer
	slogtest.Run(t, func(t *testing.T) slog.Handler {
		var log *slog.Logger
		log, buf = newLogger(t, jsonAt("debug"))
		return log.Handler()
	}, func(t *testing.T) map[string]any {
		got := entries(t, buf)
		if len(got) != 1 {
			t.Fatalf("%d entries, want 1: %s", len(got), buf)
		}
		return got[0]
	})
}

func TestSlogLevelsMapOntoZapLevels(t *testing.T) {
	tests := []struct {
		level slog.Level
		want  string
	}{
		{slog.LevelDebug - 4, "DEBUG"},
		{slog.LevelDebug, "DEBUG"},
		{slog.LevelInfo, "INFO"},
		{slog.LevelInfo + 2, "INFO"},
		{slog.LevelWarn, "WARN"},
		{slog.LevelError, "ERROR"},
		{slog.LevelError + 4, "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			log, buf := newLogger(t, jsonAt("debug"))
			log.Log(context.Background(), tt.level, "leveled")
			if got := entries(t, buf); len(got) != 1 || got[0]["level"] != tt.want {
				t.Fatalf("entries %v, want level %s", got, tt.want)
			}
		})
	}
}

func TestTheConfiguredLevelFilters(t *testing.T) {
	tests := []struct {
		name  string
		level string
		logAt slog.Level
		kept  bool
	}{
		{"debug record dropped at info", "info", slog.LevelDebug, false},
		{"info record kept at info", "info", slog.LevelInfo, true},
		{"warn record kept at info", "info", slog.LevelWarn, true},
		{"info record dropped at warn", "warn", slog.LevelInfo, false},
		{"warn record dropped at error", "error", slog.LevelWarn, false},
		{"debug record kept at debug", "debug", slog.LevelDebug, true},
		{"the level's case does not matter", "WARN", slog.LevelWarn, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, buf := newLogger(t, jsonAt(tt.level))
			if got := log.Enabled(context.Background(), tt.logAt); got != tt.kept {
				t.Fatalf("Enabled = %v, want %v", got, tt.kept)
			}
			log.Log(context.Background(), tt.logAt, "filtered")
			if kept := buf.Len() > 0; kept != tt.kept {
				t.Fatalf("kept = %v, want %v: %s", kept, tt.kept, buf)
			}
		})
	}
}

type point struct{ X, Y int }

type password string

func (password) LogValue() slog.Value { return slog.StringValue("***") }

func TestEveryKindOfValueIsEncoded(t *testing.T) {
	log, buf := newLogger(t, jsonAt("info"))
	log.Info("values",
		"bool", true, "int", -3, "uint", uint64(7), "float", 1.5, "string", "s",
		"duration", 1500*time.Millisecond, "err", errors.New("boom"),
		"struct", point{1, 2}, "nil", nil, "password", password("hunter2"),
		slog.Group("empty"), slog.Group("only_empty", slog.Group("nothing")),
		slog.Group("", "inline", 1),
		slog.Group("outer", "a", 1, slog.Group("inner", "b", 2)),
	)
	got := entries(t, buf)
	if len(got) != 1 {
		t.Fatalf("%d entries, want 1: %s", len(got), buf)
	}
	delete(got[0], "time")
	want := map[string]any{
		"level": "INFO", "msg": "values",
		"bool": true, "int": -3.0, "uint": 7.0, "float": 1.5, "string": "s",
		"duration": 1.5e9, "err": "boom",
		"struct": map[string]any{"X": 1.0, "Y": 2.0}, "nil": nil, "password": "***",
		"inline": 1.0,
		"outer":  map[string]any{"a": 1.0, "inner": map[string]any{"b": 2.0}},
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("got  %v\nwant %v", got[0], want)
	}
}

func TestATimeValueUsesTheEntryTimeLayout(t *testing.T) {
	log, buf := newLogger(t, jsonAt("info"))
	log.Info("timed", "at", time.Date(2026, 11, 1, 12, 30, 0, 5e6, time.UTC))
	if got := entries(t, buf)[0]["at"]; got != "2026-11-01T12:30:00.005Z" {
		t.Fatalf("at = %v", got)
	}
}

func TestGroupsOpenOnlyOnceTheyHoldAField(t *testing.T) {
	log, buf := newLogger(t, jsonAt("info"))
	req := log.WithGroup("req")
	req.Info("nothing in the group")
	req.With("id", 1).WithGroup("db").Info("query", "rows", 2)
	got := entries(t, buf)
	if _, ok := got[0]["req"]; ok {
		t.Fatalf("an empty group was written: %v", got[0])
	}
	want := map[string]any{"id": 1.0, "db": map[string]any{"rows": 2.0}}
	if !reflect.DeepEqual(got[1]["req"], want) {
		t.Fatalf("req = %v, want %v", got[1]["req"], want)
	}
}

func TestSourceIsTheCallersFileAndLine(t *testing.T) {
	cfg := jsonAt("info")
	cfg.Source = true
	log, buf := newLogger(t, cfg)
	_, _, line, _ := runtime.Caller(0)
	log.Info("here")
	want := fmt.Sprintf("logger/handler_test.go:%d", line+1)
	if got := entries(t, buf)[0]["source"]; got != want {
		t.Fatalf("source = %v, want %s", got, want)
	}

	log, buf = newLogger(t, jsonAt("info"))
	log.Info("no source")
	if got := entries(t, buf)[0]; slices.Contains(slices.Collect(maps.Keys(got)), "source") {
		t.Fatalf("source written without Source: %v", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestAFailedWriteIsReportedOnStderr(t *testing.T) {
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = stderr // the handler takes stderr when it is built
	t.Cleanup(func() { os.Stderr = saved })
	core := zapcore.NewCore(zapcore.NewJSONEncoder(zapcore.EncoderConfig{MessageKey: "msg"}), zapcore.AddSync(failingWriter{}), zapcore.DebugLevel)
	log := slog.New(logger.NewHandler(core, logger.HandlerOptions{}))
	log.Info("lost")
	errs, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(errs), "disk full") {
		t.Fatalf("stderr %q", errs)
	}
}

// nowhere drops what it is given, so a benchmark measures the encoding and not a file.
type nowhere struct{}

func (nowhere) Write(p []byte) (int, error) { return len(p), nil }

func BenchmarkHandlers(b *testing.B) {
	zapLog, err := logger.New(nowhere{}, jsonAt("info"))
	if err != nil {
		b.Fatal(err)
	}
	loggers := []struct {
		name string
		log  *slog.Logger
	}{
		{"zapcore", zapLog},
		{"slog-json", slog.New(slog.NewJSONHandler(nowhere{}, nil))},
	}
	errSlow := errors.New("slow save")
	for _, l := range loggers {
		b.Run(l.name, func(b *testing.B) {
			log := l.log.With("service", "world", "map_id", "henesys")
			b.ReportAllocs()
			for b.Loop() {
				log.Info("tick overran", "tick", 42, "took", 3*time.Millisecond, "players", 12, "err", errSlow)
			}
		})
	}
}
