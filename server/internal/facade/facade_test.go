package facade_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"finga/internal/facade"
	"finga/internal/kernel"
	"finga/pkg/observability/metrics"
	"finga/pkg/panics"
	"finga/pkg/transient"
)

var (
	errSoldOut  = errors.New("shop: sold out")
	ruleSoldOut = kernel.NewRule("shop_sold_out")
)

// shop is a made-up module whose one method runs what each test gives it.
type shop struct{ calls *facade.Calls }

func (s shop) buy(ctx context.Context, do func(ctx context.Context) error) (err error) {
	ctx, call := s.calls.Start(ctx, "Buy")
	defer call.End(&err)
	return do(ctx)
}

// rig is where a test reads what the calls recorded.
type rig struct {
	reg   *prometheus.Registry
	spans *tracetest.SpanRecorder
	logs  *bytes.Buffer
}

func newShop(t *testing.T, w rig, module string) shop {
	t.Helper()
	count, err := metrics.NewPanicCounter(w.reg, module)
	if err != nil {
		t.Fatal(err)
	}
	calls, err := facade.New(facade.Options{
		Module:   module,
		Traces:   sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(w.spans)),
		Registry: w.reg,
		Panics:   panics.NewRecorder(slog.New(slog.NewJSONHandler(w.logs, nil)), count),
		Errors:   facade.Errors{{Model: errSoldOut, Kernel: ruleSoldOut}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return shop{calls: calls}
}

func newRig() rig {
	return rig{reg: prometheus.NewRegistry(), spans: tracetest.NewSpanRecorder(), logs: &bytes.Buffer{}}
}

// The last span ended, with its code attribute.
func lastSpan(t *testing.T, w rig) (sdktrace.ReadOnlySpan, string) {
	t.Helper()
	ended := w.spans.Ended()
	if len(ended) == 0 {
		t.Fatal("no span ended")
	}
	s := ended[len(ended)-1]
	for _, a := range s.Attributes() {
		if a.Key == "code" {
			return s, a.Value.AsString()
		}
	}
	t.Fatalf("span %s has no code", s.Name())
	return nil, ""
}

func TestACallIsTracedCountedAndTimed(t *testing.T) {
	w := newRig()
	s := newShop(t, w, "shop")
	if err := s.buy(t.Context(), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	span, code := lastSpan(t, w)
	if span.Name() != "shop.Buy" || code != "ok" || span.Status().Code == otelcodes.Error || !hasAttr(span, attribute.String("module", "shop")) {
		t.Fatalf("span %s, code %s, status %v, attrs %v", span.Name(), code, span.Status(), span.Attributes())
	}
	want := `# HELP finga_module_calls_total Calls to a module's facade, by module, method and code: ok, the rule's reason, unavailable, canceled or internal.
# TYPE finga_module_calls_total counter
finga_module_calls_total{code="ok",method="Buy",module="shop"} 1
`
	if err := testutil.GatherAndCompare(w.reg, strings.NewReader(want), "finga_module_calls_total"); err != nil {
		t.Fatal(err)
	}
	if n, err := testutil.GatherAndCount(w.reg, "finga_module_call_duration_seconds"); err != nil || n != 1 {
		t.Fatalf("%d duration series, err %v", n, err)
	}
}

// Callers see the kernel error, with errors.Is, and the logs keep the model's cause.
func TestAModelErrorReachesTheCallerAsItsKernelError(t *testing.T) {
	w := newRig()
	s := newShop(t, w, "shop")
	err := s.buy(t.Context(), func(context.Context) error { return fmt.Errorf("buying: %w", errSoldOut) })
	if !errors.Is(err, ruleSoldOut) || !errors.Is(err, errSoldOut) {
		t.Fatalf("got %v, want the kernel rule wrapping the model error", err)
	}
	if reason, ok := kernel.ReasonOf(err); !ok || reason != "shop_sold_out" {
		t.Fatalf("reason %q, %v", reason, ok)
	}
	span, code := lastSpan(t, w)
	if code != "shop_sold_out" || span.Status().Code == otelcodes.Error {
		t.Fatalf("code %s, status %v: a rule is the call working", code, span.Status())
	}
}

// A bug in a module fails the call, not the process: the panic is counted under the module and logged with its stack.
func TestAPanicFailsOnlyTheCall(t *testing.T) {
	w := newRig()
	s := newShop(t, w, "shop")
	err := s.buy(t.Context(), func(context.Context) error { panic("till jammed") })
	if !errors.Is(err, kernel.ErrInternal) {
		t.Fatalf("got %v, want ErrInternal", err)
	}
	if _, ok := kernel.ReasonOf(err); ok {
		t.Fatal("a panic is no rule")
	}
	if got := testutil.ToFloat64(panicsOf(t, w, "shop")); got != 1 {
		t.Fatalf("%v panics counted, want 1", got)
	}
	if !strings.Contains(w.logs.String(), `"panic":"till jammed"`) || !strings.Contains(w.logs.String(), `"method":"Buy"`) || !strings.Contains(w.logs.String(), `"stack"`) {
		t.Fatalf("log %s", w.logs.String())
	}
	span, code := lastSpan(t, w)
	if code != "internal" || span.Status().Code != otelcodes.Error {
		t.Fatalf("code %s, status %v", code, span.Status())
	}
}

// A panic with a model or kernel error as its value is still a bug: it never reads as the rule, in the code or to the caller.
func TestAPanickedErrorIsNoRule(t *testing.T) {
	for _, value := range []error{errSoldOut, ruleSoldOut} {
		w := newRig()
		s := newShop(t, w, "shop")
		err := s.buy(t.Context(), func(context.Context) error { panic(value) })
		if !errors.Is(err, kernel.ErrInternal) || errors.Is(err, ruleSoldOut) {
			t.Fatalf("panic(%v): got %v, want ErrInternal alone", value, err)
		}
		if reason, ok := kernel.ReasonOf(err); ok {
			t.Fatalf("panic(%v) reads as rule %s", value, reason)
		}
		if _, code := lastSpan(t, w); code != "internal" {
			t.Fatalf("panic(%v): code %s", value, code)
		}
	}
}

func panicsOf(t *testing.T, w rig, module string) prometheus.Collector {
	t.Helper()
	vec, err := metrics.Shared(w.reg, prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: metrics.Namespace, Name: "panics_total", Help: "Panics recovered, by module and flow."}, []string{"module", "flow"}))
	if err != nil {
		t.Fatal(err)
	}
	return vec.WithLabelValues(module, panics.FlowCall)
}

// The code label is bounded: ok, a rule's reason, unavailable, canceled or internal, so a disconnecting client never reads as a server error.
func TestEachErrorEndsTheCallWithItsCode(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		code string
	}{
		{"a rule from the kernel", fmt.Errorf("equipping: %w", kernel.ErrUnknownClass), "unknown_class"},
		{"a malformed ID", fmt.Errorf("%w: %q", kernel.ErrBadID, "x"), "bad_id"},
		{"a store out of reach", fmt.Errorf("saving: %w", transient.ErrUnavailable), "unavailable"},
		{"a timeout the store turned transient", fmt.Errorf("%w: %w", transient.ErrUnavailable, context.DeadlineExceeded), "unavailable"},
		{"the caller giving up", fmt.Errorf("saving: %w", context.Canceled), "canceled"},
		{"the caller's deadline", context.DeadlineExceeded, "canceled"},
		{"anything else", errors.New("disk on fire"), "internal"},
		{"an internal error", kernel.ErrInternal, "internal"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := newRig()
			s := newShop(t, w, "shop")
			err := s.buy(t.Context(), func(context.Context) error { return tt.err })
			if !errors.Is(err, tt.err) {
				t.Fatalf("got %v, want %v passed through", err, tt.err)
			}
			span, code := lastSpan(t, w)
			if code != tt.code {
				t.Fatalf("code %s, want %s", code, tt.code)
			}
			if failed := span.Status().Code == otelcodes.Error; failed != (tt.code == "internal" || tt.code == "unavailable") {
				t.Fatalf("code %s marked the span failed: %v", code, failed)
			}
		})
	}
}

// Every module's calls count in the same two families, told apart by module.
func TestModulesShareTheFamilies(t *testing.T) {
	w := newRig()
	for _, s := range []shop{newShop(t, w, "shop"), newShop(t, w, "bank")} {
		_ = s.buy(t.Context(), func(context.Context) error { return nil })
	}
	if n, err := testutil.GatherAndCount(w.reg, "finga_module_calls_total"); err != nil || n != 2 {
		t.Fatalf("%d series, err %v", n, err)
	}
}

func hasAttr(s sdktrace.ReadOnlySpan, want attribute.KeyValue) bool {
	return slices.Contains(s.Attributes(), want)
}
