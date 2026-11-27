// Package account is the account module's Prometheus metrics, the observer its connections report to.
package account

import (
	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability/metrics"
)

// Metrics are the connections': the players online and the logins waiting to register with the social module.
type Metrics struct {
	playersOnline        prometheus.Gauge
	registrationsWaiting prometheus.Gauge
}

// New registers the connections' metrics on reg; registering twice on one registry fails.
func New(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		playersOnline: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metrics.Namespace, Name: "players_online", Help: "Players whose Hello joined them to a map.",
		}),
		registrationsWaiting: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metrics.Namespace, Name: "social_registrations_waiting", Help: "Logins whose registration with the social module has not landed yet, retries included.",
		}),
	}
	return m, metrics.Register(reg, m.playersOnline, m.registrationsWaiting)
}

// OnlineChanged moves the online gauge by delta.
func (m *Metrics) OnlineChanged(delta int) { m.playersOnline.Add(float64(delta)) }

// RegistrationsWaiting moves the gauge of logins waiting to register with social by delta.
func (m *Metrics) RegistrationsWaiting(delta int) {
	m.registrationsWaiting.Add(float64(delta))
}
