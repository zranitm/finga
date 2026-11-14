package character

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace/noop"

	"finga/internal/facade"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/panics"
	"finga/services/character/internal/application"
	"finga/services/character/internal/model"
)

var (
	owner = kernel.NewAccountID()
	ayla  = model.Character{
		ID: kernel.NewCharacterID(), AccountID: owner, Name: "Ayla", Class: "warrior", Level: 3, Exp: 40, Points: gamedata.Stats{STR: 6, VIT: 3},
		UnspentPoints: 1, FreeResetUsed: true, HP: 150, Energy: 6, MapID: "forest", X: 412.5, Y: 608,
		UpdatedAt: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC), UnspentSkillPoints: 4, SkillBar: []string{"strike", "", "", "", "", ""},
	}
	own = gamedata.Stats{STR: 18, DEX: 5, INT: 4, AGI: 5, VIT: 13}
	end = time.Date(2026, 11, 13, 10, 0, 30, 0, time.UTC)
)

// fakeService records what the facade passed in and answers err when set; derivedErr fails Derived alone.
type fakeService struct {
	in         any
	read       []string // what Load read, in order
	derivedErr error
	err        error
}

func (f *fakeService) Create(_ context.Context, account kernel.AccountID, in application.CreateInput) (model.Character, error) {
	f.in = []any{account, in}
	return ayla, f.err
}

func (f *fakeService) List(_ context.Context, account kernel.AccountID) ([]model.Character, error) {
	f.in = account
	return []model.Character{ayla}, f.err
}

func (f *fakeService) Load(_ context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	f.in, f.read = []any{id, account}, append(f.read, "character")
	return ayla, f.err
}

func (f *fakeService) Quests(_ context.Context, _ kernel.CharacterID) ([]model.Quest, error) {
	f.read = append(f.read, "quests")
	return []model.Quest{{ID: "slime_cleanup", Status: model.QuestActive, Progress: []int{7}}, {ID: "sticky_samples", Status: model.QuestFinished, TimesFinished: 2}}, nil
}

func (f *fakeService) Skills(_ context.Context, _ kernel.CharacterID) ([]model.SkillPoints, error) {
	f.read = append(f.read, "skills")
	return []model.SkillPoints{{ID: "strike", Points: 11}}, nil
}

func (f *fakeService) Timers(_ context.Context, _ kernel.CharacterID) ([]model.SkillTimer, error) {
	f.read = append(f.read, "timers")
	return []model.SkillTimer{{Skill: "iron_body", Kind: model.BuffTimer, Until: end, Points: 3}}, nil
}

func (f *fakeService) Own(model.Character) gamedata.Stats { return own }

func (f *fakeService) Derived(model.Character) (progression.Derived, error) {
	if f.derivedErr != nil {
		return progression.Derived{}, f.derivedErr
	}
	return progression.Derived{MaxHP: 250}, nil
}

func (f *fakeService) SaveCheckpoint(_ context.Context, cp model.Checkpoint) error {
	f.in = cp
	return f.err
}

func (f *fakeService) SaveLevelUp(_ context.Context, lu model.LevelUp) error {
	f.in = lu
	return f.err
}

func (f *fakeService) SetSkillBar(_ context.Context, id kernel.CharacterID, account kernel.AccountID, bar []string) error {
	f.in = []any{id, account, bar}
	return f.err
}

func newModule(t *testing.T, svc service) *Module {
	t.Helper()
	calls, err := facade.New(facade.Options{Module: "character", Traces: noop.NewTracerProvider(), Registry: prometheus.NewRegistry(), Panics: panics.NewRecorder(slog.New(slog.DiscardHandler), nil), Errors: errorTable})
	if err != nil {
		t.Fatal(err)
	}
	return &Module{calls: calls, svc: svc}
}

// wantAyla is ayla as callers see her, with her own stats and maximums.
var wantAyla = Character{
	ID: ayla.ID, AccountID: owner, Name: "Ayla", Class: "warrior", Level: 3, Exp: 40, Points: gamedata.Stats{STR: 6, VIT: 3}, UnspentPoints: 1, FreeResetUsed: true,
	OwnStats: own, UnspentSkillPoints: 4, HP: 150, MaxHP: 250, Energy: 6, MapID: "forest", X: 412.5, Y: 608, UpdatedAt: ayla.UpdatedAt, SkillBar: ayla.SkillBar,
}

func TestEachCallConvertsToAndFromTheModel(t *testing.T) {
	ctx := t.Context()
	t.Run("Create", func(t *testing.T) {
		f := &fakeService{}
		c, err := newModule(t, f).Create(ctx, owner, "Ayla")
		if err != nil || !reflect.DeepEqual(c, wantAyla) || !reflect.DeepEqual(f.in, []any{owner, application.CreateInput{Name: "Ayla"}}) {
			t.Fatalf("got %+v from %+v, err %v", c, f.in, err)
		}
	})
	t.Run("List", func(t *testing.T) {
		f := &fakeService{}
		cs, err := newModule(t, f).List(ctx, owner)
		if err != nil || len(cs) != 1 || !reflect.DeepEqual(cs[0], wantAyla) || f.in != owner {
			t.Fatalf("got %+v, err %v", cs, err)
		}
	})
	t.Run("Load reads the character, then its quests, skills and timers", func(t *testing.T) {
		f := &fakeService{}
		l, err := newModule(t, f).Load(ctx, ayla.ID, owner)
		want := Loaded{
			Character: wantAyla,
			Quests:    []Quest{{ID: "slime_cleanup", Status: QuestActive, Progress: []int{7}}, {ID: "sticky_samples", Status: QuestFinished, TimesFinished: 2}},
			Skills:    []SkillPoints{{ID: "strike", Points: 11}},
			Timers:    []SkillTimer{{Skill: "iron_body", Kind: BuffTimer, Until: end, Points: 3}},
		}
		if err != nil || !reflect.DeepEqual(l, want) || !reflect.DeepEqual(f.in, []any{ayla.ID, owner}) || !slices.Equal(f.read, []string{"character", "quests", "skills", "timers"}) {
			t.Fatalf("got %+v after reading %v, err %v", l, f.read, err)
		}
	})
	t.Run("SaveCheckpoint passes an unknown status or kind on, for the module to refuse", func(t *testing.T) {
		f := &fakeService{}
		cp := Checkpoint{
			ID: ayla.ID, Class: "thief", MapID: "cave", X: 10, Y: 20, HP: 5, Energy: 6, Level: 5, Exp: 7, Points: gamedata.Stats{STR: 9}, UnspentPoints: 1, FreeResetUsed: true,
			Quests:             []Quest{{ID: "slime_cleanup", Status: QuestActive, Progress: []int{3, 0}, TimesFinished: 1}, {ID: "odd"}},
			UnspentSkillPoints: 3, Skills: []SkillPoints{{ID: "spark", Points: 6}},
			Timers: []SkillTimer{{Skill: "haste", Kind: CooldownTimer, Until: end}, {Skill: "haste", Kind: BuffTimer, Until: end, Points: 2}, {Skill: "odd", Until: end}},
		}
		if err := newModule(t, f).SaveCheckpoint(ctx, cp); err != nil {
			t.Fatal(err)
		}
		want := model.Checkpoint{
			ID: ayla.ID, Class: "thief", MapID: "cave", X: 10, Y: 20, HP: 5, Energy: 6, Level: 5, Exp: 7, Points: gamedata.Stats{STR: 9}, UnspentPoints: 1, FreeResetUsed: true,
			Quests:             []model.Quest{{ID: "slime_cleanup", Status: model.QuestActive, Progress: []int{3, 0}, TimesFinished: 1}, {ID: "odd"}},
			UnspentSkillPoints: 3, Skills: []model.SkillPoints{{ID: "spark", Points: 6}},
			Timers: []model.SkillTimer{{Skill: "haste", Kind: model.CooldownTimer, Until: end}, {Skill: "haste", Kind: model.BuffTimer, Until: end, Points: 2}, {Skill: "odd", Until: end}},
		}
		if !reflect.DeepEqual(f.in, want) {
			t.Fatalf("got %+v", f.in)
		}
	})
	t.Run("SaveLevelUp", func(t *testing.T) {
		f := &fakeService{}
		if err := newModule(t, f).SaveLevelUp(ctx, LevelUp{ID: ayla.ID, Level: 4, Exp: 3, Points: gamedata.Stats{STR: 9, VIT: 6}, HP: 280}); err != nil {
			t.Fatal(err)
		}
		if f.in != (model.LevelUp{ID: ayla.ID, Level: 4, Exp: 3, Points: gamedata.Stats{STR: 9, VIT: 6}, HP: 280}) {
			t.Fatalf("got %+v", f.in)
		}
	})
	t.Run("SetSkillBar", func(t *testing.T) {
		f := &fakeService{}
		bar := []string{"", "throw", "", "", "", "strike"}
		if err := newModule(t, f).SetSkillBar(ctx, ayla.ID, owner, bar); err != nil || !reflect.DeepEqual(f.in, []any{ayla.ID, owner, bar}) {
			t.Fatalf("got %+v, err %v", f.in, err)
		}
	})
}

// A class the game data lost is refused at Load, before any quest is read; in a list it only leaves the maximums 0.
func TestAClassTheGameDataLacks(t *testing.T) {
	f := &fakeService{derivedErr: model.ErrClassUnavailable}
	m := newModule(t, f)
	if _, err := m.Load(t.Context(), ayla.ID, owner); !errors.Is(err, kernel.ErrClassUnavailable) || !slices.Equal(f.read, []string{"character"}) {
		t.Fatalf("got %v after reading %v", err, f.read)
	}
	cs, err := m.List(t.Context(), owner)
	if err != nil || len(cs) != 1 || cs[0].MaxHP != 0 || cs[0].OwnStats != own {
		t.Fatalf("got %+v, err %v", cs, err)
	}
}

// Callers find the kernel error with errors.Is, and the logs keep the model's cause.
func TestEveryModelErrorReachesCallersAsItsKernelError(t *testing.T) {
	for _, row := range errorTable {
		t.Run(row.Model.Error(), func(t *testing.T) {
			m := newModule(t, &fakeService{err: fmt.Errorf("saving: %w", row.Model)})
			err := m.SaveLevelUp(t.Context(), LevelUp{ID: ayla.ID})
			if !errors.Is(err, row.Kernel) || !errors.Is(err, row.Model) {
				t.Fatalf("got %v, want %v wrapping %v", err, row.Kernel, row.Model)
			}
		})
	}
	m := newModule(t, &fakeService{err: model.ErrNotFound})
	if _, err := m.Load(t.Context(), ayla.ID, kernel.NewAccountID()); !errors.Is(err, kernel.ErrCharacterNotFound) {
		t.Fatalf("someone else's character: got %v", err)
	}
}
