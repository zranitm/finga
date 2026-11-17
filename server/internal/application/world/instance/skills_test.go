package instance_test

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/pkg/transient"
)

// A warrior learns its own skills and the Beginner's; 10 points evolve Second Wind into Deep Breath, whose regen joins the sheet as the skills' share.
func TestSpendingSkillPoints(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cps := &worldtest.Checkpoints{}
		inst, stop := worldtest.Run(t, data, cps, worldtest.Options{CheckpointInterval: time.Hour})
		t.Cleanup(stop)
		x, z := worldtest.TownSpawn(t, data)
		p := worldtest.NewPlayer("Ayla", x, z)
		p.Level, p.Connection, p.HP, p.Skills = 3, "c1", 100, model.Skills{Unspent: 15}
		s := join(t, inst, p)
		cps.Drain()
		spend := func(conn string, expected int, add map[string]int) error {
			err := inst.SpendSkillPoints(context.Background(), p.ID, conn, expected, add)
			tick(1)
			return err
		}
		for _, c := range []struct {
			name     string
			expected int
			add      map[string]int
			want     error
		}{
			{"a negative count", 15, map[string]int{"strike": -1, "throw": 2}, model.ErrBadSkillPoints},
			{"no points at all", 15, map[string]int{"strike": 0}, model.ErrBadSkillPoints},
			{"a skill no class teaches", 15, map[string]int{"fireball": 1}, model.ErrSkillNotLearnable},
			{"its granted break-free, which takes no points", 15, map[string]int{"unyielding": 1}, model.ErrSkillNotLearnable},
			{"a count already changed", 14, map[string]int{"strike": 1}, model.ErrSkillPointsChanged},
			{"more than unspent", 15, map[string]int{"strike": 10, "spark": 6}, model.ErrNotEnoughSkillPoints},
		} {
			if err := spend("c1", c.expected, c.add); !errors.Is(err, c.want) {
				t.Fatalf("%s: %v, want %v", c.name, err, c.want)
			}
		}
		if got := eventsOf[model.StatsChanged](s); len(got) != 0 || len(cps.Drain()) != 0 {
			t.Fatalf("refusals changed the sheet %+v or saved", got)
		}

		if err := spend("c1", 15, map[string]int{"second_wind": 10, "strike": 3}); err != nil {
			t.Fatal(err)
		}
		want := map[string]int{"second_wind": 10, "strike": 3}
		changed := eventsOf[model.StatsChanged](s)
		if len(changed) != 1 || !maps.Equal(changed[0].Sheet.Skills.Points, want) || changed[0].Sheet.Skills.Unspent != 2 {
			t.Fatalf("stats changed %+v; want the points invested and 2 left", changed)
		}
		// Deep Breath at 10 points: 10 + 1 a point tenths of a point a second of HP regen.
		if sk := changed[0].Sheet.DerivedSkills; sk.HPRegen != 2 || changed[0].Sheet.Derived.HPRegen-changed[0].Sheet.DerivedWorn.HPRegen < 2 {
			t.Fatalf("the skills' share %+v; want Deep Breath's 2 a second", sk)
		}
		if saved := cps.Drain(); len(saved) != 1 || !maps.Equal(saved[0].Skills.Points, want) || saved[0].Skills.Unspent != 2 {
			t.Fatalf("checkpoints %+v; want the spend saved at once", saved)
		}
		if err := spend("c1", 2, map[string]int{"strike": 2}); err != nil {
			t.Fatal(err)
		}
		got := eventsOf[model.StatsChanged](s)
		if len(got) != 2 || got[1].Sheet.Skills.Points["strike"] != 5 || got[1].Sheet.Skills.Unspent != 0 {
			t.Fatalf("a second spend %+v; want Strike at 5 and none left", got)
		}
		if got[0].Sheet.Skills.Points["strike"] != 3 {
			t.Fatalf("the first sheet now says %+v; want it to keep its own copy", got[0].Sheet.Skills)
		}
		if err := spend("c2", 0, map[string]int{"strike": 1}); !errors.Is(err, transient.ErrUnavailable) {
			t.Fatalf("through another connection: %v", err)
		}
	})
}

// C17: a skill takes no point below its level, whatever else the spend holds, and takes them from that level on.
func TestASkillTakesNoPointBelowItsLevel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadDataWith(t, map[string]func(string) string{"skills.yaml": func(s string) string {
			const throw = "  - id: throw\n"
			if !strings.Contains(s, throw) {
				t.Fatal("skills.yaml has no throw")
			}
			return strings.Replace(s, throw, throw+"    level: 4\n", 1)
		}})
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{CheckpointInterval: time.Hour})
		t.Cleanup(stop)
		x, z := worldtest.TownSpawn(t, data)
		for _, c := range []struct {
			level int
			add   map[string]int
			want  error
		}{
			{3, map[string]int{"throw": 1}, model.ErrSkillLocked},
			{3, map[string]int{"strike": 2, "throw": 1}, model.ErrSkillLocked},
			{3, map[string]int{"strike": 2}, nil},
			{4, map[string]int{"throw": 1}, nil},
		} {
			p := worldtest.NewPlayer("Ayla", x, z)
			p.Level, p.Connection, p.Skills = c.level, "c1", model.Skills{Unspent: 5}
			s := join(t, inst, p)
			err := inst.SpendSkillPoints(context.Background(), p.ID, "c1", 5, c.add)
			tick(1)
			if !errors.Is(err, c.want) {
				t.Fatalf("level %d spending %v: %v, want %v", c.level, c.add, err, c.want)
			}
			if spent := len(eventsOf[model.StatsChanged](s)) == 1; spent != (c.want == nil) {
				t.Fatalf("level %d spending %v: sheet changed %v", c.level, c.add, spent)
			}
			if err := inst.Leave(p.ID, "c1", nil); err != nil {
				t.Fatal(err)
			}
			tick(1)
		}
	})
}
