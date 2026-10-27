package outbox_test

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
	"finga/pkg/pgtest"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, func(ctx context.Context, pool *pgxpool.Pool) error {
		_, err := pool.Exec(ctx, outbox.Schema)
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

func rows(t *testing.T, pool *pgxpool.Pool) []outbox.Event {
	t.Helper()
	rs, err := pool.Query(context.Background(), `SELECT event_id, topic, key, type, content_type, payload, occurred_at FROM outbox ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []outbox.Event
	for rs.Next() {
		var e outbox.Event
		if err := rs.Scan(&e.ID, &e.Topic, &e.Key, &e.Type, &e.ContentType, &e.Payload, &e.OccurredAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func TestAdd(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	t0 := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	given := uuid.New()

	t.Run("rows exist exactly when the transaction commits", func(t *testing.T) {
		err := dbx.InTx(ctx, pool, func(tx pgx.Tx) error {
			return outbox.NewWriter(tx).Add(ctx,
				outbox.Event{ID: given, Topic: "item", Key: "item-1", Type: "ItemLooted", Payload: []byte(`{"item":"1"}`), OccurredAt: t0},
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
		if got[0].ID != given || got[0].Type != "ItemLooted" || string(got[0].Payload) != `{"item":"1"}` || !got[0].OccurredAt.Equal(t0) || got[0].ContentType != "application/json" {
			t.Fatalf("row 0 %+v", got[0])
		}
		if got[1].ID == uuid.Nil || got[1].ID.Version() != 7 || string(got[1].Payload) != "{}" || got[1].OccurredAt.IsZero() {
			t.Fatalf("row 1 %+v: defaults not applied", got[1])
		}
	})
	t.Run("none after a rollback", func(t *testing.T) {
		err := dbx.InTx(ctx, pool, func(tx pgx.Tx) error {
			if err := outbox.NewWriter(tx).Add(ctx, outbox.Event{Topic: "item", Key: "x", Type: "Lost"}); err != nil {
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
	t.Run("a duplicate event ID is rejected", func(t *testing.T) {
		err := dbx.InTx(ctx, pool, func(tx pgx.Tx) error {
			return outbox.NewWriter(tx).Add(ctx, outbox.Event{ID: given, Topic: "item", Key: "x", Type: "Again"})
		})
		if err == nil {
			t.Fatal("no error")
		}
	})
	t.Run("an event without a topic or type is refused", func(t *testing.T) {
		err := dbx.InTx(ctx, pool, func(tx pgx.Tx) error {
			return outbox.NewWriter(tx).Add(ctx, outbox.Event{Key: "x"})
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
