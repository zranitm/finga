package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/pgtest"
	"finga/pkg/transient"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
	"finga/services/item/migrations"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, pgtest.Migrations(migrations.FS, migrations.Table)) // the item service's set only
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

var t0 = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func insertItem(t *testing.T, store *Postgres, it model.Item) {
	t.Helper()
	err := store.InTx(context.Background(), func(uow application.UnitOfWork) error { return uow.Items().Insert(context.Background(), it) })
	if err != nil {
		t.Fatal(err)
	}
}

func TestRepository(t *testing.T) {
	ctx := context.Background()
	store := NewPostgres(cluster.DB(t), nil)
	alice, bob := kernel.NewCharacterID(), kernel.NewCharacterID()
	sword := model.Item{ID: kernel.NewItemID(), TemplateID: "wooden_sword", Owner: alice, Tab: gamedata.Equip, Slot: 0, Quantity: 1}
	potions := model.Item{ID: kernel.NewItemID(), TemplateID: "red_potion", Owner: alice, Tab: gamedata.Use, Slot: 3, Quantity: 40}
	insertItem(t, store, sword)
	insertItem(t, store, potions)

	t.Run("LockOwned on your own item", func(t *testing.T) {
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			it, err := uow.Items().LockOwned(ctx, sword.ID, alice)
			if err != nil {
				return err
			}
			if it.TemplateID != "wooden_sword" || it.Tab != gamedata.Equip || it.Slot != 0 || it.Quantity != 1 {
				t.Fatalf("item %+v", it)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("LockOwned on someone else's item gives ErrNotFound", func(t *testing.T) {
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			_, err := uow.Items().LockOwned(ctx, sword.ID, bob)
			return err
		})
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("occupied slots and stack quantities", func(t *testing.T) {
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			inv, err := uow.Items().LockInventory(ctx, alice)
			if err != nil {
				return err
			}
			if len(inv) != 2 || inv[0].Tab != gamedata.Equip || inv[1].Tab != gamedata.Use || inv[1].Slot != 3 || inv[1].Quantity != 40 {
				t.Fatalf("inventory %+v", inv)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		list, err := store.Inventory(ctx, alice)
		if err != nil || len(list.Items) != 2 {
			t.Fatalf("Inventory %v, %v", list, err)
		}
		if none, err := store.Inventory(ctx, bob); err != nil || len(none.Items) != 0 || none.Revision != 0 {
			t.Fatalf("empty inventory %v, %v", none, err)
		}
	})
	t.Run("a full tab", func(t *testing.T) {
		full := kernel.NewCharacterID()
		for slot := range model.SlotsPerTab {
			insertItem(t, store, model.Item{ID: kernel.NewItemID(), TemplateID: "slime_gel", Owner: full, Tab: gamedata.Etc, Slot: slot, Quantity: 100})
		}
		// The lock returns the full tab in slot order, which the free-slot search relies on.
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			inv, err := uow.Items().LockInventory(ctx, full)
			if err != nil {
				return err
			}
			if len(inv) != model.SlotsPerTab {
				t.Fatalf("%d rows for a full tab", len(inv))
			}
			for slot, it := range inv {
				if it.Tab != gamedata.Etc || it.Slot != slot {
					t.Fatalf("row %d is %s/%d", slot, it.Tab, it.Slot)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("two items in one slot are rejected as ErrSlotTaken", func(t *testing.T) {
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			return uow.Items().Insert(ctx, model.Item{ID: kernel.NewItemID(), TemplateID: "iron_sword", Owner: alice, Tab: gamedata.Equip, Slot: 0, Quantity: 1})
		})
		if !errors.Is(err, model.ErrSlotTaken) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("quantity, slot, destroy and events", func(t *testing.T) {
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			if err := uow.Items().SetQuantity(ctx, potions.ID, 35); err != nil {
				return err
			}
			if err := uow.Items().SetSlot(ctx, potions.ID, gamedata.Use, 7); err != nil {
				return err
			}
			if err := uow.Events().Append(ctx, model.Event{ItemID: potions.ID, Kind: model.EventDiscarded, QuantityDelta: -5, FromOwner: &alice, Cause: model.Cause{Type: model.CausePlayer}, OccurredAt: t0}); err != nil {
				return err
			}
			return uow.Items().Destroy(ctx, sword.ID)
		})
		if err != nil {
			t.Fatal(err)
		}
		read, _ := store.Inventory(ctx, alice)
		if list := read.Items; len(list) != 1 || list[0].ID != potions.ID || list[0].Quantity != 35 || list[0].Slot != 7 {
			t.Fatalf("inventory %+v", list)
		}
		err = store.InTx(ctx, func(uow application.UnitOfWork) error {
			_, err := uow.Items().LockOwned(ctx, sword.ID, alice)
			return err
		})
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("a destroyed item is still lockable: %v", err)
		}
		var kind string
		var delta int
		if err := store.pool.QueryRow(ctx, `SELECT kind, quantity_delta FROM item_events WHERE item_id = $1`, potions.ID).Scan(&kind, &delta); err != nil {
			t.Fatal(err)
		}
		if kind != "discarded" || delta != -5 {
			t.Fatalf("event %s %d", kind, delta)
		}
	})
	t.Run("updates of an unknown item give ErrNotFound", func(t *testing.T) {
		for name, fn := range map[string]func(uow application.UnitOfWork) error{
			"SetQuantity": func(uow application.UnitOfWork) error { return uow.Items().SetQuantity(ctx, kernel.NewItemID(), 1) },
			"SetSlot": func(uow application.UnitOfWork) error {
				return uow.Items().SetSlot(ctx, kernel.NewItemID(), gamedata.Use, 1)
			},
			"Destroy": func(uow application.UnitOfWork) error { return uow.Items().Destroy(ctx, kernel.NewItemID()) },
		} {
			if err := store.InTx(ctx, fn); !errors.Is(err, model.ErrNotFound) {
				t.Fatalf("%s: %v", name, err)
			}
		}
	})
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"nil", nil, nil},
		{"no rows", pgx.ErrNoRows, model.ErrNotFound},
		{"one item per slot", &pgconn.PgError{Code: "23505", ConstraintName: "one_item_per_slot"}, model.ErrSlotTaken},
		{"quantity check", &pgconn.PgError{Code: "23514", ConstraintName: "items_quantity_check"}, model.ErrBadQuantity},
		{"another module's constraint passes through", &pgconn.PgError{Code: "23505", ConstraintName: "characters_name_key"}, nil},
		{"a serialization failure past the retries", &pgconn.PgError{Code: "40001"}, transient.ErrUnavailable},
		{"the server starting up", &pgconn.PgError{Code: "57P03"}, transient.ErrUnavailable},
		{"a lock timeout", &pgconn.PgError{Code: "55P03"}, transient.ErrUnavailable},
		{"a statement timeout", &pgconn.PgError{Code: "57014"}, transient.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := failures.Classify(tt.err)
			if tt.want == nil {
				if got != nil && !errors.Is(got, tt.err) {
					t.Fatalf("got %v, want %v unchanged", got, tt.err)
				}
				return
			}
			if !errors.Is(got, tt.want) || !errors.Is(got, tt.err) {
				t.Fatalf("got %v, want %v wrapping the cause", got, tt.want)
			}
		})
	}
}
