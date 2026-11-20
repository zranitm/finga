package world_test

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"testing"
	"time"

	adapter "finga/internal/adapter/world"
	"finga/internal/application/character"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/pkg/copytest"
	"finga/pkg/transient"
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
		Waypoints: model.Waypoints{"woods": end, "town": end.Add(-time.Hour)},
	}
	if err := adapter.NewSaver(f).SaveCheckpoint(t.Context(), cp); err != nil {
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
		Waypoints: []character.Waypoint{{ID: "town", FoundAt: end.Add(-time.Hour)}, {ID: "woods", FoundAt: end}},
	}
	if len(f.saved) != 1 || !reflect.DeepEqual(f.saved[0], want) {
		t.Fatalf("saved %+v\nwant  %+v", f.saved, want)
	}
	if f.saved[0].Quests[0].Progress != nil {
		t.Fatal("a finished quest's empty progress is not nil")
	}
}

// characterEnums fill the quest statuses and timer kinds, which their own tests map.
var characterEnums = map[reflect.Type]any{
	reflect.TypeFor[model.QuestStatus]():     model.QuestActive,
	reflect.TypeFor[character.QuestStatus](): character.QuestActive,
	reflect.TypeFor[character.TimerKind]():   character.CooldownTimer,
}

// shieldKept and giverKept are why a shield's pool and a buff's giver aren't saved: like an action under way, they're seconds of combat that a map change carries and a checkpoint doesn't.
const (
	shieldKept = "a shield's pool lasts seconds: a map change carries it, a checkpoint keeps none"
	giverKept  = "a buff's giver names it for seconds: a map change carries it, a checkpoint keeps none, and a relog shows the buff unattributed"
)

// A checkpoint reaches the character module with every field: the points and skills flattened, the log, skills and timers listed.
func TestSaverKeepsEveryFieldOfTheCheckpoint(t *testing.T) {
	f := &characterFake{}
	match := copytest.Match{
		Renames: map[string]string{
			"Points.Spent": "Points", "Points.Unspent": "UnspentPoints", "Points.FreeResetUsed": "FreeResetUsed",
			"Skills.Unspent": "UnspentSkillPoints", "Skills.Points.key": "Skills.ID", "Skills.Points.value": "Skills.Points",
			"Quests.key": "Quests.ID", "Quests.value": "Quests",
			"Timers.Cooldowns.key": "Timers.Skill", "Timers.Cooldowns.value": "Timers.Until",
			"Timers.Buffs.key": "Timers.Skill", "Timers.Buffs.value": "Timers",
			"Waypoints.key": "Waypoints.ID", "Waypoints.value": "Waypoints.FoundAt",
		},
		Skip:     map[string]string{"Timers.Buffs.value.Shield": shieldKept, "Timers.Buffs.value.From": giverKept, "Skills.Traits": traitByPoints},
		Computed: map[string]string{"Skills.Trait": traitByPoints},
		Enums:    characterEnums,
	}
	got := copytest.Diff(match, func(cp model.Checkpoint) character.Checkpoint {
		f.saved = nil
		if err := adapter.NewSaver(f).SaveCheckpoint(t.Context(), cp); err != nil {
			t.Fatal(err)
		}
		return f.saved[0]
	})
	if len(got) > 0 {
		t.Fatal(got)
	}
}

// traitByPoints is why the generic checks leave a skill's trait to TestSaverSavesEachSkillsTraitWithItsPoints: it rides its skill's row, so it shares its points' key.
const traitByPoints = "a skill's trait is saved on its points' row"

// C20 §4: a skill's trait is saved beside its points, the default as ""; a skill needs a point to have a row, so a trait without one isn't saved.
func TestSaverSavesEachSkillsTraitWithItsPoints(t *testing.T) {
	f := &characterFake{}
	cp := model.Checkpoint{Skills: model.Skills{Points: map[string]int{"strike": 25, "throw": 3}, Traits: map[string]string{"strike": "concussion", "spark": "static"}}}
	if err := adapter.NewSaver(f).SaveCheckpoint(t.Context(), cp); err != nil {
		t.Fatal(err)
	}
	want := []character.SkillPoints{{ID: "strike", Points: 25, Trait: "concussion"}, {ID: "throw", Points: 3}}
	if got := f.saved[0].Skills; !reflect.DeepEqual(got, want) {
		t.Fatalf("skills %+v, want %+v", got, want)
	}
}

func TestSaverSendsAnEmptyCharacterAsNothingListed(t *testing.T) {
	f := &characterFake{}
	if err := adapter.NewSaver(f).SaveCheckpoint(t.Context(), model.Checkpoint{}); err != nil {
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
	if err := adapter.NewSaver(f).SaveCheckpoint(t.Context(), model.Checkpoint{}); !errors.Is(err, cause) {
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
		Skills: []character.SkillPoints{{ID: "throw", Points: 7, Trait: "bola"}, {ID: "second_wind", Points: 2}},
		Quests: []character.Quest{
			{ID: "slime_cleanup", Status: character.QuestActive, Progress: []int{7}},
			{ID: "first_steps", Status: character.QuestFinished, TimesFinished: 2},
		},
		Timers: []character.SkillTimer{
			{Skill: "focus", Kind: character.CooldownTimer, Until: until},
			{Skill: "focus", Kind: character.BuffTimer, Until: until.Add(-10 * time.Second), Points: 2, Trait: "expose"},
			{Skill: "odd", Kind: character.TimerKind("odd"), Until: until},
		},
		Waypoints: []character.Waypoint{{ID: "town", FoundAt: until}},
	}}
	got, err := adapter.NewCharacters(f).Load(t.Context(), id, account)
	if err != nil {
		t.Fatal(err)
	}
	want := model.Character{
		ID: id, Account: account, Name: "Ayla", Class: kernel.Class("bowman"), Level: 3, Exp: 40,
		Points: model.Points{Spent: gamedata.Stats{DEX: 6, AGI: 4}, Unspent: 2, FreeResetUsed: true},
		Skills: model.Skills{Points: map[string]int{"throw": 7, "second_wind": 2}, Traits: map[string]string{"throw": "bola"}, Unspent: 3}, // a default isn't listed
		HP:     90, Energy: 3, MapID: "cave", X: 12.5, Y: 600,
		Quests: model.Quests{"slime_cleanup": {Status: model.QuestActive, Progress: []int{7}}, "first_steps": {Status: model.QuestFinished, TimesFinished: 2}},
		Timers: model.SkillTimers{ // a kind this build doesn't know is left out
			Cooldowns: map[string]time.Time{"focus": until},
			Buffs:     map[string]model.BuffTimer{"focus": {Until: until.Add(-10 * time.Second), Points: 2, Trait: "expose"}},
		},
		Waypoints: model.Waypoints{"town": until},
	}
	if !reflect.DeepEqual(got, want) || f.loadedID != id || f.loadedAc != account {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// loadCall is what Load is asked for and what the character module answers.
type loadCall struct {
	ID      kernel.CharacterID
	Account kernel.AccountID
	Loaded  character.Loaded
}

// A loaded character reaches the world with every field it keeps: the points and skills nested, the log and timers by ID.
func TestCharactersLoadEveryFieldTheWorldKeeps(t *testing.T) {
	f := &characterFake{}
	for kind, timers := range map[character.TimerKind]map[string]string{
		character.CooldownTimer: {"Loaded.Timers.Skill": "Timers.Cooldowns.key", "Loaded.Timers.Until": "Timers.Cooldowns.value"},
		character.BuffTimer:     {"Loaded.Timers.Skill": "Timers.Buffs.key", "Loaded.Timers": "Timers.Buffs.value"},
	} {
		match := copytest.Match{
			Renames: map[string]string{
				"Loaded.Character": "", "Loaded.Character.Points": "Points.Spent", "Loaded.Character.UnspentPoints": "Points.Unspent",
				"Loaded.Character.FreeResetUsed": "Points.FreeResetUsed", "Loaded.Character.UnspentSkillPoints": "Skills.Unspent",
				"Loaded.Skills.ID": "Skills.Points.key", "Loaded.Skills.Points": "Skills.Points.value", "Loaded.Skills.Trait": "Skills.Traits.value",
				"Loaded.Quests.ID": "Quests.key", "Loaded.Quests": "Quests.value",
				"Loaded.Waypoints.ID": "Waypoints.key", "Loaded.Waypoints.FoundAt": "Waypoints.value",
			},
			Skip: map[string]string{
				"Loaded.Character.ID":        "the character asked for is the one loaded",
				"Loaded.Character.AccountID": "the account asked for owns it",
				"Loaded.Character.OwnStats":  "the world derives the stats from the points",
				"Loaded.Character.MaxHP":     "the world derives it from the stats",
				"Loaded.Character.UpdatedAt": "the world keeps no save times",
				"Loaded.Character.SkillBar":  "the skill bar reaches the client with the character list, not through the world",
			},
			Computed: map[string]string{"Timers.Buffs.value.Shield": shieldKept, "Timers.Buffs.value.From": giverKept, "Skills.Traits.key": "the skill's ID, as Skills.Points.key"},
			Enums:    maps.Clone(characterEnums),
		}
		maps.Copy(match.Renames, timers)
		match.Enums[reflect.TypeFor[character.TimerKind]()] = kind
		if kind == character.CooldownTimer {
			match.Skip["Loaded.Timers.Points"] = "a cooldown has no points"
			match.Skip["Loaded.Timers.Trait"] = "a cooldown has no trait"
		}
		got := copytest.Diff(match, func(c loadCall) model.Character {
			f.loaded = c.Loaded
			out, err := adapter.NewCharacters(f).Load(t.Context(), c.ID, c.Account)
			if err != nil {
				t.Fatal(err)
			}
			return out
		})
		if len(got) > 0 {
			t.Errorf("%s timers: %v", kind, got)
		}
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
			_, err := adapter.NewCharacters(&characterFake{loaded: tt.loaded, err: tt.err}).Load(t.Context(), id, account)
			if err == nil || (tt.err != nil && !errors.Is(err, tt.err)) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
