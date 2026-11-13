package kernel

import (
	"errors"

	"finga/pkg/transient"
)

// Rule is a failure a caller gets back as it is: the request breaks a rule of the game or of the contract, or a limit.
// Most rules are final, and no retry passes; a retryable one, a module busy or stopping, is also transient.ErrUnavailable.
type Rule struct {
	reason    string
	retryable bool
}

// NewRule is the rule named reason, in snake case and unique across modules: metrics and clients tell failures apart by it.
func NewRule(reason string) *Rule { return &Rule{reason: reason} }

// Retryable marks a rule a later try may pass, as it's declared, so a retry keyed on transient.ErrUnavailable retries it too.
func (r *Rule) Retryable() *Rule {
	r.retryable = true
	return r
}

func (r *Rule) Error() string { return r.reason }

// Is finds transient.ErrUnavailable in a retryable rule.
func (r *Rule) Is(target error) bool { return r.retryable && target == transient.ErrUnavailable }

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
