package logger_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"finga/pkg/observability/logger"
)

func jsonAt(level string) logger.Config {
	return logger.Config{Level: level, Format: logger.FormatJSON}
}

func newLogger(t *testing.T, cfg logger.Config) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	log, err := logger.New(&buf, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return log, &buf
}

func entries(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.Lines(buf.String()) {
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("%v: %q", err, line)
		}
		out = append(out, e)
	}
	return out
}

func TestJSONLinesUseSlogsKeys(t *testing.T) {
	log, buf := newLogger(t, jsonAt("info"))
	log.Info("listening", "listener", "grpc", "addr", "127.0.0.1:9004")
	line := buf.String()
	pattern := `^\{"level":"INFO","time":"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?[^"]*","msg":"listening","listener":"grpc","addr":"127.0.0.1:9004"\}\n$`
	if !regexp.MustCompile(pattern).MatchString(line) {
		t.Fatalf("line %q", line)
	}
}

func TestTheConsoleFormatIsOneReadableLine(t *testing.T) {
	log, buf := newLogger(t, logger.Config{Level: "info", Format: logger.FormatConsole})
	log.With("map_id", "henesys").Info("player joined", "player", "ann", "took", 1500*time.Millisecond)
	line := buf.String()
	for _, want := range []string{"INFO", "\tplayer joined\t", `{"map_id": "henesys", "player": "ann", "took": "1.5s"}`} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q lacks %q", line, want)
		}
	}
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("line %q is not one line", line)
	}
}

func TestSamplingThinsOutARepeatedMessagePerSecond(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := jsonAt("info")
		cfg.Sampling = logger.Sampling{Initial: 2, Thereafter: 3}
		log, buf := newLogger(t, cfg)
		for range 10 {
			log.Info("hot")
		}
		log.Info("cold")
		log.Warn("hot")
		time.Sleep(time.Second)
		log.Info("hot")

		counts := map[string]int{}
		for _, e := range entries(t, buf) {
			counts[e["level"].(string)+" "+e["msg"].(string)]++
		}
		want := map[string]int{"INFO hot": 2 + 2 + 1, "INFO cold": 1, "WARN hot": 1}
		if len(counts) != len(want) {
			t.Fatalf("counts %v, want %v", counts, want)
		}
		for k, n := range want {
			if counts[k] != n {
				t.Fatalf("counts %v, want %v", counts, want)
			}
		}
	})
}

func TestNoSamplingKeepsEveryRecord(t *testing.T) {
	log, buf := newLogger(t, jsonAt("info"))
	for range 500 {
		log.Info("hot")
	}
	if n := len(entries(t, buf)); n != 500 {
		t.Fatalf("%d entries, want 500", n)
	}
}

func TestNewRefusesABadConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  logger.Config
		want string
	}{
		{"an unknown level", logger.Config{Level: "verbose", Format: "json"}, `level "verbose"`},
		{"no level", logger.Config{Format: "json"}, `level ""`},
		{"an unknown format", logger.Config{Level: "info", Format: "xml"}, `format "xml"`},
		{"no format", logger.Config{Level: "info"}, `format ""`},
		{"a negative initial", logger.Config{Level: "info", Format: "json", Sampling: logger.Sampling{Initial: -1}}, "sampling"},
		{"a negative thereafter", logger.Config{Level: "info", Format: "json", Sampling: logger.Sampling{Initial: 1, Thereafter: -1}}, "sampling"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := logger.New(io.Discard, tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one naming %s", err, tt.want)
			}
		})
	}
}
