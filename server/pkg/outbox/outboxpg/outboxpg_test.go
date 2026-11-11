package outboxpg_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/pkg/dbx"
	"finga/pkg/outbox"
	"finga/pkg/outbox/outboxpg"
	"finga/pkg/outbox/outboxpg/outboxpgtest"
	"finga/pkg/pgtest"
	"finga/pkg/uuidv7"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, func(ctx context.Context, pool *pgxpool.Pool) error {
		_, err := pool.Exec(ctx, outboxpgtest.Schema)
		return err
	})
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

var errBoom = errors.New("boom")

type row struct {
	ID uuid.UUID
	outbox.Event
}

func rows(t *testing.T, pool *pgxpool.Pool) []row {
	t.Helper()
	rs, err := pool.Query(context.Background(), `SELECT event_id, topic, key, type, content_type, payload, occurred_at FROM outbox ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.ID, &r.Topic, &r.Key, &r.Type, &r.ContentType, &r.Payload, &r.OccurredAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestAdd(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	t0 := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	now := dbx.Now()

	t.Run("rows exist exactly when the transaction commits", func(t *testing.T) {
		err := dbx.InTx(ctx, pool, func(tx pgx.Tx) error {
			return outboxpg.NewWriter(tx, now).Add(ctx,
				outbox.Event{Topic: "item", Key: "item-1", Type: "ItemLooted", Payload: []byte(`{"item":"1"}`), OccurredAt: t0},
				outbox.Event{Topic: "item", Key: "item-2", Type: "ItemDiscarded"},
			)
		})
		if err != nil {
			t.Fatal(err)
		}
		got := rows(t, pool)
		if len(got) != 2 {
			t.Fatalf("%d rows, want 2", len(got))
		}
		for i, r := range got {
			if r.ID.Version() != 7 {
				t.Fatalf("row %d ID %v, want a UUIDv7", i, r.ID)
			}
		}
		if got[0].ID == got[1].ID {
			t.Fatalf("both rows have ID %v, want one each", got[0].ID)
		}
		if got[0].Type != "ItemLooted" || string(got[0].Payload) != `{"item":"1"}` || !got[0].OccurredAt.Equal(t0) || got[0].ContentType != "application/json" {
			t.Fatalf("row 0 %+v", got[0])
		}
		if string(got[1].Payload) != "{}" || !got[1].OccurredAt.Equal(now.Time()) {
			t.Fatalf("row 1 %+v: defaults not applied", got[1])
		}
		var stamped int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE created_at = $1 AND updated_at = $1`, now.Time()).Scan(&stamped); err != nil || stamped != 2 {
			t.Fatalf("%d rows carry the writer's stamp (%v), want 2", stamped, err)
		}
	})
	t.Run("a writer without a stamp writes nothing", func(t *testing.T) {
		if err := outboxpg.NewWriter(pool, dbx.Stamp{}).Add(ctx, outbox.Event{Topic: "item", Key: "x", Type: "Unstamped"}); err == nil {
			t.Fatal("an event without a stamp was written")
		}
	})
	t.Run("none after a rollback", func(t *testing.T) {
		err := dbx.InTx(ctx, pool, func(tx pgx.Tx) error {
			if err := outboxpg.NewWriter(tx, now).Add(ctx, outbox.Event{Topic: "item", Key: "x", Type: "Lost"}); err != nil {
				return err
			}
			return errBoom
		})
		if !errors.Is(err, errBoom) {
			t.Fatal(err)
		}
		if got := rows(t, pool); len(got) != 2 {
			t.Fatalf("%d rows after a rollback, want 2", len(got))
		}
	})
	t.Run("an event without a topic or type is refused", func(t *testing.T) {
		err := dbx.InTx(ctx, pool, func(tx pgx.Tx) error {
			return outboxpg.NewWriter(tx, now).Add(ctx, outbox.Event{Key: "x"})
		})
		if err == nil {
			t.Fatal("no error")
		}
	})
	t.Run("rows are unpublished until the relay arrives", func(t *testing.T) {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE published_at IS NULL`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("%d unpublished, want 2", n)
		}
	})
}

func TestUnpublishedCountsWhatNoRelayHasPublished(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := outboxpg.NewWriter(pool, dbx.Now()).Add(ctx, outbox.Event{Topic: "item", Key: "a", Type: "A"}, outbox.Event{Topic: "item", Key: "b", Type: "B"}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE outbox SET published_at = now(), updated_at = now() WHERE key = 'a'`); err != nil {
		t.Fatal(err)
	}
	if n, err := outboxpg.Unpublished(ctx, pool); err != nil || n != 1 {
		t.Fatalf("unpublished %d, %v; want 1", n, err)
	}
}

// stamped writes an event keyed key, created at created and published at published, or not at all when zero.
func stamped(t *testing.T, pool *pgxpool.Pool, key string, created, published time.Time) {
	t.Helper()
	var at *time.Time
	if !published.IsZero() {
		at = &published
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO outbox (event_id, topic, key, type, content_type, payload, occurred_at, published_at, created_at, updated_at)
		VALUES ($1, 'item', $2, 'T', 'application/json', '{}', $3, $4, $3, $3)`, uuidv7.New(), key, created, at); err != nil {
		t.Fatal(err)
	}
}

func keys(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rs, err := pool.Query(context.Background(), `SELECT key FROM outbox ORDER BY key`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rs, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Each rule compares the app's cutoff with the app's stamps, and leaves the other rule's rows alone.
func TestPruneDeletesOnlyItsRowsPastTheCutoff(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name  string
		prune func(context.Context, dbx.DBTX, time.Time, int) (int64, error)
		left  []string
	}{
		{"published rows past the grace", outboxpg.PrunePublished, []string{"published-new", "unpublished-new", "unpublished-old"}},
		{"unpublished rows past the TTL", outboxpg.PruneUnpublished, []string{"published-new", "published-old", "unpublished-new"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pool := cluster.DB(t)
			stamped(t, pool, "published-old", t0.Add(-30*time.Hour), t0.Add(-25*time.Hour))
			stamped(t, pool, "published-new", t0.Add(-30*time.Hour), t0.Add(-23*time.Hour)) // written long ago, published inside the grace
			stamped(t, pool, "unpublished-old", t0.Add(-25*time.Hour), time.Time{})
			stamped(t, pool, "unpublished-new", t0.Add(-23*time.Hour), time.Time{})
			n, err := tt.prune(ctx, pool, t0.Add(-24*time.Hour), 100)
			if err != nil || n != 1 {
				t.Fatalf("deleted %d, %v; want 1", n, err)
			}
			if got := keys(t, pool); !slices.Equal(got, tt.left) {
				t.Fatalf("left %v, want %v", got, tt.left)
			}
		})
	}
}

// A call deletes at most its limit, the oldest first, so retention's batches end on a short one.
func TestPruneDeletesABatchOldestFirst(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	for name, prune := range map[string]func(context.Context, dbx.DBTX, time.Time, int) (int64, error){"published": outboxpg.PrunePublished, "unpublished": outboxpg.PruneUnpublished} {
		t.Run(name, func(t *testing.T) {
			pool := cluster.DB(t)
			for _, key := range []string{"e", "c", "a", "d", "b"} { // written out of order; a is the oldest, e the newest
				at := t0.Add(-time.Duration(10-int(key[0]-'a')) * time.Hour)
				published := time.Time{}
				if name == "published" {
					published = at
				}
				stamped(t, pool, key, at, published)
			}
			var got []int64
			for range 4 {
				n, err := prune(ctx, pool, t0, 2)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, n)
				if len(got) == 1 {
					if left := keys(t, pool); !slices.Equal(left, []string{"c", "d", "e"}) {
						t.Fatalf("after one batch %v left; want the two oldest gone", left)
					}
				}
			}
			if !slices.Equal(got, []int64{2, 2, 1, 0}) {
				t.Fatalf("batches deleted %v, want 2, 2, 1, 0", got)
			}
		})
	}
}
