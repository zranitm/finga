package local_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/transient"
	"finga/services/character"
	"finga/services/world/internal/local"
	"finga/services/world/internal/model"
)

type characterFake struct {
	saved    []character.Checkpoint
	loadedID kernel.CharacterID
	loadedAc kernel.AccountID
	loaded   character.Loaded
	err      error
}

func (f *characterFake) Load(_ context.Context, id kernel.CharacterID, account kernel.AccountID) (character.Loaded, error) {
	f.loadedID, f.loadedAc = id, account
	return f.loaded, f.err
}

func (f *characterFake) SaveCheckpoint(_ context.Context, cp character.Checkpoint) error {
	f.saved = append(f.saved, cp)
	return f.err
}

func TestSaverSendsEveryFieldOfTheCheckpointInOrder(t *testing.T) {
	f := &characterFake{}
	id := kernel.NewCharacterID()
	end := time.Date(2026, 11, 12, 10, 0, 30, 0, time.UTC)
	cp := model.Checkpoint{
		ID: id, Class: kernel.Class("bowman"), MapID: "cave", X: 1.5, Y: 2, HP: 3, Energy: 4, Level: 5, Exp: 6,
		Points: model.Points{Spent: gamedata.Stats{STR: 9, VIT: 4}, Unspent: 2, FreeResetUsed: true},
		Quests: model.Quests{
			"sticky_samples": {Status: model.QuestActive, Progress: []int{3}, TimesFinished: 1},
			"slime_cleanup":  {Status: model.QuestFinished, TimesFinished: 2},
		},
		Skills: model.Skills{Points: map[string]int{"throw": 7, "second_wind": 2}, Unspent: 3},
		Timers: model.SkillTimers{
			Cooldowns: map[string]time.Time{"iron_body": end.Add(30 * time.Second), "focus": end},
			Buffs:     map[string]model.BuffTimer{"iron_body": {Until: end, Points: 3}, "anger": {Until: end, Points: 1}},
		},
	}
	if err := local.NewSaver(f).SaveCheckpoint(t.Context(), cp); err != nil {
		t.Fatal(err)
	}
	want := character.Checkpoint{
		ID: id, Class: kernel.Class("bowman"), MapID: "cave", X: 1.5, Y: 2, HP: 3, Energy: 4, Level: 5, Exp: 6,
		Points: gamedata.Stats{STR: 9, VIT: 4}, UnspentPoints: 2, FreeResetUsed: true,
		Quests: []character.Quest{
			{ID: "slime_cleanup", Status: character.QuestFinished, TimesFinished: 2},
			{ID: "sticky_samples", Status: character.QuestActive, Progress: []int{3}, TimesFinished: 1},
		},
		UnspentSkillPoints: 3,
		Skills:             []character.SkillPoints{{ID: "second_wind", Points: 2}, {ID: "throw", Points: 7}},
		Timers: []character.SkillTimer{
			{Skill: "focus", Kind: character.CooldownTimer, Until: end},
			{Skill: "iron_body", Kind: character.CooldownTimer, Until: end.Add(30 * time.Second)},
			{Skill: "anger", Kind: character.BuffTimer, Until: end, Points: 1},
			{Skill: "iron_body", Kind: character.BuffTimer, Until: end, Points: 3},
		},
	}
	if len(f.saved) != 1 || !reflect.DeepEqual(f.saved[0], want) {
		t.Fatalf("saved %+v\nwant  %+v", f.saved, want)
	}
	if f.saved[0].Quests[0].Progress != nil {
		t.Fatal("a finished quest's empty progress is not nil")
	}
}

func TestSaverSendsAnEmptyCharacterAsNothingListed(t *testing.T) {
	f := &characterFake{}
	if err := local.NewSaver(f).SaveCheckpoint(t.Context(), model.Checkpoint{}); err != nil {
		t.Fatal(err)
	}
	cp := f.saved[0]
	if len(cp.Quests) != 0 || len(cp.Skills) != 0 || len(cp.Timers) != 0 {
		t.Fatalf("saved %+v", cp)
	}
}

func TestSaverPassesTheModulesFailureOn(t *testing.T) {
	cause := errors.New("the module's cause")
	f := &characterFake{err: cause}
	if err := local.NewSaver(f).SaveCheckpoint(t.Context(), model.Checkpoint{}); !errors.Is(err, cause) {
		t.Fatalf("got %v", err)
	}
}

func TestCharactersLoadAsTheWorldJoinsThem(t *testing.T) {
	id, account := kernel.NewCharacterID(), kernel.NewAccountID()
	until := time.Unix(1_800_000_000, 0).UTC()
	f := &characterFake{loaded: character.Loaded{
		Character: character.Character{
			ID: id, Name: "Ayla", Class: kernel.Class("bowman"), Level: 3, Exp: 40,
			Points: gamedata.Stats{DEX: 6, AGI: 4}, UnspentPoints: 2, FreeResetUsed: true, UnspentSkillPoints: 3,
			HP: 90, Energy: 3, MapID: "cave", X: 12.5, Y: 600,
		},
		Skills: []character.SkillPoints{{ID: "throw", Points: 7}, {ID: "second_wind", Points: 2}},
		Quests: []character.Quest{
			{ID: "slime_cleanup", Status: character.QuestActive, Progress: []int{7}},
			{ID: "first_steps", Status: character.QuestFinished, TimesFinished: 2},
		},
		Timers: []character.SkillTimer{
			{Skill: "focus", Kind: character.CooldownTimer, Until: until},
			{Skill: "focus", Kind: character.BuffTimer, Until: until.Add(-10 * time.Second), Points: 2},
			{Skill: "odd", Kind: character.TimerKind("odd"), Until: until},
		},
	}}
	got, err := local.NewCharacters(f).Load(t.Context(), id, account)
	if err != nil {
		t.Fatal(err)
	}
	want := model.Character{
		ID: id, Account: account, Name: "Ayla", Class: kernel.Class("bowman"), Level: 3, Exp: 40,
		Points: model.Points{Spent: gamedata.Stats{DEX: 6, AGI: 4}, Unspent: 2, FreeResetUsed: true},
		Skills: model.Skills{Points: map[string]int{"throw": 7, "second_wind": 2}, Unspent: 3},
		HP:     90, Energy: 3, MapID: "cave", X: 12.5, Y: 600,
		Quests: model.Quests{"slime_cleanup": {Status: model.QuestActive, Progress: []int{7}}, "first_steps": {Status: model.QuestFinished, TimesFinished: 2}},
		Timers: model.SkillTimers{ // a kind this build doesn't know is left out
			Cooldowns: map[string]time.Time{"focus": until},
			Buffs:     map[string]model.BuffTimer{"focus": {Until: until.Add(-10 * time.Second), Points: 2}},
		},
	}
	if !reflect.DeepEqual(got, want) || f.loadedID != id || f.loadedAc != account {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestCharactersFailWhatTheyCannotJoin(t *testing.T) {
	id, account := kernel.NewCharacterID(), kernel.NewAccountID()
	cause := transient.ErrUnavailable
	tests := []struct {
		name   string
		loaded character.Loaded
		err    error
	}{
		{"a quest status this build doesn't know", character.Loaded{Quests: []character.Quest{{ID: "q", Status: character.QuestStatus("odd")}}}, nil},
		{"the module's failure", character.Loaded{}, cause},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := local.NewCharacters(&characterFake{loaded: tt.loaded, err: tt.err}).Load(t.Context(), id, account)
			if err == nil || (tt.err != nil && !errors.Is(err, tt.err)) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
