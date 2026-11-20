// Package world is the world's Prometheus metrics, the observer its maps, item calls and saver report to.
package world

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability/metrics"
)

// Tick buckets cover the 50 ms budget at 20 Hz with headroom for overruns.
var tickBuckets = []float64{.0005, .001, .002, .005, .01, .015, .025, .05, .1, .25}

// batchBuckets are drops per grant call, up to the item module's 32.
var batchBuckets = []float64{1, 2, 4, 8, 16, 32}

// Metrics are the world's: its maps' ticks, its calls to the item module and its dropped saves.
type Metrics struct {
	tickDuration *prometheus.HistogramVec // by map and channel
	tickOverruns *prometheus.CounterVec   // by map and channel
	channelsOpen *prometheus.GaugeVec     // by region
	itemCalls    prometheus.Gauge
	batchSize    prometheus.Histogram
	savesDropped *prometheus.CounterVec // by reason
}

// New registers the world's metrics on reg, each of dropReasons at 0 so its first drop shows as an increase; registering twice on one registry fails.
func New(reg prometheus.Registerer, dropReasons []string) (*Metrics, error) {
	m := &Metrics{
		tickDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metrics.Namespace, Name: "tick_duration_seconds", Help: "Processing time of one map instance tick: a region's by channel, 0 off regions.", Buckets: tickBuckets,
		}, []string{"map", "channel"}),
		tickOverruns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metrics.Namespace, Name: "tick_overruns_total", Help: "Ticks that took longer than the tick interval: a region's by channel, 0 off regions.",
		}, []string{"map", "channel"}),
		channelsOpen: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metrics.Namespace, Name: "channels_open", Help: "A region's open channels.",
		}, []string{"region"}),
		itemCalls: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metrics.Namespace, Name: "item_calls_inflight", Help: "Calls to the item module waiting for a worker or running.",
		}),
		batchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: metrics.Namespace, Name: "item_calls_batch_size", Help: "Drops carried by each grant call to the item module.", Buckets: batchBuckets,
		}),
		savesDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metrics.Namespace, Name: "saves_dropped_total", Help: "Checkpoints the world gave up on, so progress lost, by why. One at shutdown is counted after /metrics has closed: alert on its ERROR log line instead, \"persistence failed\" with dropped=shutdown.",
		}, []string{"reason"}),
	}
	for _, reason := range dropReasons {
		m.savesDropped.WithLabelValues(reason)
	}
	return m, metrics.Register(reg, m.tickDuration, m.tickOverruns, m.channelsOpen, m.itemCalls, m.batchSize, m.savesDropped)
}

// SaveDropped counts a checkpoint given up on.
func (m *Metrics) SaveDropped(reason string) { m.savesDropped.WithLabelValues(reason).Inc() }

// ObserveTick records one tick of mapID's channel, 0 off regions, counting an overrun when took exceeds budget.
func (m *Metrics) ObserveTick(mapID string, channel int, took, budget time.Duration) {
	ch := strconv.Itoa(channel)
	m.tickDuration.WithLabelValues(mapID, ch).Observe(took.Seconds())
	if took > budget {
		m.tickOverruns.WithLabelValues(mapID, ch).Inc()
	}
}

// ChannelsOpen sets how many channels region has open.
func (m *Metrics) ChannelsOpen(region string, open int) {
	m.channelsOpen.WithLabelValues(region).Set(float64(open))
}

// CallQueued counts a call to the item module queued for a worker.
func (m *Metrics) CallQueued() { m.itemCalls.Inc() }

// CallDone counts a call to the item module done.
func (m *Metrics) CallDone() { m.itemCalls.Dec() }

// ObserveBatch records how many drops a grant call carries.
func (m *Metrics) ObserveBatch(drops int) { m.batchSize.Observe(float64(drops)) }
