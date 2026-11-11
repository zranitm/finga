// Package promtest reads a metric's value back from a registry, for tests; only tests import it.
package promtest

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Sample is the value of the series name{labels} on reg: a counter's or gauge's value, a histogram's count; 0 when there is none.
func Sample(t testing.TB, reg *prometheus.Registry, name string, labels ...string) float64 {
	t.Helper()
	v, _ := Series(t, reg, name, labels...)
	return v
}

// Series is the value of the series name{labels} on reg, as Sample, and whether reg has it.
func Series(t testing.TB, reg *prometheus.Registry, name string, labels ...string) (float64, bool) {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if matches(m, labels) {
				switch {
				case m.GetCounter() != nil:
					return m.GetCounter().GetValue(), true
				case m.GetGauge() != nil:
					return m.GetGauge().GetValue(), true
				case m.GetHistogram() != nil:
					return float64(m.GetHistogram().GetSampleCount()), true
				}
			}
		}
	}
	return 0, false
}

func matches(m *dto.Metric, labels []string) bool {
	have := map[string]string{}
	for _, l := range m.GetLabel() {
		have[l.GetName()] = l.GetValue()
	}
	for i := 0; i+1 < len(labels); i += 2 {
		if have[labels[i]] != labels[i+1] {
			return false
		}
	}
	return true
}

// Names are the metric families reg has.
func Names(t testing.TB, reg *prometheus.Registry) map[string]bool {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, f := range families {
		out[f.GetName()] = true
	}
	return out
}
