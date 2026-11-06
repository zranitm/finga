package dbx

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Errors are the sentinels a repository's database failures become.
type Errors struct {
	NotFound    error            // no row
	TimedOut    error            // a lock wait or a statement past its timeout
	Constraints map[string]error // a violated constraint, by name
}

// Classify wraps err in its sentinel, keeping the cause; any other error passes through as it is, so InTx still sees a code it retries.
func (e Errors) Classify(err error) error {
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%w: %w", e.NotFound, err)
	case TimedOut(err):
		return fmt.Errorf("%w: %w", e.TimedOut, err)
	case errors.As(err, &pgErr) && e.Constraints[pgErr.ConstraintName] != nil:
		return fmt.Errorf("%w: %w", e.Constraints[pgErr.ConstraintName], err)
	default:
		return err
	}
}

// RowsOrNotFound is pgx.ErrNoRows for a statement that changed no row, else err.
func RowsOrNotFound(n int64, err error) error {
	if err == nil && n == 0 {
		return pgx.ErrNoRows
	}
	return err
}
