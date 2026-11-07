package dbx

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Locked runs fn holding goose's session lock for migrations, on a connection of its own beside pool's, so two runs at once take turns.
func Locked(ctx context.Context, pool *pgxpool.Pool, fn func() error) (err error) {
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 300)) // tried each second, for five minutes
	if err != nil {
		return fmt.Errorf("migrations: lock: %w", err)
	}
	db := stdlib.OpenDB(*pool.Config().ConnConfig.Copy())
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrations: lock: %w", err)
	}
	defer conn.Close()
	if err := locker.SessionLock(ctx, conn); err != nil {
		return fmt.Errorf("migrations: lock: %w", err)
	}
	defer func() {
		if unlockErr := locker.SessionUnlock(context.WithoutCancel(ctx), conn); unlockErr != nil && err == nil {
			err = fmt.Errorf("migrations: unlock: %w", unlockErr)
		}
	}()
	return fn()
}

// Migrate applies fsys's pending goose migrations, versioned in table, and reports how many ran.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, table string) (int, error) {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithTableName(table))
	if err != nil {
		return 0, fmt.Errorf("migrations %s: %w", table, err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrations %s: %w", table, err)
	}
	return len(results), nil
}

// TableExists reports whether name is a table on the search path.
func TableExists(ctx context.Context, db DBTX, name string) (bool, error) {
	var found bool
	err := db.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&found)
	return found, err
}
