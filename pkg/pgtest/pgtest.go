// Package pgtest gives tests a real Postgres: one server per test run,
// started with Testcontainers (or an existing server named by PGTEST_URL),
// a template database prepared once by a migrate function, and a fresh copy
// of the template for every test. Imported by tests only.
//
// Typical use, in a package's TestMain:
//
//	cluster, err := pgtest.Start(ctx, migrations.Up)
//	...
//	pool := cluster.DB(t) // in each test; closed and dropped by t.Cleanup
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	// EnvURL names an existing server to use instead of a container, for
	// example the Compose Postgres: postgres://finga:finga@localhost:5432/finga?sslmode=disable.
	// The user must be allowed to create databases.
	EnvURL = "PGTEST_URL"

	image        = "postgres:17-alpine"
	templateName = "pgtest_template"
	adminDB      = "postgres"
	poolConns    = 4
)

// MigrateFunc prepares the template database, typically migrations.Apply.
// nil leaves it empty.
type MigrateFunc func(ctx context.Context, pool *pgxpool.Pool) error

// Cluster is one Postgres server with a prepared template database.
type Cluster struct {
	container *postgres.PostgresContainer // nil when PGTEST_URL is used
	base      *url.URL                    // server URL; the path is replaced per database
	admin     *pgxpool.Pool               // connected to the postgres database
	seq       atomic.Int64
	run       string // random per Start, so parallel packages never collide
}

// Start brings up the server, creates the template database and runs
// migrate on it. Close releases everything.
func Start(ctx context.Context, migrate MigrateFunc) (*Cluster, error) {
	c := &Cluster{run: randomHex(4)}
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	if err := c.prepareTemplate(ctx, migrate); err != nil {
		_ = c.Close(ctx)
		return nil, err
	}
	return c, nil
}

func (c *Cluster) connect(ctx context.Context) error {
	raw := os.Getenv(EnvURL)
	if raw == "" {
		container, err := postgres.Run(ctx, image,
			postgres.WithDatabase(adminDB), postgres.WithUsername("pgtest"), postgres.WithPassword("pgtest"),
			postgres.BasicWaitStrategies())
		if err != nil {
			return fmt.Errorf("pgtest: start container: %w", err)
		}
		c.container = container
		raw, err = container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			_ = testcontainers.TerminateContainer(container)
			return fmt.Errorf("pgtest: connection string: %w", err)
		}
	}
	base, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("pgtest: %s: %w", EnvURL, err)
	}
	c.base = base
	c.admin, err = pgxpool.New(ctx, c.urlFor(adminDB))
	if err != nil {
		return fmt.Errorf("pgtest: connect: %w", err)
	}
	if err := c.admin.Ping(ctx); err != nil {
		return fmt.Errorf("pgtest: ping: %w", err)
	}
	return nil
}

func (c *Cluster) prepareTemplate(ctx context.Context, migrate MigrateFunc) error {
	tmpl := c.templateName()
	if _, err := c.admin.Exec(ctx, `DROP DATABASE IF EXISTS `+quote(tmpl)+` WITH (FORCE)`); err != nil {
		return fmt.Errorf("pgtest: drop old template: %w", err)
	}
	if _, err := c.admin.Exec(ctx, `CREATE DATABASE `+quote(tmpl)); err != nil {
		return fmt.Errorf("pgtest: create template: %w", err)
	}
	if migrate == nil {
		return nil
	}
	pool, err := pgxpool.New(ctx, c.urlFor(tmpl))
	if err != nil {
		return fmt.Errorf("pgtest: connect to template: %w", err)
	}
	defer pool.Close() // CREATE DATABASE ... TEMPLATE needs no open connections to it
	if err := migrate(ctx, pool); err != nil {
		return fmt.Errorf("pgtest: migrate template: %w", err)
	}
	return nil
}

// DB creates a fresh database from the template and returns a pool on it.
// The pool is closed and the database dropped when t ends.
func (c *Cluster) DB(t testing.TB) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("pgtest_%s_%d", c.run, c.seq.Add(1))
	if _, err := c.admin.Exec(ctx, `CREATE DATABASE `+quote(name)+` TEMPLATE `+quote(c.templateName())); err != nil {
		t.Fatalf("pgtest: create database: %v", err)
	}
	cfg, err := pgxpool.ParseConfig(c.urlFor(name))
	if err != nil {
		t.Fatalf("pgtest: %v", err)
	}
	cfg.MaxConns = poolConns
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pgtest: connect: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = c.admin.Exec(ctx, `DROP DATABASE IF EXISTS `+quote(name)+` WITH (FORCE)`)
	})
	return pool
}

// URL is the connection URL of a fresh database from the template, for code
// that opens its own pool (dbx.NewPool, cmd/migrate). Dropped when t ends.
func (c *Cluster) URL(t testing.TB) string {
	t.Helper()
	pool := c.DB(t)
	return c.urlFor(pool.Config().ConnConfig.Database)
}

// Close drops the template, closes the admin pool, and stops the container.
func (c *Cluster) Close(ctx context.Context) error {
	var errs []string
	if c.admin != nil {
		if _, err := c.admin.Exec(ctx, `DROP DATABASE IF EXISTS `+quote(c.templateName())+` WITH (FORCE)`); err != nil {
			errs = append(errs, err.Error())
		}
		c.admin.Close()
	}
	if c.container != nil {
		if err := testcontainers.TerminateContainer(c.container); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("pgtest: close: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (c *Cluster) templateName() string { return templateName + "_" + c.run }

func (c *Cluster) urlFor(db string) string {
	u := *c.base
	u.Path = "/" + db
	return u.String()
}

func quote(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
