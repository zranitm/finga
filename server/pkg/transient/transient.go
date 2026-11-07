// Package transient names a failure a later try may clear, so a handler can map it without importing what failed.
package transient

import "errors"

// ErrUnavailable is a dependency busy or out of reach: a database past its lock or statement timeout, refusing connections, or restarting.
var ErrUnavailable = errors.New("busy or out of reach")
