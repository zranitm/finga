// Package telemetry defines the game's metrics, which each binary registers, and the application modules' observers.
package telemetry

import (
	"context"
	"log/slog"
	"path"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"finga/pkg/dbx"
	"finga/pkg/observability"
	"finga/pkg/retention"
)

// Tick buckets cover the 50 ms budget at 20 Hz with headroom for overruns.
var tickBuckets = []float64{.0005, .001, .002, .005, .01, .015, .025, .05, .1, .25}

// Transaction buckets cover a local Postgres (sub-millisecond) to a slow one.
var txBuckets = []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1}

func register(reg prometheus.Registerer, cs ...prometheus.Collector) error {
	for _, c := range cs {
		if err := reg.Register(c); err != nil {
			return err
		}
	}
	return nil
}

// WorldMetrics are the world's: its maps' ticks and its economy jobs.
type WorldMetrics struct {
	tickDuration *prometheus.HistogramVec // by map
	tickOverruns *prometheus.CounterVec   // by map
	jobsInflight prometheus.Gauge
	savesDropped *prometheus.CounterVec // by reason
}

// NewWorldMetrics registers the world's metrics on reg; registering twice on one registry fails.
func NewWorldMetrics(reg prometheus.Registerer) (*WorldMetrics, error) {
	m := &WorldMetrics{
		tickDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: observability.Namespace, Name: "tick_duration_seconds", Help: "Processing time of one map instance tick.", Buckets: tickBuckets,
		}, []string{"map"}),
		tickOverruns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: observability.Namespace, Name: "tick_overruns_total", Help: "Ticks that took longer than the tick interval.",
		}, []string{"map"}),
		jobsInflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: observability.Namespace, Name: "economy_jobs_inflight", Help: "Pickups waiting on or inside a call to the item service.",
		}),
		savesDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: observability.Namespace, Name: "saves_dropped_total", Help: "Checkpoints the world gave up on, so progress lost, by why.",
		}, []string{"reason"}),
	}
	return m, register(reg, m.tickDuration, m.tickOverruns, m.jobsInflight, m.savesDropped)
}

// SaveDropped counts a checkpoint given up on.
func (m *WorldMetrics) SaveDropped(reason string) { m.savesDropped.WithLabelValues(reason).Inc() }

// ObserveTick records one tick of mapID, counting an overrun when took exceeds budget.
func (m *WorldMetrics) ObserveTick(mapID string, took, budget time.Duration) {
	m.tickDuration.WithLabelValues(mapID).Observe(took.Seconds())
	if took > budget {
		m.tickOverruns.WithLabelValues(mapID).Inc()
	}
}

// JobStarted counts an economy job queued for a worker.
func (m *WorldMetrics) JobStarted() { m.jobsInflight.Inc() }

// JobFinished counts an economy job done.
func (m *WorldMetrics) JobFinished() { m.jobsInflight.Dec() }

// GatewayMetrics are the gateway's: its players and their WebSocket traffic.
type GatewayMetrics struct {
	playersOnline prometheus.Gauge
	messages      *prometheus.CounterVec // by direction (in, out) and type
	slowConsumers prometheus.Counter
}

// NewGatewayMetrics registers the gateway's metrics on reg; registering twice on one registry fails.
func NewGatewayMetrics(reg prometheus.Registerer) (*GatewayMetrics, error) {
	m := &GatewayMetrics{
		playersOnline: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: observability.Namespace, Name: "players_online", Help: "Players whose Hello joined them to a map.",
		}),
		messages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: observability.Namespace, Name: "ws_messages_total", Help: "WebSocket messages by direction and type.",
		}, []string{"direction", "type"}),
		slowConsumers: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: observability.Namespace, Name: "ws_slow_consumer_disconnects_total", Help: "Connections closed because their send queue filled up.",
		}),
	}
	return m, register(reg, m.playersOnline, m.messages, m.slowConsumers)
}

// MessageIn counts a message received over the WebSocket.
func (m *GatewayMetrics) MessageIn(msgType string) { m.messages.WithLabelValues("in", msgType).Inc() }

// MessageOut counts a message sent over the WebSocket.
func (m *GatewayMetrics) MessageOut(msgType string) { m.messages.WithLabelValues("out", msgType).Inc() }

// SlowConsumer counts a connection closed because its send queue filled up.
func (m *GatewayMetrics) SlowConsumer() { m.slowConsumers.Inc() }

// OnlineChanged moves the online gauge by delta.
func (m *GatewayMetrics) OnlineChanged(delta int) { m.playersOnline.Add(float64(delta)) }

// DBMetrics are a database service's transactions; it is its stores' dbx.Observer.
type DBMetrics struct {
	duration *prometheus.HistogramVec // by operation
	retries  *prometheus.CounterVec   // by operation
}

var _ dbx.Observer = (*DBMetrics)(nil)

// NewDBMetrics registers the transaction metrics on reg; registering twice on one registry fails.
func NewDBMetrics(reg prometheus.Registerer) (*DBMetrics, error) {
	m := &DBMetrics{
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: observability.Namespace, Name: "db_tx_duration_seconds", Help: "Database transaction time by operation, retries included.", Buckets: txBuckets,
		}, []string{"operation"}),
		retries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: observability.Namespace, Name: "db_tx_retries_total", Help: "Transactions re-run after a deadlock, a serialization failure or a lock timeout.",
		}, []string{"operation"}),
	}
	return m, register(reg, m.duration, m.retries)
}

// TxDone records a transaction under the gRPC method it served, as Add for /finga.item.v1.ItemService/Add.
func (m *DBMetrics) TxDone(ctx context.Context, took time.Duration, runs int) {
	op := "other"
	if method, ok := grpc.Method(ctx); ok {
		op = path.Base(method)
	}
	m.duration.WithLabelValues(op).Observe(took.Seconds())
	if runs > 1 {
		m.retries.WithLabelValues(op).Add(float64(runs - 1))
	}
}

// RetentionMetrics are the rows each retention task deleted; it is their retention.Observer.
type RetentionMetrics struct {
	deleted *prometheus.CounterVec // by store: a table, or social's in-memory members
}

var _ retention.Observer = (*RetentionMetrics)(nil)

// NewRetentionMetrics registers the deleted-rows counter on reg; registering twice on one registry fails.
func NewRetentionMetrics(reg prometheus.Registerer) (*RetentionMetrics, error) {
	m := &RetentionMetrics{deleted: prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: observability.Namespace, Name: "retention_deleted_total", Help: "Rows a retention task deleted once kept their time, by store: a table, or social's in-memory members.",
	}, []string{"store"})}
	return m, register(reg, m.deleted)
}

// Deleted counts n rows deleted from store; a pass that deleted none still shows the store, at 0.
func (m *RetentionMetrics) Deleted(store string, n int64) {
	m.deleted.WithLabelValues(store).Add(float64(n))
}

// OutboxMetrics are the outbox's: the rows a relay has yet to publish.
type OutboxMetrics struct {
	unpublished prometheus.Gauge
}

// NewOutboxMetrics registers the outbox gauge on reg; registering twice on one registry fails.
func NewOutboxMetrics(reg prometheus.Registerer) (*OutboxMetrics, error) {
	m := &OutboxMetrics{unpublished: prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: observability.Namespace, Name: "outbox_unpublished", Help: "Outbox rows not yet published. Grows in Stage 1; relay lag in Stage 2.",
	})}
	return m, register(reg, m.unpublished)
}

// Track sets the gauge from count now and every interval until ctx ends; a failed count leaves the gauge as it was.
func (m *OutboxMetrics) Track(ctx context.Context, every time.Duration, count func(context.Context) (int64, error), log *slog.Logger) error {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		n, err := count(ctx)
		switch {
		case err == nil:
			m.unpublished.Set(float64(n))
		case ctx.Err() == nil:
			log.WarnContext(ctx, "outbox count failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}
