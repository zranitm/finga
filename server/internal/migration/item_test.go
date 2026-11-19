package migration_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/migration"
	"finga/pkg/idempotency/idempotencypg/idempotencypgtest"
	"finga/pkg/outbox/outboxpg/outboxpgtest"
	"finga/pkg/pgtest"
	"finga/pkg/uuidv7"
)

// The outbox and idempotency tables here must match the Schema their test support holds.
func TestTheLibraryTablesMatchTheirSchemas(t *testing.T) {
	ctx := context.Background()
	migrated := cluster.DB(t)
	if err := pgtest.Migrations(migration.FS, migration.Table)(ctx, migrated); err != nil {
		t.Fatal(err)
	}
	reference := cluster.DB(t)
	for _, ddl := range []string{outboxpgtest.Schema, idempotencypgtest.Schema} {
		if _, err := reference.Exec(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"outbox", "idempotency_keys"} {
		t.Run(table, func(t *testing.T) {
			if got, want := shape(t, migrated, table), shape(t, reference, table); !slices.Equal(got, want) {
				t.Fatalf("migration:\n%v\nlibrary schema:\n%v", got, want)
			}
		})
	}
}

// shape lists a table's columns, in order and with their defaults, and its index definitions.
func shape(t *testing.T, pool *pgxpool.Pool, table string) []string {
	t.Helper()
	ctx := context.Background()
	var out []string
	rows, err := pool.Query(ctx, `
		SELECT column_name || ' ' || data_type || ' ' || is_nullable || coalesce(' ' || column_default, '')
		FROM information_schema.columns WHERE table_name = $1 ORDER BY ordinal_position`, table)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, cols...)
	rows, err = pool.Query(ctx, `SELECT indexdef FROM pg_indexes WHERE tablename = $1 ORDER BY indexname`, table)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return append(out, idx...)
}

// A swap moves the new item into the equip slot before the worn one leaves it, so one item per equip slot holds at commit, not row by row.
func TestOneItemPerEquipSlotHoldsAtCommit(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.Migrations(migration.FS, migration.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	owner, worn, next := uuidv7.New(), uuidv7.New(), uuidv7.New()
	if _, err := pool.Exec(ctx, `INSERT INTO items (id, template_id, owner_id, location, tab, slot, equip_slot, quantity, created_at, updated_at) VALUES
		($1, 'wooden_sword', $3, 'equipped', NULL, NULL, 'weapon', 1, now(), now()),
		($2, 'iron_sword', $3, 'inventory', 'equip', 0, NULL, 1, now(), now())`, worn, next, owner); err != nil {
		t.Fatal(err)
	}
	type change struct {
		sql string
		id  uuid.UUID
	}
	const (
		wear    = `UPDATE items SET location = 'equipped', tab = NULL, slot = NULL, equip_slot = 'weapon' WHERE id = $1`
		takeOff = `UPDATE items SET location = 'inventory', tab = 'equip', slot = 0, equip_slot = NULL WHERE id = $1`
	)
	commit := func(changes ...change) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		for _, c := range changes {
			if _, err := tx.Exec(ctx, c.sql, c.id); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	}
	t.Run("two worn in one slot fail at commit", func(t *testing.T) {
		err := commit(change{wear, next})
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.ConstraintName != "one_item_per_equip_slot" {
			t.Fatalf("err = %v, want one_item_per_equip_slot", err)
		}
	})
	t.Run("a swap commits", func(t *testing.T) {
		if err := commit(change{wear, next}, change{takeOff, worn}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a worn item names its slot, and only a worn one", func(t *testing.T) {
		for _, c := range []change{
			{`UPDATE items SET equip_slot = NULL WHERE id = $1`, next},
			{`UPDATE items SET equip_slot = 'hat' WHERE id = $1`, worn},
		} {
			if err := commit(c); err == nil {
				t.Fatalf("%s committed", c.sql)
			}
		}
	})
}

// A wardrobe entry names its item exactly when an item gave it, and its rank, quest or feat otherwise, once for each piece.
func TestAWardrobeEntryNamesWhatGaveIt(t *testing.T) {
	ctx := context.Background()
	pool := migratedDB(t)
	account, item := uuidv7.New(), uuidv7.New()
	insert := func(piece, cause string, ref *string, item *uuid.UUID) error {
		_, err := pool.Exec(ctx, `INSERT INTO account_pieces (account_id, piece_id, cause_type, cause_ref, item_id, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, now(), now())`, account, piece, cause, ref, item)
		return err
	}
	silver := "silver"
	if err := insert("knight_plate", "item", nil, &item); err != nil {
		t.Fatal(err)
	}
	if err := insert("knight_helm", "rank", &silver, nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, piece, cause string
		ref                *string
		item               *uuid.UUID
		want               string
	}{
		{"an item cause with no item", "mage_robe", "item", nil, nil, "account_pieces_item"},
		{"a rank cause naming an item", "mage_robe", "rank", &silver, &item, "account_pieces_item"},
		{"a rank cause naming no rank", "mage_robe", "rank", nil, nil, "account_pieces_item"},
		{"a cause the wardrobe doesn't know", "mage_robe", "gacha", &silver, nil, "account_pieces_cause"},
		{"a piece collected twice", "knight_plate", "rank", &silver, nil, "account_pieces_pkey"},
	} {
		if err := insert(c.piece, c.cause, c.ref, c.item); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s stored: %v, want %s", c.name, err, c.want)
		}
	}
}
