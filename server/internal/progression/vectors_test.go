package progression_test

import (
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"finga/internal/gamedata"
	"finga/internal/gamedata/gamedatatest"
	"finga/internal/kernel"
	"finga/internal/progression"
)

// The vectors in testdata/stats must be reproduced here and by the client's preview; -update rewrites their expected sheets.
var update = flag.Bool("update", false, "rewrite the expected sheets in testdata/stats")

const vectorsDir = "../../testdata/stats"

// vector is one file: a character's class, level, points spent, gear and skill points, with the sheet they make under data/.
type vector struct {
	Name     string                `json:"name"`
	Notes    string                `json:"notes,omitempty"`
	Class    kernel.Class          `json:"class"`
	Level    int                   `json:"level"`
	Points   gamedata.Stats        `json:"points"`
	Worn     progression.ItemStats `json:"worn"`
	Skills   map[string]int        `json:"skills,omitempty"` // points per skill
	Expected progression.Sheet     `json:"expected"`
}

// Every case must have a file.
var requiredStatVectors = []string{
	"warrior-level-1", "warrior-level-10-auto-assigned", "bowman-level-1", "bowman-level-20-spread",
	"warrior-every-stat-worn", "bowman-rates-at-their-limits", "magic-build",
	"beginner-level-1", "thief-level-15-auto-assigned", "magician-level-15-auto-assigned", "beginner-second-wind-evolved",
}

func TestStatVectors(t *testing.T) {
	data := gamedatatest.Shipped(t)
	files, err := filepath.Glob(filepath.Join(vectorsDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		names[name] = true
		t.Run(name, func(t *testing.T) {
			v := loadVector(t, file)
			if v.Name != name {
				t.Fatalf("name %q inside the file, want %q", v.Name, name)
			}
			if _, ok := data.Class(v.Class); !ok {
				t.Fatalf("class %q", v.Class)
			}
			var learned []progression.Learned
			for id, points := range v.Skills {
				s, ok := data.Skill(id)
				if !ok {
					t.Fatalf("skill %q", id)
				}
				learned = append(learned, progression.Learned{Skill: s, Points: points})
			}
			got := progression.SheetOf(data.StatRules(), v.Class, v.Level, progression.AddStats(data.StartClass().BaseStats, v.Points), v.Worn, progression.Passives(learned))
			if *update {
				v.Expected = got
				saveVector(t, file, v)
				return
			}
			if got.Primary != v.Expected.Primary || got.PrimaryWorn != v.Expected.PrimaryWorn || got.PrimarySkills != v.Expected.PrimarySkills {
				t.Fatalf("primary %+v, worn %+v and skills %+v, want %+v, %+v and %+v", got.Primary, got.PrimaryWorn, got.PrimarySkills, v.Expected.Primary, v.Expected.PrimaryWorn, v.Expected.PrimarySkills)
			}
			assertDerived(t, got.Derived, v.Expected.Derived)
			assertDerived(t, got.DerivedWorn, v.Expected.DerivedWorn)
			assertDerived(t, got.DerivedSkills, v.Expected.DerivedSkills)
		})
	}
	for _, name := range requiredStatVectors {
		if !names[name] {
			t.Errorf("missing vector %s.json", name)
		}
	}
}

// Every stat travels in the vectors under its data-file name, so the client reads them by the same names.
func TestStatVectorsNameEveryStat(t *testing.T) {
	sheet := progression.Sheet{}
	worn := progression.ItemStats{}
	for _, v := range []any{&sheet.Derived, &sheet.DerivedWorn, &worn, &sheet.Primary, &sheet.PrimaryWorn} {
		rv := reflect.ValueOf(v).Elem()
		for _, f := range rv.Fields() {
			f.Set(reflect.ValueOf(1).Convert(f.Type()))
		}
	}
	data, err := json.Marshal(struct {
		Sheet progression.Sheet     `json:"sheet"`
		Worn  progression.ItemStats `json:"worn"`
	}{sheet, worn})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"str", "dex", "int", "agi", "vit", "primary", "primary_worn", "derived", "derived_worn",
		"physical_attack", "magic_attack", "physical_defense", "magic_defense", "max_hp", "accuracy", "evasion",
		"crit_rate", "crit_damage", "penetration", "hp_regen", "cooldown_speed", "move_speed",
	} {
		if !strings.Contains(string(data), `"`+key+`":`) {
			t.Errorf("JSON lacks %q: %s", key, data)
		}
	}
	if n := strings.Count(string(data), `":1`); n != 13*2+18+5*2 {
		t.Errorf("%d stats in the JSON, want every one of the sheet's and the worn set's: %s", n, data)
	}
}

func loadVector(t *testing.T, file string) vector {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var v vector
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	if math.IsNaN(v.Expected.Derived.CritRate) {
		t.Fatalf("%s: no expected sheet", file)
	}
	return v
}

func saveVector(t *testing.T, file string, v vector) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
