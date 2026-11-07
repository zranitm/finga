package outboxpg_test

import (
	"context"
	"errors"
	"os"
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
