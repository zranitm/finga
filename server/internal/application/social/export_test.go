package social

// ErrorTable is the facade's table, for the error table test, which runs as a caller would.
var ErrorTable = errorTable

// memory is the party store the internal tests run on, set by main_test.go from the repository, which imports this package.
var memory func() PartyStore

// SetMemory gives the internal tests the repository's party store.
func SetMemory(f func() PartyStore) { memory = f }

// The party and chat use cases, which the module does not export, for the external tests.
type (
	PartyCases     = partyCases
	PartyCasesDeps = partyCasesDeps
	ChatCases      = chatCases
	ChatCasesDeps  = chatCasesDeps
)

var (
	NewPartyCases = newPartyCases
	NewChatCases  = newChatCases
)
