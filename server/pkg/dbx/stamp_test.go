package dbx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"finga/pkg/dbx"
)

func TestStampAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE stamped (id int PRIMARY KEY, created_at timestamptz NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	t.Run("a stamp reads back as written", func(t *testing.T) {
		now := dbx.Now()
		if _, err := pool.Exec(ctx, `INSERT INTO stamped VALUES (1, $1)`, now); err != nil {
			t.Fatal(err)
		}
		var got dbx.Stamp
		if err := pool.QueryRow(ctx, `SELECT created_at FROM stamped WHERE id = 1`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if !got.Time().Equal(now.Time()) {
			t.Fatalf("read %v, wrote %v", got.Time(), now.Time())
		}
	})

	t.Run("a write that left its stamp out is refused", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO stamped VALUES (2, $1)`, dbx.Stamp{})
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "23502" {
			t.Fatalf("err = %v, want a not-null violation", err)
		}
	})

	t.Run("a NULL is no stamp", func(t *testing.T) {
		var got dbx.Stamp
		if err := pool.QueryRow(ctx, `SELECT NULL::timestamptz`).Scan(&got); err == nil {
			t.Fatalf("scanned NULL as %v", got.Time())
		}
	})
}
