package observability_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"finga/pkg/observability"
)

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

func TestPanicCounterCountsByFlow(t *testing.T) {
	reg := prometheus.NewRegistry()
	c, err := observability.NewPanicCounter(reg)
	if err != nil {
		t.Fatal(err)
	}
	c.Inc("ws")
	c.Inc("ws")
	c.Inc("grpc")
	want := `# HELP panics_total Panics recovered, by flow.
# TYPE panics_total counter
panics_total{flow="grpc"} 1
panics_total{flow="ws"} 2
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "panics_total"); err != nil {
		t.Fatal(err)
	}
	if _, err := observability.NewPanicCounter(reg); err == nil {
		t.Fatal("a second counter registered panics_total again")
	}
}
