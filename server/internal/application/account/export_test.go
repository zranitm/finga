package account

import (
	"finga/pkg/hub"
)

// ErrorTable is the facade's table, for the error table test, which runs as a caller would.
var ErrorTable = errorTable

// Streams is a module with only its sessions stream, and the notifier its application tells, for the revoke reason test.
func Streams() (*Module, Notifier) {
	sessions := hub.New[Event](sessionsQueue)
	return &Module{sessions: sessions}, notifier{sessions: sessions}
}

// Cases, CasesDeps and NewCases give the external tests the use cases, which the module does not export.
type (
	Cases     = cases
	CasesDeps = casesDeps
)

var NewCases = newCases
