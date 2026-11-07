package dbx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"finga/pkg/transient"
)

// Errors are the sentinels a repository's database failures become, besides transient.ErrUnavailable, which every repository shares.
type Errors struct {
	NotFound    error            // no row
	Constraints map[string]error // a violated constraint, by name
}

// Classify wraps err in its sentinel, keeping the cause: transient.ErrUnavailable for a lock or statement timeout, a conflict InTx's retries did not clear, or the database out of reach or short of resources; any other error passes through as it is.
func (e Errors) Classify(err error) error {
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%w: %w", e.NotFound, err)
	case timedOut(err), retryable(err), unavailable(err):
		return fmt.Errorf("%w: %w", transient.ErrUnavailable, err)
	case errors.As(err, &pgErr) && e.Constraints[pgErr.ConstraintName] != nil:
		return fmt.Errorf("%w: %w", e.Constraints[pgErr.ConstraintName], err)
	default:
		return err
	}
}

// unavailableClasses are the SQLSTATE classes of a server out of reach or short of something: 08 connection exceptions, 53 insufficient resources (disk, memory, connections), 58 system errors (I/O).
var unavailableClasses = []string{"08", "53", "58"}

// unavailableCodes are the other codes of a server out of reach: shutting down, crashed, starting up, or ending a session or transaction left idle past its timeout.
var unavailableCodes = []string{"57P01", "57P02", "57P03", "57P05", "25P03"}

// unavailable reports whether err is the database out of reach or short of resources: a connection refused or lost, a server shutting down, starting up, full or failing its disk; a later try may pass.
func unavailable(err error) bool {
	if _, ok := errors.AsType[*pgconn.ConnectError](err); ok {
		return true // the pool dials on a context of its own, so a deadline in here is the connect timeout, not the caller's
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false // the caller gave up, which its own code says
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		inClass := func(class string) bool { return strings.HasPrefix(pgErr.Code, class) }
		return slices.ContainsFunc(unavailableClasses, inClass) || slices.Contains(unavailableCodes, pgErr.Code)
	}
	_, isNet := errors.AsType[net.Error](err)
	return isNet || pgconn.SafeToRetry(err) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// RowsOrNotFound is pgx.ErrNoRows for a statement that changed no row, else err.
func RowsOrNotFound(n int64, err error) error {
	if err == nil && n == 0 {
		return pgx.ErrNoRows
	}
	return err
}
