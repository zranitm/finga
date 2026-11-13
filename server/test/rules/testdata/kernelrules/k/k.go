// Package k declares rules for the rule-reason check: one fine, and one of each kind it reports.
package k

// Rule is a failure named by its reason.
type Rule struct{ reason string }

// NewRule is the rule named reason.
func NewRule(reason string) *Rule { return &Rule{reason: reason} }

func (r *Rule) Error() string { return r.reason }

var (
	ErrFine     = NewRule("fine")
	ErrSoldOut  = NewRule("sold_out")
	ErrGone     = NewRule("sold_out")
	ErrOK       = NewRule("ok")
	ErrBadName  = NewRule("Bad Name")
	ErrComputed = NewRule(name())
)

func name() string { return "made_up" }
