// Package local adapts the account, character, item, world and social modules, in the same process, to the gateway's ports, one file per module.
package local

import (
	"errors"

	"finga/internal/kernel"
	"finga/pkg/transient"
	"finga/services/gateway/internal/model"
)

// wrap names a module's failure as the client sees it, with the kind the use cases act on, as errmap.Wrap does over gRPC; nil stays nil.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	e := &model.ServiceError{Public: model.PublicOf(err), Err: err}
	switch {
	case errors.Is(err, kernel.ErrNotSubscribed):
		e.Kind = model.ErrNotSubscribed
	case errors.Is(err, transient.ErrUnavailable):
		e.Kind = model.ErrUnavailable // with a rule too: a busy or stopping world is worth waiting for
	}
	return e
}
