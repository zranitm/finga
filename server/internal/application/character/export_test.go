package character

// ErrorTable is the facade's table, for the error table test, which runs as a caller would.
var ErrorTable = errorTable

// Cases, CasesDeps and NewCases give the external tests the use cases, which the module does not export.
type (
	Cases     = cases
	CasesDeps = casesDeps
)

var NewCases = newCases
