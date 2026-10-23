package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"debug", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"INFO", slog.LevelInfo, false},
		{"verbose", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := observability.ParseLevel(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoggerLevel(t *testing.T) {
	tests := []struct {
		name    string
		level   slog.Level
		logAt   slog.Level
		wantOut bool
	}{
		{"debug record dropped at info", slog.LevelInfo, slog.LevelDebug, false},
		{"info record kept at info", slog.LevelInfo, slog.LevelInfo, true},
		{"warn record kept at info", slog.LevelInfo, slog.LevelWarn, true},
		{"info record dropped at warn", slog.LevelWarn, slog.LevelInfo, false},
		{"debug record kept at debug", slog.LevelDebug, slog.LevelDebug, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := observability.NewLogger(&buf, tt.level)
			log.Log(context.Background(), tt.logAt, "hello", "player_id", "p1")
			if (buf.Len() > 0) != tt.wantOut {
				t.Fatalf("output %q, wantOut %v", buf.String(), tt.wantOut)
			}
			if !tt.wantOut {
				return
			}
			var rec map[string]any
			if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
				t.Fatalf("not JSON: %q", buf.String())
			}
			if rec["msg"] != "hello" || rec["player_id"] != "p1" || rec["level"] != tt.logAt.String() {
				t.Fatalf("record = %v", rec)
			}
		})
	}
}

func TestMetricsHandler(t *testing.T) {
	reg := observability.NewRegistry()
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "finga_test_total", Help: "test"})
	if err := reg.Register(c); err != nil {
		t.Fatal(err)
	}
	c.Add(3)

	srv := httptest.NewServer(observability.MetricsHandler(reg))
	defer srv.Close()
	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body bytes.Buffer
	if _, err := body.ReadFrom(res.Body); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	tests := []struct {
		name string
		want string
	}{
		{"the registered metric", "finga_test_total 3"},
		{"the Go runtime collector", "go_goroutines"},
		{"the process collector", "process_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(body.String(), tt.want) {
				t.Fatalf("/metrics lacks %q:\n%s", tt.want, body.String())
			}
		})
	}
}
