package tracing_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"finga/pkg/configload"
	"finga/pkg/observability/tracing"
)

func TestOnlyTheEntryPointsStartASampledTrace(t *testing.T) {
	ctx := context.Background()
	tp, err := tracing.New(ctx, tracing.Config{}, tracing.Options{Service: "shop", Root: func(name string) bool { return name == "shop.buy" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tp.Shutdown(ctx) })
	tracer := tp.Tracer("test")

	buyCtx, buy := tracer.Start(ctx, "shop.buy")
	defer buy.End()
	_, other := tracer.Start(ctx, "shop.tick")
	defer other.End()
	_, child := tracer.Start(buyCtx, "item.add")
	defer child.End()
	tests := []struct {
		name    string
		sampled bool
		got     bool
	}{
		{"an entry point is sampled", true, buy.SpanContext().IsSampled()},
		{"any other root is not", false, other.SpanContext().IsSampled()},
		{"a child follows its parent", true, child.SpanContext().IsSampled() && child.SpanContext().TraceID() == buy.SpanContext().TraceID()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.sampled {
				t.Fatalf("sampled %v, want %v", tt.got, tt.sampled)
			}
		})
	}
	t.Run("a root that is not sampled still has its IDs, for the logs", func(t *testing.T) {
		if !other.SpanContext().TraceID().IsValid() || !other.SpanContext().SpanID().IsValid() {
			t.Fatalf("span context %+v", other.SpanContext())
		}
	})
}

// A configured collector gets the sampled spans, flushed at Shutdown; an unreachable one fails no call.
func TestAnEndpointExportsWithoutBlocking(t *testing.T) {
	ctx := context.Background()
	tp, err := tracing.New(ctx, tracing.Config{OTLPEndpoint: "127.0.0.1:1"}, tracing.Options{Service: "shop", Root: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	_, span := tp.Tracer("test").Start(ctx, "shop.buy")
	span.End()
	if !span.SpanContext().IsSampled() {
		t.Fatal("the entry point was not sampled")
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 0)
	defer cancel()
	_ = tp.Shutdown(shutdownCtx) // the collector is not there; Shutdown gives up when its context ends
}

// The resource names the service, its version and this process, on top of the SDK's and OTEL_RESOURCE_ATTRIBUTES.
func TestTheResourceNamesTheServiceItsVersionAndProcess(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=test,service.name=other")
	instance := func() attribute.Set {
		tp, err := tracing.New(t.Context(), tracing.Config{}, tracing.Options{Service: "shop", Version: "v1.2.3", Root: func(string) bool { return true }})
		if err != nil {
			t.Fatal(err)
		}
		rec := tracetest.NewSpanRecorder()
		tp.RegisterSpanProcessor(rec)
		_, span := tp.Tracer("test").Start(t.Context(), "shop.buy")
		span.End()
		return *rec.Ended()[0].Resource().Set()
	}
	first, second := instance(), instance()
	for key, want := range map[attribute.Key]string{"service.name": "shop", "service.version": "v1.2.3", "deployment.environment.name": "test", "telemetry.sdk.language": "go"} {
		if v, _ := first.Value(key); v.AsString() != want {
			t.Errorf("%s is %q, want %q", key, v.AsString(), want)
		}
	}
	a, _ := first.Value("service.instance.id")
	b, _ := second.Value("service.instance.id")
	if a.AsString() == "" || a.AsString() == b.AsString() {
		t.Fatalf("instance IDs %q and %q; want one per process", a.AsString(), b.AsString())
	}
}

// OpenTelemetry's own errors, such as a failed export, go to the service's logger, one a minute, each line counting those left out.
func TestOpenTelemetrysErrorsGoToTheLoggerSampled(t *testing.T) {
	prev := otel.GetErrorHandler()
	t.Cleanup(func() { otel.SetErrorHandler(prev) }) // the handler is process-wide
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		if _, err := tracing.New(t.Context(), tracing.Config{}, tracing.Options{Service: "shop", Logger: slog.New(slog.NewJSONHandler(&logs, nil))}); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			otel.Handle(errors.New("traces export: connection refused"))
		}
		if got := strings.Count(logs.String(), "connection refused"); got != 1 || !strings.Contains(logs.String(), `"suppressed":0`) {
			t.Fatalf("%d lines for 3 errors; want 1, with none suppressed before it:\n%s", got, logs.String())
		}
		time.Sleep(time.Minute)
		otel.Handle(errors.New("traces export: connection refused"))
		if got := strings.Count(logs.String(), "connection refused"); got != 2 || !strings.Contains(logs.String(), `"suppressed":2`) {
			t.Fatalf("%d lines; want a second a minute on, counting the 2 left out:\n%s", got, logs.String())
		}
	})
}

// Shutdown reports the errors left out since the last line, so a run that ends within the gap, the last flush's included, still shows.
func TestShutdownReportsTheErrorsLeftOut(t *testing.T) {
	prev := otel.GetErrorHandler()
	t.Cleanup(func() { otel.SetErrorHandler(prev) })
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		tp, err := tracing.New(t.Context(), tracing.Config{}, tracing.Options{Service: "shop", Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
		if err != nil {
			t.Fatal(err)
		}
		otel.Handle(errors.New("traces export: connection refused"))
		otel.Handle(errors.New("traces export: connection refused"))
		otel.Handle(errors.New("traces export: context deadline exceeded"))
		if err := tp.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(logs.String(), `"msg":"opentelemetry error"`); got != 2 || !strings.Contains(logs.String(), `"err":"traces export: context deadline exceeded","suppressed":1`) {
			t.Fatalf("%d lines; want a second at Shutdown, the last error left out, counting the 1 before it:\n%s", got, logs.String())
		}
	})
}

func TestTheEndpointIsAHostAndPort(t *testing.T) {
	type config struct {
		Tracing tracing.Config `mapstructure:"tracing"`
	}
	for endpoint, valid := range map[string]bool{
		"": true, "alloy:4317": true, "127.0.0.1:4317": true, "[::1]:4317": true, "otel_collector:4317": true, "collector.obs.svc.:4317": true,
		"http://alloy:4317": false, "alloy": false, ":4317": false, "alloy:0": false, "alloy:65536": false, "::1:4317": false, "al loy:4317": false,
	} {
		_, err := configload.LoadFrom[config](strings.NewReader("tracing:\n  otlp_endpoint: \""+endpoint+"\"\n"), "test", configload.Options{})
		if (err == nil) != valid {
			t.Errorf("%q: %v", endpoint, err)
		}
	}
}
