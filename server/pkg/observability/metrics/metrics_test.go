package metrics_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"finga/pkg/observability/metrics"
)

func TestHandler(t *testing.T) {
	reg := metrics.NewRegistry()
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "finga_test_total", Help: "test"})
	if err := reg.Register(c); err != nil {
		t.Fatal(err)
	}
	c.Add(3)

	srv := httptest.NewServer(metrics.Handler(reg))
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

// Every module's every flow is a series from the start, so an alert on its rate sees the first panic; modules on one registry share the family.
func TestPanicCounterCountsByModuleAndFlow(t *testing.T) {
	reg := prometheus.NewRegistry()
	item, err := metrics.NewPanicCounter(reg, "item")
	if err != nil {
		t.Fatal(err)
	}
	world, err := metrics.NewPanicCounter(reg, "world")
	if err != nil {
		t.Fatal(err)
	}
	item.Inc("grpc")
	world.Inc("task")
	world.Inc("task")
	want := `# HELP finga_panics_total Panics recovered, by module and flow.
# TYPE finga_panics_total counter
finga_panics_total{flow="grpc",module="item"} 1
finga_panics_total{flow="grpc",module="world"} 0
finga_panics_total{flow="http",module="item"} 0
finga_panics_total{flow="http",module="world"} 0
finga_panics_total{flow="job",module="item"} 0
finga_panics_total{flow="job",module="world"} 0
finga_panics_total{flow="stream",module="item"} 0
finga_panics_total{flow="stream",module="world"} 0
finga_panics_total{flow="task",module="item"} 0
finga_panics_total{flow="task",module="world"} 2
finga_panics_total{flow="ws",module="item"} 0
finga_panics_total{flow="ws",module="world"} 0
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "finga_panics_total"); err != nil {
		t.Fatal(err)
	}
}

// A restartable task shows at 0 before its first restart; restarts count by module and task.
func TestTaskRestartsCountByModuleAndTask(t *testing.T) {
	reg := prometheus.NewRegistry()
	c, err := metrics.NewTaskRestarts(reg)
	if err != nil {
		t.Fatal(err)
	}
	c.Init("item", "outbox gauge")
	c.Init("world", "parties")
	c.Inc("world", "parties")
	want := `# HELP finga_task_restarts_total Restarts of a restartable task after a panic, an error or an early end, by module and task.
# TYPE finga_task_restarts_total counter
finga_task_restarts_total{module="item",task="outbox gauge"} 0
finga_task_restarts_total{module="world",task="parties"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "finga_task_restarts_total"); err != nil {
		t.Fatal(err)
	}
}

// A family registered again on one registry is the one already there; a different family under its name is refused.
func TestSharedReusesTheFamilyAlreadyRegistered(t *testing.T) {
	reg := prometheus.NewRegistry()
	opts := prometheus.CounterOpts{Name: "shared_total", Help: "A shared family."}
	first, err := metrics.Shared(reg, prometheus.NewCounterVec(opts, []string{"module"}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := metrics.Shared(reg, prometheus.NewCounterVec(opts, []string{"module"}))
	if err != nil || second != first {
		t.Fatalf("got %p, %v; want the first family %p", second, err, first)
	}
	if _, err := metrics.Shared(reg, prometheus.NewCounterVec(opts, []string{"store"})); err == nil {
		t.Fatal("a family with other labels shared the name")
	}
}
