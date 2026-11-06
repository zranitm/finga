package dbx

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Stamp is a row's created_at or updated_at from the app's clock; a zero Stamp writes NULL, so a NOT NULL
// column refuses a write that left it out.
type Stamp time.Time

// Now is the app clock's stamp, at the database's microsecond precision.
func Now() Stamp { return Stamp(time.Now().Truncate(time.Microsecond)) }

// Time is the stamp's time.
func (s Stamp) Time() time.Time { return time.Time(s) }

// TimestamptzValue encodes the stamp for pgx.
func (s Stamp) TimestamptzValue() (pgtype.Timestamptz, error) {
	return pgtype.Timestamptz{Time: time.Time(s), Valid: !time.Time(s).IsZero()}, nil
}

// ScanTimestamptz decodes a stamp; a NULL is an error, as stamped columns are NOT NULL.
func (s *Stamp) ScanTimestamptz(v pgtype.Timestamptz) error {
	if !v.Valid {
		return errors.New("dbx: a NULL stamp")
	}
	*s = Stamp(v.Time)
	return nil
}
