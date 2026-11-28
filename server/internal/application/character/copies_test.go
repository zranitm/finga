package character

import (
	"context"
	model "finga/internal/model/character"
	"finga/internal/shared/kernel"
	"finga/pkg/copytest"
	"testing"
)

// stored is a character and its rows as the module holds them.
type stored struct {
	Character model.Character
	Quests    []model.Quest
	Skills    []model.SkillPoints
	Timers    []model.SkillTimer
	Waypoints []model.Waypoint
}

// filledCases answers with a filled character and its rows; the own stats and maximums stay the fake's.
type filledCases struct {
	fakeCases
	rows stored
}

func (f *filledCases) Create(_ context.Context, account kernel.AccountID, in CreateInput) (model.Character, error) {
	f.in = []any{account, in}
	return f.rows.Character, nil
}

func (f *filledCases) List(context.Context, kernel.AccountID) ([]model.Character, error) {
	return []model.Character{f.rows.Character}, nil
}

func (f *filledCases) Load(context.Context, kernel.CharacterID, kernel.AccountID) (model.Character, error) {
	return f.rows.Character, nil
}

func (f *filledCases) Quests(context.Context, kernel.CharacterID) ([]model.Quest, error) {
	return f.rows.Quests, nil
}

func (f *filledCases) Skills(context.Context, kernel.CharacterID) ([]model.SkillPoints, error) {
	return f.rows.Skills, nil
}

func (f *filledCases) Timers(context.Context, kernel.CharacterID) ([]model.SkillTimer, error) {
	return f.rows.Timers, nil
}

func (f *filledCases) Waypoints(context.Context, kernel.CharacterID) ([]model.Waypoint, error) {
	return f.rows.Waypoints, nil
}

// worked out are the character's fields the use cases derives rather than stores, below prefix.
func workedOut(prefix string) map[string]string {
	out := map[string]string{prefix + "MaxHP": "derived from the class, level and stats by the service"}
	for _, stat := range []string{"STR", "DEX", "INT", "AGI", "VIT"} {
		out[prefix+"OwnStats."+stat] = "the class's starting stats plus the points, worked out by the service"
	}
	return out
}

// Each conversion between the facade and the module carries every field, so one added on both sides and missed in between shows.
func TestEveryConversionKeepsEveryField(t *testing.T) {
	ctx := t.Context()
	f := &filledCases{}
	m := newModule(t, f)
	for name, diff := range map[string]func() []string{
		"Create's request": func() []string {
			type asked struct {
				Account kernel.AccountID
				Name    string
			}
			type sent struct {
				Account kernel.AccountID
				In      CreateInput
			}
			return copytest.Diff(copytest.Match{Renames: map[string]string{"Name": "In.Name"}}, func(a asked) sent {
				if _, err := m.Create(ctx, a.Account, a.Name); err != nil {
					t.Fatal(err)
				}
				in := f.in.([]any)
				return sent{in[0].(kernel.AccountID), in[1].(CreateInput)}
			})
		},
		"Create's character": func() []string {
			return copytest.Diff(copytest.Match{Computed: workedOut("")}, func(c model.Character) Character {
				f.rows.Character = c
				out, err := m.Create(ctx, c.AccountID, c.Name)
				if err != nil {
					t.Fatal(err)
				}
				return out
			})
		},
		"List": func() []string {
			return copytest.Diff(copytest.Match{Computed: workedOut("")}, func(c model.Character) []Character {
				f.rows.Character = c
				out, err := m.List(ctx, c.AccountID)
				if err != nil {
					t.Fatal(err)
				}
				return out
			})
		},
		"Load": func() []string {
			return copytest.Diff(copytest.Match{Computed: workedOut("Character.")}, func(rows stored) Loaded {
				f.rows = rows
				out, err := m.Load(ctx, rows.Character.ID, rows.Character.AccountID)
				if err != nil {
					t.Fatal(err)
				}
				return out
			})
		},
		"SaveCheckpoint": func() []string {
			return copytest.Diff(copytest.Match{}, func(cp Checkpoint) model.Checkpoint {
				if err := m.SaveCheckpoint(ctx, cp); err != nil {
					t.Fatal(err)
				}
				return f.in.(model.Checkpoint)
			})
		},
		"FindWaypoints": func() []string {
			type asked struct {
				ID    kernel.CharacterID
				Found []Waypoint
			}
			type sent struct {
				ID    kernel.CharacterID
				Found []model.Waypoint
			}
			return copytest.Diff(copytest.Match{}, func(a asked) sent {
				if err := m.FindWaypoints(ctx, a.ID, a.Found); err != nil {
					t.Fatal(err)
				}
				in := f.in.([]any)
				return sent{in[0].(kernel.CharacterID), in[1].([]model.Waypoint)}
			})
		},
	} {
		if got := diff(); len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}
