// Package gateway adapts the account, character, item, world and social modules, in the same process, to the gateway's ports, one file per module.
package account

import (
	"errors"
	"finga/internal/shared/failure"

	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// wrap names a module's failure as the client sees it, with the kind the use cases act on; nil stays nil.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	e := &failure.ServiceError{Public: failure.PublicOf(err), Err: err}
	switch {
	case errors.Is(err, kernel.ErrNotSubscribed):
		e.Kind = failure.ErrNotSubscribed
	case errors.Is(err, transient.ErrUnavailable):
		e.Kind = failure.ErrUnavailable // with a rule too: a busy or stopping world is worth waiting for
	}
	return e
}
