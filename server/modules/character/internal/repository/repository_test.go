package repository

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/modules/character/internal/application"
	"finga/modules/character/internal/model"
	"finga/modules/character/migrations"
	"finga/pkg/pgtest"
	"finga/pkg/transient"
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
		ID: kernel.NewCharacterID(), AccountID: account, Name: name, Class: kernel.Class("warrior"), Level: 1, Exp: 0,
		Points: gamedata.Stats{STR: 1, AGI: 2}, UnspentPoints: 3, FreeResetUsed: true, HP: 184,
		MapID: "town", X: 416, Y: 608, SkillBar: []string{"", "", "", "", "", ""},
	}
}

// insert returns the character as stored, with its updated_at.
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
	pool := cluster.DB(t)
	store := NewPostgres(pool, nil)
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
		if !reflect.DeepEqual(got, ayla) {
			t.Fatalf("got %+v\nwant %+v", got, ayla)
		}
	})

	t.Run("the skill bar is set only for its owner", func(t *testing.T) {
		bar := []string{"strike", "", "", "", "", "spark"}
		set := func(account kernel.AccountID, bar []string) error {
			return store.InTx(ctx, func(uow application.UnitOfWork) error {
				return uow.Characters().SetSkillBar(ctx, ayla.ID, account, bar)
			})
		}
		if err := set(bob, bar); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("someone else's: %v, want ErrNotFound", err)
		}
		if err := set(alice, bar); err != nil {
			t.Fatal(err)
		}
		got := load(t)
		if !slices.Equal(got.SkillBar, bar) || !got.UpdatedAt.After(stored.UpdatedAt) {
			t.Fatalf("skill bar %q at %v, want %q stamped after %v", got.SkillBar, got.UpdatedAt, bar, stored.UpdatedAt)
		}
		if err := set(alice, ayla.SkillBar); err != nil {
			t.Fatal(err)
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
		robin.Class = kernel.Class("bowman")
		insert(t, store, robin)
		insert(t, store, sample(bob, "Bruno"))
		list, err := store.List(ctx, alice)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].Name != "Ayla" || list[1].Name != "Robin" || list[1].Class != kernel.Class("bowman") {
			t.Fatalf("list = %+v", list)
		}
		none, err := store.List(ctx, kernel.NewAccountID())
		if err != nil || len(none) != 0 {
			t.Fatalf("empty list: %v, %v", none, err)
		}
	})

	t.Run("a checkpoint saved, then loaded, matches and moves updated_at", func(t *testing.T) {
		before := load(t)
		cp := model.Checkpoint{ID: ayla.ID, Class: "warrior", MapID: "forest", X: 300.5, Y: 608, HP: 150, Energy: 6, Level: 2, Exp: 12, Points: gamedata.Stats{STR: 3, DEX: 1, INT: 2, AGI: 4, VIT: 5}, UnspentPoints: 0, FreeResetUsed: false}
		if err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Characters().SaveCheckpoint(ctx, cp) }); err != nil {
			t.Fatal(err)
		}
		got := load(t)
		if got.Class != "warrior" || got.MapID != "forest" || got.X != 300.5 || got.Y != 608 || got.HP != 150 || got.Energy != 6 || got.Exp != 12 || !got.UpdatedAt.After(before.UpdatedAt) {
			t.Fatalf("got %+v", got)
		}
		if got.Level != 2 || got.Points != cp.Points || got.UnspentPoints != 0 || got.FreeResetUsed {
			t.Fatalf("level %d, points %+v, unspent %d, reset used %v: a checkpoint saves the whole progression", got.Level, got.Points, got.UnspentPoints, got.FreeResetUsed)
		}
	})

	t.Run("a checkpoint's quest log replaces the stored one, writing only the rows that changed", func(t *testing.T) {
		save := func(t *testing.T, quests ...model.Quest) {
			t.Helper()
			cp := model.Checkpoint{ID: ayla.ID, MapID: "forest", Level: 2, Quests: quests}
			if err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Characters().SaveCheckpoint(ctx, cp) }); err != nil {
				t.Fatal(err)
			}
		}
		quests := func(t *testing.T) []model.Quest {
			t.Helper()
			got, err := store.Quests(ctx, ayla.ID)
			if err != nil {
				t.Fatal(err)
			}
			return got
		}
		stamp := func(t *testing.T, quest string) time.Time {
			t.Helper()
			var at time.Time
			if err := pool.QueryRow(ctx, `SELECT updated_at FROM character_quests WHERE character_id = $1 AND quest_id = $2`, ayla.ID, quest).Scan(&at); err != nil {
				t.Fatal(err)
			}
			return at
		}
		if got := quests(t); len(got) != 0 {
			t.Fatalf("a new character has quests %+v", got)
		}
		cleanup := model.Quest{ID: "slime_cleanup", Status: model.QuestActive, Progress: []int{3, 0}}
		samples := model.Quest{ID: "sticky_samples", Status: model.QuestFinished, Progress: []int{}, TimesFinished: 1}
		save(t, cleanup, samples)
		if got := quests(t); !reflect.DeepEqual(got, []model.Quest{cleanup, samples}) {
			t.Fatalf("quests %+v", got)
		}
		finished := stamp(t, "sticky_samples")
		cleanup.Progress = []int{10, 1}
		save(t, cleanup, samples)
		if got := quests(t); !reflect.DeepEqual(got, []model.Quest{cleanup, samples}) {
			t.Fatalf("quests %+v", got)
		}
		if !stamp(t, "sticky_samples").Equal(finished) {
			t.Fatal("a row saved as it was is written again")
		}
		// A repeatable quest active again keeps its count; one left out, abandoned before it ever finished, goes.
		samples = model.Quest{ID: "sticky_samples", Status: model.QuestActive, Progress: []int{2}, TimesFinished: 1}
		save(t, samples)
		if got := quests(t); !reflect.DeepEqual(got, []model.Quest{samples}) {
			t.Fatalf("quests %+v", got)
		}
		save(t)
		if got := quests(t); len(got) != 0 {
			t.Fatalf("quests %+v after an empty log", got)
		}
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			return uow.Characters().SaveCheckpoint(ctx, model.Checkpoint{ID: ayla.ID, MapID: "forest", Level: 2, Quests: []model.Quest{{ID: "q", Status: model.QuestFinished, Progress: []int{}}}})
		})
		if err == nil {
			t.Fatal("the table took a quest finished no times")
		}
	})

	t.Run("a checkpoint's skills are saved and kept, writing only the rows that changed", func(t *testing.T) {
		save := func(t *testing.T, unspent int, skills ...model.SkillPoints) {
			t.Helper()
			cp := model.Checkpoint{ID: ayla.ID, MapID: "forest", Level: 3, UnspentSkillPoints: unspent, Skills: skills}
			if err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Characters().SaveCheckpoint(ctx, cp) }); err != nil {
				t.Fatal(err)
			}
		}
		skills := func(t *testing.T) []model.SkillPoints {
			t.Helper()
			got, err := store.Skills(ctx, ayla.ID)
			if err != nil {
				t.Fatal(err)
			}
			return got
		}
		stamp := func(t *testing.T, skill string) time.Time {
			t.Helper()
			var at time.Time
			if err := pool.QueryRow(ctx, `SELECT updated_at FROM character_skills WHERE character_id = $1 AND skill_id = $2`, ayla.ID, skill).Scan(&at); err != nil {
				t.Fatal(err)
			}
			return at
		}
		if got := skills(t); len(got) != 0 {
			t.Fatalf("a new character has skills %+v", got)
		}
		save(t, 3, model.SkillPoints{ID: "strike", Points: 4}, model.SkillPoints{ID: "second_wind", Points: 3})
		if got := skills(t); !reflect.DeepEqual(got, []model.SkillPoints{{ID: "second_wind", Points: 3}, {ID: "strike", Points: 4}}) {
			t.Fatalf("skills %+v", got)
		}
		if c := load(t); c.UnspentSkillPoints != 3 {
			t.Fatalf("unspent skill points %d, want 3", c.UnspentSkillPoints)
		}
		wind := stamp(t, "second_wind")
		save(t, 0, model.SkillPoints{ID: "strike", Points: 7}, model.SkillPoints{ID: "second_wind", Points: 3})
		if !stamp(t, "second_wind").Equal(wind) {
			t.Fatal("a row saved as it was is written again")
		}
		save(t, 0, model.SkillPoints{ID: "throw", Points: 1})
		if got := skills(t); !reflect.DeepEqual(got, []model.SkillPoints{{ID: "second_wind", Points: 3}, {ID: "strike", Points: 7}, {ID: "throw", Points: 1}}) {
			t.Fatalf("skills %+v; points are never taken back, so the ones left out stay", got)
		}
		for _, bad := range []model.Checkpoint{
			{ID: ayla.ID, MapID: "forest", Level: 3, Skills: []model.SkillPoints{{ID: "throw", Points: 0}}},
			{ID: ayla.ID, MapID: "forest", Level: 3, UnspentSkillPoints: -1},
		} {
			if err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Characters().SaveCheckpoint(ctx, bad) }); err == nil {
				t.Fatalf("the table took %+v", bad)
			}
		}
	})

	t.Run("a checkpoint's skill timers replace the stored ones, writing only the rows that changed", func(t *testing.T) {
		end := time.Date(2026, 11, 12, 10, 0, 30, 0, time.UTC)
		save := func(t *testing.T, timers ...model.SkillTimer) error {
			t.Helper()
			cp := model.Checkpoint{ID: ayla.ID, MapID: "forest", Level: 2, Timers: timers}
			return store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Characters().SaveCheckpoint(ctx, cp) })
		}
		both := []model.SkillTimer{
			{Skill: "iron_body", Kind: model.BuffTimer, Until: end, Points: 3},
			{Skill: "iron_body", Kind: model.CooldownTimer, Until: end.Add(30 * time.Second)},
		}
		if err := save(t, both...); err != nil {
			t.Fatal(err)
		}
		got, err := store.Timers(ctx, ayla.ID)
		if err != nil || len(got) != 2 || !got[0].Until.Equal(end) || got[0] != (model.SkillTimer{Skill: "iron_body", Kind: model.BuffTimer, Until: got[0].Until, Points: 3}) || got[1].Kind != model.CooldownTimer {
			t.Fatalf("timers %+v (%v), want the buff then the cooldown", got, err)
		}
		stamp := func(t *testing.T, kind model.TimerKind) time.Time {
			t.Helper()
			var at time.Time
			if err := pool.QueryRow(ctx, `SELECT updated_at FROM character_skill_timers WHERE character_id = $1 AND skill_id = 'iron_body' AND kind = $2`, ayla.ID, string(kind)).Scan(&at); err != nil {
				t.Fatal(err)
			}
			return at
		}
		buffAt, cooldownAt := stamp(t, model.BuffTimer), stamp(t, model.CooldownTimer)
		both[1].Until = both[1].Until.Add(time.Second)
		if err := save(t, both...); err != nil {
			t.Fatal(err)
		}
		if !stamp(t, model.BuffTimer).Equal(buffAt) || !stamp(t, model.CooldownTimer).After(cooldownAt) {
			t.Fatal("want the moved cooldown written again and the unchanged buff left alone")
		}
		if err := save(t); err != nil {
			t.Fatal(err)
		}
		if got, err := store.Timers(ctx, ayla.ID); err != nil || len(got) != 0 {
			t.Fatalf("timers %+v (%v) after a checkpoint with none", got, err)
		}
		for _, bad := range [][]model.SkillTimer{
			{{Skill: "haste", Kind: "charge", Until: end}},
			{{Skill: "haste", Kind: model.BuffTimer, Until: end}},
			{{Skill: "haste", Kind: model.CooldownTimer, Until: end}, {Skill: "haste", Kind: model.CooldownTimer, Until: end}},
		} {
			if err := save(t, bad...); err == nil {
				t.Fatalf("the table took %+v", bad)
			}
		}
	})

	t.Run("a level-up saved, then loaded, matches and moves updated_at", func(t *testing.T) {
		before := load(t)
		lu := model.LevelUp{ID: ayla.ID, Level: 2, Exp: 3, Points: gamedata.Stats{STR: 6, VIT: 4}, HP: 250}
		if err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Characters().SaveLevelUp(ctx, lu) }); err != nil {
			t.Fatal(err)
		}
		got := load(t)
		if got.Level != 2 || got.Exp != 3 || got.Points != lu.Points || got.HP != 250 || !got.UpdatedAt.After(before.UpdatedAt) {
			t.Fatalf("got %+v", got)
		}
		if got.MapID != "forest" {
			t.Fatal("a level-up touched the position")
		}
	})

	t.Run("a save leaves the list oldest first", func(t *testing.T) {
		list, err := store.List(ctx, alice)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].Name != "Ayla" || list[1].Name != "Robin" {
			t.Fatalf("after Ayla's saves the list is %+v, want Ayla then Robin", list)
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
		{"a deadlock past the retries", &pgconn.PgError{Code: "40P01"}, transient.ErrUnavailable},
		{"the server shutting down", &pgconn.PgError{Code: "57P01"}, transient.ErrUnavailable},
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
