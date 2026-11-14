package world

import (
	"testing"

	"finga/internal/host"
	"finga/pkg/hub"
)

// Over is the module over an application a test built from fake ports, with the host module's calls and the maps' tick rate, as New builds it over the real ones.
func Over(t testing.TB, m *host.Module, svc service, tickRateHz int) *Module {
	t.Helper()
	calls, err := m.Calls(errorTable)
	if err != nil {
		t.Fatal(err)
	}
	return &Module{calls: calls, svc: svc, deliver: hub.New[Delivery](deliverQueue), tickRate: tickRateHz}
}

// ErrorTable is the facade's table, for the wire test, which runs as a caller would.
var ErrorTable = errorTable
