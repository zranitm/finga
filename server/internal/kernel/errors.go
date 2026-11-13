package kernel

import "errors"

// Rule is a failure a caller gets back as it is and never retries: the request breaks a rule of the game or of the contract, or a limit.
type Rule struct{ reason string }

// NewRule is the rule named reason, in snake case and unique across modules: metrics and clients tell failures apart by it.
func NewRule(reason string) *Rule { return &Rule{reason: reason} }

func (r *Rule) Error() string { return r.reason }

// badID is the rule ErrBadID breaks; ErrBadID stays typedid's error, so an ID decoded from JSON matches it.
var badID = NewRule("bad_id")

// ReasonOf is the reason of the rule err breaks, if it breaks one.
func ReasonOf(err error) (string, bool) {
	var r *Rule
	switch {
	case errors.As(err, &r):
		return r.reason, true
	case errors.Is(err, ErrBadID):
		return badID.reason, true
	}
	return "", false
}

// ErrInternal is a failure inside a module, a recovered panic among them; the caller learns no more, and the module logs the cause.
var ErrInternal = errors.New("internal error")
