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
	"finga/services/character/internal/application"
	"finga/services/character/internal/model"
	"finga/services/character/migrations"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, pgtest.Migrations(migrations.FS, migrations.Table)) // the character service's set only
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

func sample(account kernel.AccountID, name string) model.Character {
	return model.Character{
		ID: kernel.NewCharacterID(), AccountID: account, Name: name, Class: kernel.Warrior, Level: 1, Exp: 0,
		Stats: gamedata.Stats{STR: 12, DEX: 5, INT: 4, AGI: 5, VIT: 10}, HP: 204, MP: 28,
		MapID: "town", X: 416, Y: 608,
	}
}

// insert returns the character as stored, with the updated_at the database gave it.
func insert(t *testing.T, store *Postgres, c model.Character) model.Character {
	t.Helper()
	var stored model.Character
	err := store.InTx(context.Background(), func(uow application.UnitOfWork) error {
		var err error
		stored, err = uow.Characters().Insert(context.Background(), c)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func TestRepository(t *testing.T) {
	ctx := context.Background()
	store := NewPostgres(cluster.DB(t), nil)
	alice, bob := kernel.NewAccountID(), kernel.NewAccountID()
	ayla := sample(alice, "Ayla")
	stored := insert(t, store, ayla)
	load := func(t *testing.T) model.Character {
		t.Helper()
		got, err := store.Load(ctx, ayla.ID, alice)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	t.Run("a duplicate name in different letter case gives ErrNameTaken", func(t *testing.T) {
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			_, err := uow.Characters().Insert(ctx, sample(bob, "AYLA"))
			return err
		})
		if !errors.Is(err, model.ErrNameTaken) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("load by ID and owning account round-trips every column", func(t *testing.T) {
		got := load(t)
		if stored.UpdatedAt.IsZero() || !got.UpdatedAt.Equal(stored.UpdatedAt) {
			t.Fatalf("updated_at %v, want the insert's %v", got.UpdatedAt, stored.UpdatedAt)
		}
		got.UpdatedAt = time.Time{}
		if got != ayla {
			t.Fatalf("got %+v\nwant %+v", got, ayla)
		}
	})

	t.Run("loading another account's character gives ErrNotFound", func(t *testing.T) {
		_, err := store.Load(ctx, ayla.ID, bob)
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("list by account, oldest first", func(t *testing.T) {
		robin := sample(alice, "Robin")
		robin.Class = kernel.Bowman
		insert(t, store, robin)
		insert(t, store, sample(bob, "Bruno"))
		list, err := store.List(ctx, alice)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].Name != "Ayla" || list[1].Name != "Robin" || list[1].Class != kernel.Bowman {
			t.Fatalf("list = %+v", list)
		}
		none, err := store.List(ctx, kernel.NewAccountID())
		if err != nil || len(none) != 0 {
			t.Fatalf("empty list: %v, %v", none, err)
		}
	})

	t.Run("a checkpoint saved, then loaded, matches and moves updated_at", func(t *testing.T) {
		before := load(t)
		cp := model.Checkpoint{ID: ayla.ID, MapID: "forest", X: 300.5, Y: 608, HP: 150, MP: 20, Level: 2, Exp: 12, Stats: gamedata.Stats{STR: 15, DEX: 5, INT: 4, AGI: 5, VIT: 12}}
		if err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Characters().SaveCheckpoint(ctx, cp) }); err != nil {
			t.Fatal(err)
		}
		got := load(t)
		if got.MapID != "forest" || got.X != 300.5 || got.Y != 608 || got.HP != 150 || got.MP != 20 || got.Exp != 12 || !got.UpdatedAt.After(before.UpdatedAt) {
			t.Fatalf("got %+v", got)
		}
		if got.Level != 2 || got.Stats != cp.Stats {
			t.Fatalf("level %d, stats %+v: a checkpoint saves the whole progression", got.Level, got.Stats)
		}
	})

	t.Run("a level-up saved, then loaded, matches and moves updated_at", func(t *testing.T) {
		before := load(t)
		lu := model.LevelUp{ID: ayla.ID, Level: 2, Exp: 3, Stats: gamedata.Stats{STR: 15, DEX: 5, INT: 4, AGI: 5, VIT: 12}, HP: 250, MP: 32}
		if err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Characters().SaveLevelUp(ctx, lu) }); err != nil {
			t.Fatal(err)
		}
		got := load(t)
		if got.Level != 2 || got.Exp != 3 || got.Stats != lu.Stats || got.HP != 250 || got.MP != 32 || !got.UpdatedAt.After(before.UpdatedAt) {
			t.Fatalf("got %+v", got)
		}
		if got.MapID != "forest" {
			t.Fatal("a level-up touched the position")
		}
	})

	t.Run("saves for an unknown character give ErrNotFound", func(t *testing.T) {
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			return uow.Characters().SaveCheckpoint(ctx, model.Checkpoint{ID: kernel.NewCharacterID(), MapID: "town"})
		})
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("checkpoint: %v", err)
		}
		err = store.InTx(ctx, func(uow application.UnitOfWork) error {
			return uow.Characters().SaveLevelUp(ctx, model.LevelUp{ID: kernel.NewCharacterID(), Level: 2})
		})
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("level-up: %v", err)
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
		{"name unique constraint", &pgconn.PgError{Code: "23505", ConstraintName: "characters_name_key"}, model.ErrNameTaken},
		{"another constraint passes through", &pgconn.PgError{Code: "23505", ConstraintName: "accounts_username_key"}, nil},
		{"a deadlock passes through", &pgconn.PgError{Code: "40P01"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.err)
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
