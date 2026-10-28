package character

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"finga/internal/gamedata"
	"finga/internal/model"
	"finga/pkg/pgtest"
	"finga/services/character/migrations"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, migrations.Up) // the character service's set only
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func sample(account model.AccountID, name string) Character {
	return Character{
		ID: model.NewCharacterID(), AccountID: account, Name: name, Class: model.Warrior, Level: 1, Exp: 0,
		Stats: gamedata.Stats{STR: 12, DEX: 5, INT: 4, AGI: 5, VIT: 10}, HP: 204, MP: 28,
		MapID: "town", X: 416, Y: 608, UpdatedAt: t0,
	}
}

func insert(t *testing.T, store *Postgres, c Character) {
	t.Helper()
	if err := store.InTx(context.Background(), func(uow UnitOfWork) error { return uow.Characters().Insert(context.Background(), c) }); err != nil {
		t.Fatal(err)
	}
}

func TestRepository(t *testing.T) {
	ctx := context.Background()
	store := NewPostgres(cluster.DB(t))
	alice, bob := model.NewAccountID(), model.NewAccountID()
	ayla := sample(alice, "Ayla")
	insert(t, store, ayla)

	t.Run("a duplicate name in different letter case gives ErrNameTaken", func(t *testing.T) {
		err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Characters().Insert(ctx, sample(bob, "AYLA")) })
		if !errors.Is(err, ErrNameTaken) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("load by ID and owning account round-trips every column", func(t *testing.T) {
		got, err := store.Load(ctx, ayla.ID, alice)
		if err != nil {
			t.Fatal(err)
		}
		if !got.UpdatedAt.Equal(ayla.UpdatedAt) {
			t.Fatalf("updated_at %v, want %v", got.UpdatedAt, ayla.UpdatedAt)
		}
		got.UpdatedAt, ayla.UpdatedAt = time.Time{}, time.Time{}
		if got != ayla {
			t.Fatalf("got %+v\nwant %+v", got, ayla)
		}
	})

	t.Run("loading another account's character gives ErrNotFound", func(t *testing.T) {
		_, err := store.Load(ctx, ayla.ID, bob)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("list by account, oldest first", func(t *testing.T) {
		robin := sample(alice, "Robin")
		robin.Class = model.Bowman
		robin.UpdatedAt = t0.Add(time.Minute)
		insert(t, store, robin)
		insert(t, store, sample(bob, "Bruno"))
		list, err := store.List(ctx, alice)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].Name != "Ayla" || list[1].Name != "Robin" || list[1].Class != model.Bowman {
			t.Fatalf("list = %+v", list)
		}
		none, err := store.List(ctx, model.NewAccountID())
		if err != nil || len(none) != 0 {
			t.Fatalf("empty list: %v, %v", none, err)
		}
	})

	t.Run("a checkpoint saved, then loaded, matches", func(t *testing.T) {
		cp := Checkpoint{ID: ayla.ID, MapID: "forest", X: 300.5, Y: 608, HP: 150, MP: 20, Exp: 12, At: t0.Add(time.Hour)}
		if err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Characters().SaveCheckpoint(ctx, cp) }); err != nil {
			t.Fatal(err)
		}
		got, err := store.Load(ctx, ayla.ID, alice)
		if err != nil {
			t.Fatal(err)
		}
		if got.MapID != "forest" || got.X != 300.5 || got.Y != 608 || got.HP != 150 || got.MP != 20 || got.Exp != 12 || !got.UpdatedAt.Equal(cp.At) {
			t.Fatalf("got %+v", got)
		}
		if got.Level != 1 || got.Stats != ayla.Stats {
			t.Fatal("a checkpoint touched level or stats")
		}
	})

	t.Run("a level-up saved, then loaded, matches", func(t *testing.T) {
		lu := LevelUp{ID: ayla.ID, Level: 2, Exp: 3, Stats: gamedata.Stats{STR: 15, DEX: 5, INT: 4, AGI: 5, VIT: 12}, HP: 250, MP: 32, At: t0.Add(2 * time.Hour)}
		if err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Characters().SaveLevelUp(ctx, lu) }); err != nil {
			t.Fatal(err)
		}
		got, err := store.Load(ctx, ayla.ID, alice)
		if err != nil {
			t.Fatal(err)
		}
		if got.Level != 2 || got.Exp != 3 || got.Stats != lu.Stats || got.HP != 250 || got.MP != 32 || !got.UpdatedAt.Equal(lu.At) {
			t.Fatalf("got %+v", got)
		}
		if got.MapID != "forest" {
			t.Fatal("a level-up touched the position")
		}
	})

	t.Run("saves for an unknown character give ErrNotFound", func(t *testing.T) {
		err := store.InTx(ctx, func(uow UnitOfWork) error {
			return uow.Characters().SaveCheckpoint(ctx, Checkpoint{ID: model.NewCharacterID(), MapID: "town", At: t0})
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("checkpoint: %v", err)
		}
		err = store.InTx(ctx, func(uow UnitOfWork) error {
			return uow.Characters().SaveLevelUp(ctx, LevelUp{ID: model.NewCharacterID(), Level: 2, At: t0})
		})
		if !errors.Is(err, ErrNotFound) {
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
		{"no rows", pgx.ErrNoRows, ErrNotFound},
		{"name unique constraint", &pgconn.PgError{Code: "23505", ConstraintName: "characters_name_key"}, ErrNameTaken},
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
