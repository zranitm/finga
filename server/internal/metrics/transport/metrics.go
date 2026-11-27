// Package transport is the WebSocket transport's Prometheus metrics, the observer its server and outboxes report to.
package transport

import (
	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability/metrics"
)

// welcomeBuckets are a Welcome's encoded bytes, 512 B to 512 KiB.
var welcomeBuckets = prometheus.ExponentialBuckets(512, 2, 11)

// Metrics are the WebSocket's: its messages, its slow consumers, the size of each Welcome and the pushes with no encoder.
type Metrics struct {
	messages      *prometheus.CounterVec // by direction (in, out) and type
	slowConsumers prometheus.Counter
	welcomeBytes  *prometheus.HistogramVec // by kind (login, rejoin)
	unencoded     *prometheus.CounterVec   // by push type: a push no handler had an encoder for
}

// New registers the WebSocket's metrics on reg; registering twice on one registry fails.
func New(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		messages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metrics.Namespace, Name: "ws_messages_total", Help: "WebSocket messages by direction and type.",
		}, []string{"direction", "type"}),
		slowConsumers: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metrics.Namespace, Name: "ws_slow_consumer_disconnects_total", Help: "Connections closed because their send queue filled up.",
		}),
		welcomeBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metrics.Namespace, Name: "ws_welcome_bytes", Help: "Encoded size of each Welcome, by kind: login or rejoin.", Buckets: welcomeBuckets,
		}, []string{"kind"}),
		unencoded: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metrics.Namespace, Name: "ws_unencoded_pushes_total", Help: "Pushes dropped because no handler registered an encoder for their type, which is a bug.",
		}, []string{"type"}),
	}
	return m, metrics.Register(reg, m.messages, m.slowConsumers, m.welcomeBytes, m.unencoded)
}

// MessageIn counts a message received over the WebSocket.
func (m *Metrics) MessageIn(msgType string) { m.messages.WithLabelValues("in", msgType).Inc() }

// MessageOut counts a message sent over the WebSocket.
func (m *Metrics) MessageOut(msgType string) { m.messages.WithLabelValues("out", msgType).Inc() }

// WelcomeSent records a Welcome's encoded size; it is the signal for when its sections need skipping or compressing.
func (m *Metrics) WelcomeSent(kind string, bytes int) {
	m.welcomeBytes.WithLabelValues(kind).Observe(float64(bytes))
}

// SlowConsumer counts a connection closed because its send queue filled up.
func (m *Metrics) SlowConsumer() { m.slowConsumers.Inc() }

// Unencoded counts a push dropped for want of an encoder.
func (m *Metrics) Unencoded(pushType string) { m.unencoded.WithLabelValues(pushType).Inc() }
