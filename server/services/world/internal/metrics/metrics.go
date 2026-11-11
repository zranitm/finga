// Package metrics is the world's Prometheus metrics, the observer its maps, item calls and saver report to.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability"
)

// Tick buckets cover the 50 ms budget at 20 Hz with headroom for overruns.
var tickBuckets = []float64{.0005, .001, .002, .005, .01, .015, .025, .05, .1, .25}

// batchBuckets are drops per grant call, up to the item service's 32.
var batchBuckets = []float64{1, 2, 4, 8, 16, 32}

// Metrics are the world's: its maps' ticks, its calls to the item service and its dropped saves.
type Metrics struct {
	tickDuration *prometheus.HistogramVec // by map
	tickOverruns *prometheus.CounterVec   // by map
	itemCalls    prometheus.Gauge
	batchSize    prometheus.Histogram
	savesDropped *prometheus.CounterVec // by reason
}

// New registers the world's metrics on reg, each of dropReasons at 0 so its first drop shows as an increase; registering twice on one registry fails.
func New(reg prometheus.Registerer, dropReasons []string) (*Metrics, error) {
	m := &Metrics{
		tickDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: observability.Namespace, Name: "tick_duration_seconds", Help: "Processing time of one map instance tick.", Buckets: tickBuckets,
		}, []string{"map"}),
		tickOverruns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: observability.Namespace, Name: "tick_overruns_total", Help: "Ticks that took longer than the tick interval.",
		}, []string{"map"}),
		itemCalls: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: observability.Namespace, Name: "item_calls_inflight", Help: "Calls to the item service waiting for a worker or running.",
		}),
		batchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: observability.Namespace, Name: "item_calls_batch_size", Help: "Drops carried by each grant call to the item service.", Buckets: batchBuckets,
		}),
		savesDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: observability.Namespace, Name: "saves_dropped_total", Help: "Checkpoints the world gave up on, so progress lost, by why. One at shutdown is counted after /metrics has closed: alert on its ERROR log line instead, \"persistence failed\" with dropped=shutdown.",
		}, []string{"reason"}),
	}
	for _, reason := range dropReasons {
		m.savesDropped.WithLabelValues(reason)
	}
	return m, observability.Register(reg, m.tickDuration, m.tickOverruns, m.itemCalls, m.batchSize, m.savesDropped)
}

// SaveDropped counts a checkpoint given up on.
func (m *Metrics) SaveDropped(reason string) { m.savesDropped.WithLabelValues(reason).Inc() }

// ObserveTick records one tick of mapID, counting an overrun when took exceeds budget.
func (m *Metrics) ObserveTick(mapID string, took, budget time.Duration) {
	m.tickDuration.WithLabelValues(mapID).Observe(took.Seconds())
	if took > budget {
		m.tickOverruns.WithLabelValues(mapID).Inc()
	}
}

// CallQueued counts a call to the item service queued for a worker.
func (m *Metrics) CallQueued() { m.itemCalls.Inc() }

// CallDone counts a call to the item service done.
func (m *Metrics) CallDone() { m.itemCalls.Dec() }

// ObserveBatch records how many drops a grant call carries.
func (m *Metrics) ObserveBatch(drops int) { m.batchSize.Observe(float64(drops)) }
