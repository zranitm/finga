// Package telemetry defines the game server's metrics (Low-level design,
// page 3) and, from later steps, implements the observer interfaces the
// application modules declare, so application code never imports it.
package telemetry

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds every metric named on page 3, registered on one registry.
type Metrics struct {
	TickDuration              *prometheus.HistogramVec // by map
	TickOverruns              *prometheus.CounterVec   // by map
	PlayersOnline             prometheus.Gauge
	WSMessages                *prometheus.CounterVec // by direction (in, out) and type
	WSSlowConsumerDisconnects prometheus.Counter
	DBTxDuration              *prometheus.HistogramVec // by operation
	DBTxRetries               *prometheus.CounterVec   // by operation
	EconomyJobsInflight       prometheus.Gauge
	OutboxUnpublished         prometheus.Gauge
}

// Tick buckets cover the 50 ms budget at 20 Hz with headroom for overruns.
var tickBuckets = []float64{.0005, .001, .002, .005, .01, .015, .025, .05, .1, .25}

// Transaction buckets cover a local Postgres (sub-millisecond) to a slow one.
var txBuckets = []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1}

// NewMetrics defines the metrics and registers them on reg. Registering
// twice on one registry fails, so each process builds one Metrics.
func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		TickDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "tick_duration_seconds", Help: "Processing time of one map instance tick.", Buckets: tickBuckets,
		}, []string{"map"}),
		TickOverruns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tick_overruns_total", Help: "Ticks that took longer than the tick interval.",
		}, []string{"map"}),
		PlayersOnline: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "players_online", Help: "Characters attached to a map instance.",
		}),
		WSMessages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ws_messages_total", Help: "WebSocket messages by direction and type.",
		}, []string{"direction", "type"}),
		WSSlowConsumerDisconnects: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ws_slow_consumer_disconnects_total", Help: "Connections closed because their send queue filled up.",
		}),
		DBTxDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "db_tx_duration_seconds", Help: "Database transaction time by operation, retries included.", Buckets: txBuckets,
		}, []string{"operation"}),
		DBTxRetries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "db_tx_retries_total", Help: "Transactions re-run after a deadlock or serialization failure.",
		}, []string{"operation"}),
		EconomyJobsInflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "economy_jobs_inflight", Help: "Pickups and inventory actions waiting on or inside a transaction.",
		}),
		OutboxUnpublished: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "outbox_unpublished", Help: "Outbox rows not yet published. Grows in Stage 1; relay lag in Stage 2.",
		}),
	}
	for _, c := range []prometheus.Collector{
		m.TickDuration, m.TickOverruns, m.PlayersOnline, m.WSMessages, m.WSSlowConsumerDisconnects,
		m.DBTxDuration, m.DBTxRetries, m.EconomyJobsInflight, m.OutboxUnpublished,
	} {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ObserveTick records one tick of mapID that took took, counting an overrun
// when it exceeded budget (the tick interval).
func (m *Metrics) ObserveTick(mapID string, took, budget time.Duration) {
	m.TickDuration.WithLabelValues(mapID).Observe(took.Seconds())
	if took > budget {
		m.TickOverruns.WithLabelValues(mapID).Inc()
	}
}

// MessageIn counts a message received over the WebSocket.
func (m *Metrics) MessageIn(msgType string) { m.WSMessages.WithLabelValues("in", msgType).Inc() }

// MessageOut counts a message sent over the WebSocket.
func (m *Metrics) MessageOut(msgType string) { m.WSMessages.WithLabelValues("out", msgType).Inc() }

// SlowConsumer counts a connection closed because its send queue filled up.
func (m *Metrics) SlowConsumer() { m.WSSlowConsumerDisconnects.Inc() }

// OnlineChanged moves the online gauge by delta.
func (m *Metrics) OnlineChanged(delta int) { m.PlayersOnline.Add(float64(delta)) }

// JobStarted counts an economy job queued for a worker.
func (m *Metrics) JobStarted() { m.EconomyJobsInflight.Inc() }

// JobFinished counts an economy job done.
func (m *Metrics) JobFinished() { m.EconomyJobsInflight.Dec() }
