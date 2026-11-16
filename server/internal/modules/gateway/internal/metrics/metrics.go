// Package metrics is the gateway's Prometheus metrics, the observer its sessions and WebSocket handlers report to.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability/metrics"
)

// welcomeBuckets are a Welcome's encoded bytes, 512 B to 512 KiB.
var welcomeBuckets = prometheus.ExponentialBuckets(512, 2, 11)

// Metrics are the gateway's: its players and their WebSocket traffic.
type Metrics struct {
	playersOnline        prometheus.Gauge
	registrationsWaiting prometheus.Gauge
	messages             *prometheus.CounterVec // by direction (in, out) and type
	slowConsumers        prometheus.Counter
	welcomeBytes         *prometheus.HistogramVec // by kind (login, rejoin)
}

// New registers the gateway's metrics on reg; registering twice on one registry fails.
func New(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		playersOnline: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metrics.Namespace, Name: "players_online", Help: "Players whose Hello joined them to a map.",
		}),
		registrationsWaiting: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metrics.Namespace, Name: "social_registrations_waiting", Help: "Logins whose registration with the social module has not landed yet, retries included.",
		}),
		messages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metrics.Namespace, Name: "ws_messages_total", Help: "WebSocket messages by direction and type.",
		}, []string{"direction", "type"}),
		slowConsumers: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metrics.Namespace, Name: "ws_slow_consumer_disconnects_total", Help: "Connections closed because their send queue filled up.",
		}),
		welcomeBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metrics.Namespace, Name: "ws_welcome_bytes", Help: "Encoded size of each Welcome, by kind: login or rejoin.", Buckets: welcomeBuckets,
		}, []string{"kind"}),
	}
	return m, metrics.Register(reg, m.playersOnline, m.registrationsWaiting, m.messages, m.slowConsumers, m.welcomeBytes)
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

// OnlineChanged moves the online gauge by delta.
func (m *Metrics) OnlineChanged(delta int) { m.playersOnline.Add(float64(delta)) }

// RegistrationsWaiting moves the gauge of logins waiting to register with social by delta.
func (m *Metrics) RegistrationsWaiting(delta int) {
	m.registrationsWaiting.Add(float64(delta))
}
