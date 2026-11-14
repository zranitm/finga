package e2e_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/cmd/finga/app"
	"finga/config"
	"finga/internal/host"
	"finga/pkg/runner/runnertest"
	"finga/pkg/uuidv7"
)

// seededStores are the rows the game starts over: in each store it prunes, one on either side of the cutoff.
type seededStores struct{ activeSession string }

// seedStores writes the rows before the game starts, as rows left by an earlier run.
func seedStores(ctx context.Context, url string) (seededStores, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return seededStores{}, err
	}
	defer pool.Close()
	account, ended, active := uuidv7.New(), uuidv7.New(), uuidv7.New()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO accounts (id, username, password_hash, created_at, updated_at) VALUES ($1, 'olda', 'x', now(), now())`, []any{account}},
		{`INSERT INTO sessions (id, account_id, token_hash, created_at, updated_at, expires_at, revoked_at) VALUES
			($2, $1, '\x01', now() - interval '40 days', now() - interval '31 days', now() - interval '20 days', now() - interval '31 days'),
			($3, $1, '\x02', now(), now(), now() + interval '7 days', NULL)`, []any{account, ended, active}},
		{`INSERT INTO idempotency_keys (owner, scope, key, request_hash, response, created_at, updated_at)
			VALUES ($1, 'item.add', 'old', '\x00', '\x00', now() - interval '25 hours', now() - interval '25 hours'), ($1, 'item.add', 'new', '\x00', '\x00', now(), now())`, []any{account.String()}},
		{`INSERT INTO outbox (event_id, topic, key, type, content_type, payload, occurred_at, published_at, created_at, updated_at)
			SELECT gen_random_uuid(), 'item', key, 'T', 'application/json', '{}', now() - created, now() - published, now() - created, now() - created
			FROM (VALUES ('published-old', interval '30 days', interval '25 hours'), ('published-new', interval '30 days', interval '23 hours'),
				('unpublished-old', interval '8 days', NULL), ('unpublished-new', interval '6 days', NULL)) AS r(key, created, published)`, nil},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			return seededStores{}, err
		}
	}
	return seededStores{activeSession: active.String()}, nil
}

// startedOver runs cmd/finga's app over seeded stores, with the outbox's unpublished TTL at ttl.
func startedOver(t *testing.T, ttl time.Duration) (*stack, seededStores) {
	t.Helper()
	var seeded seededStores
	s := newStackBuilt(t, "../data", func(ctx context.Context, h *host.Host, cfg config.Config) error {
		var err error
		if seeded, err = seedStores(ctx, cfg.Database.URL); err != nil {
			return err
		}
		cfg.Item.Outbox.UnpublishedKeep = ttl
		return app.Build(ctx, h, cfg)
	})
	return s, seeded
}

// left is what one column of a store holds, in order.
func (s *stack) left(t *testing.T, query string) []string {
	t.Helper()
	rows, err := s.db.Query(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The game prunes each store from its start, counted under its module and store: sessions ended a month ago, idempotency keys past the retry window, outbox rows published a day ago and unsent ones past the Stage 1 TTL.
func TestTheGamePrunesEachStoreFromItsStart(t *testing.T) {
	t.Parallel()
	s, seeded := startedOver(t, 7*24*time.Hour)
	for _, want := range []string{
		`finga_retention_deleted_total{module="account",store="sessions"} 1` + "\n",
		`finga_retention_deleted_total{module="item",store="idempotency_keys"} 1` + "\n",
		`finga_retention_deleted_total{module="item",store="outbox"} 2` + "\n",
	} {
		runnertest.GetUntil(t, s.ops+"/metrics", want)
	}
	if got := s.left(t, `SELECT id::text FROM sessions`); !slices.Equal(got, []string{seeded.activeSession}) {
		t.Errorf("sessions left %v, want only the active one", got)
	}
	if got := s.left(t, `SELECT key FROM idempotency_keys`); !slices.Equal(got, []string{"new"}) {
		t.Errorf("keys left %v, want only the fresh one", got)
	}
	if got := s.left(t, `SELECT key FROM outbox ORDER BY key`); !slices.Equal(got, []string{"published-new", "unpublished-new"}) {
		t.Errorf("outbox rows left %v, want the new ones", got)
	}
	if _, body := runnertest.Get(t, s.ops+"/metrics"); !strings.Contains(body, `finga_outbox_unpublished{module="item"} `) {
		t.Errorf("the ops page has no unpublished outbox gauge")
	}
}

// With the unpublished TTL off, as Stage 2's relay needs, an unsent row stays however old.
func TestAnUnsentOutboxRowStaysWithTheTTLOff(t *testing.T) {
	t.Parallel()
	s, _ := startedOver(t, 0)
	runnertest.GetUntil(t, s.ops+"/metrics", `finga_retention_deleted_total{module="item",store="outbox"} 1`+"\n")
	if got := s.left(t, `SELECT key FROM outbox ORDER BY key`); !slices.Equal(got, []string{"published-new", "unpublished-new", "unpublished-old"}) {
		t.Errorf("outbox rows left %v, want every unsent one", got)
	}
}

// The world's metrics show on the game's ops page: each map's tick, and every reason a save is dropped for, at 0 from the start.
func TestTheOpsPageShowsTheWorldFromItsStart(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	runnertest.GetUntil(t, s.ops+"/metrics", `finga_tick_duration_seconds_count{map="town"}`)
	_, body := runnertest.Get(t, s.ops+"/metrics")
	for _, reason := range []string{"refused", "unexplained", "shutdown"} {
		if want := `finga_saves_dropped_total{reason="` + reason + `"} 0`; !strings.Contains(body, want) {
			t.Errorf("the ops page lacks %s", want)
		}
	}
}
