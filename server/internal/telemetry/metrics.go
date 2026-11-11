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

// welcomeBuckets are a Welcome's encoded bytes, 512 B to 512 KiB.
var welcomeBuckets = prometheus.ExponentialBuckets(512, 2, 11)

// batchBuckets are drops per grant call, up to the item service's 32.
var batchBuckets = []float64{1, 2, 4, 8, 16, 32}

func register(reg prometheus.Registerer, cs ...prometheus.Collector) error {
	for _, c := range cs {
		if err := reg.Register(c); err != nil {
			return err
		}
	}
	return nil
}

// WorldMetrics are the world's: its maps' ticks, its calls to the item service and its dropped saves.
type WorldMetrics struct {
	tickDuration *prometheus.HistogramVec // by map
	tickOverruns *prometheus.CounterVec   // by map
	itemCalls    prometheus.Gauge
	batchSize    prometheus.Histogram
	savesDropped *prometheus.CounterVec // by reason
}

// NewWorldMetrics registers the world's metrics on reg, each of dropReasons at 0 so its first drop shows as an increase; registering twice on one registry fails.
func NewWorldMetrics(reg prometheus.Registerer, dropReasons []string) (*WorldMetrics, error) {
	m := &WorldMetrics{
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
	return m, register(reg, m.tickDuration, m.tickOverruns, m.itemCalls, m.batchSize, m.savesDropped)
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

// CallQueued counts a call to the item service queued for a worker.
func (m *WorldMetrics) CallQueued() { m.itemCalls.Inc() }

// CallDone counts a call to the item service done.
func (m *WorldMetrics) CallDone() { m.itemCalls.Dec() }

// ObserveBatch records how many drops a grant call carries.
func (m *WorldMetrics) ObserveBatch(drops int) { m.batchSize.Observe(float64(drops)) }

// GatewayMetrics are the gateway's: its players and their WebSocket traffic.
type GatewayMetrics struct {
	playersOnline        prometheus.Gauge
	registrationsWaiting prometheus.Gauge
	messages             *prometheus.CounterVec // by direction (in, out) and type
	slowConsumers        prometheus.Counter
	welcomeBytes         *prometheus.HistogramVec // by kind (login, rejoin)
}

// NewGatewayMetrics registers the gateway's metrics on reg; registering twice on one registry fails.
func NewGatewayMetrics(reg prometheus.Registerer) (*GatewayMetrics, error) {
	m := &GatewayMetrics{
		playersOnline: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: observability.Namespace, Name: "players_online", Help: "Players whose Hello joined them to a map.",
		}),
		registrationsWaiting: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: observability.Namespace, Name: "social_registrations_waiting", Help: "Logins whose registration with the social service has not landed yet, retries included.",
		}),
		messages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: observability.Namespace, Name: "ws_messages_total", Help: "WebSocket messages by direction and type.",
		}, []string{"direction", "type"}),
		slowConsumers: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: observability.Namespace, Name: "ws_slow_consumer_disconnects_total", Help: "Connections closed because their send queue filled up.",
		}),
		welcomeBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: observability.Namespace, Name: "ws_welcome_bytes", Help: "Encoded size of each Welcome, by kind: login or rejoin.", Buckets: welcomeBuckets,
		}, []string{"kind"}),
	}
	return m, register(reg, m.playersOnline, m.registrationsWaiting, m.messages, m.slowConsumers, m.welcomeBytes)
}

// MessageIn counts a message received over the WebSocket.
func (m *GatewayMetrics) MessageIn(msgType string) { m.messages.WithLabelValues("in", msgType).Inc() }

// MessageOut counts a message sent over the WebSocket.
func (m *GatewayMetrics) MessageOut(msgType string) { m.messages.WithLabelValues("out", msgType).Inc() }

// WelcomeSent records a Welcome's encoded size; it is the signal for when its sections need skipping or compressing.
func (m *GatewayMetrics) WelcomeSent(kind string, bytes int) {
	m.welcomeBytes.WithLabelValues(kind).Observe(float64(bytes))
}

// SlowConsumer counts a connection closed because its send queue filled up.
func (m *GatewayMetrics) SlowConsumer() { m.slowConsumers.Inc() }

// OnlineChanged moves the online gauge by delta.
func (m *GatewayMetrics) OnlineChanged(delta int) { m.playersOnline.Add(float64(delta)) }

// RegistrationsWaiting moves the gauge of logins waiting to register with social by delta.
func (m *GatewayMetrics) RegistrationsWaiting(delta int) {
	m.registrationsWaiting.Add(float64(delta))
}

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
