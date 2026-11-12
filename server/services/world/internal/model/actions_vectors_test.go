package model_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"finga/services/world/internal/model"
)

// The vectors in testdata/actions must be reproduced here and by the client's action clock; -update rewrites their expected ticks and points.
var update = flag.Bool("update", false, "rewrite the expected ticks and points in testdata/actions")

const actionVectorsDir = "../../../../testdata/actions"

// actionVector is one file: an action's kind, length, attack speed, shares and tick rate, with the ticks and points they make.
type actionVector struct {
	Name        string  `json:"name"`
	Notes       string  `json:"notes,omitempty"`
	Kind        string  `json:"kind"` // attack or skill
	LengthMs    int64   `json:"length_ms"`
	AttackSpeed float64 `json:"attack_speed"`
	Hit         float64 `json:"hit"`
	Cancel      float64 `json:"cancel"`
	TickHz      int     `json:"tick_hz"`
	Expected    points  `json:"expected"`
}

type points struct {
	Ticks  uint64 `json:"ticks"`
	Hit    uint64 `json:"hit"`
	Cancel uint64 `json:"cancel"`
}

// Every case must have a file.
var requiredActionVectors = []string{
	"beginner-attack", "warrior-attack", "thief-attack", "bowman-attack", "magician-attack",
	"attack-speed", "attack-speed-part-tick", "skill-ignores-attack-speed", "skill-part-tick",
	"share-on-a-whole-tick", "wind-up", "hit-at-cancel", "one-tick", "zero-cancel", "full-cancel", "thirty-hz",
}

func TestActionVectors(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(actionVectorsDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		names = append(names, name)
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var v actionVector
			if err := json.Unmarshal(raw, &v); err != nil {
				t.Fatal(err)
			}
			if v.Name != name {
				t.Fatalf("name %q inside the file, want %q", v.Name, name)
			}
			length, interval := time.Duration(v.LengthMs)*time.Millisecond, time.Second/time.Duration(v.TickHz)
			var got points
			switch v.Kind {
			case "attack":
				got.Ticks = model.AttackLengthTicks(length, v.AttackSpeed, interval)
			case "skill":
				got.Ticks = model.SkillLengthTicks(length, interval)
			default:
				t.Fatalf("kind %q: attack or skill", v.Kind)
			}
			got.Hit, got.Cancel = model.ActionPoints(got.Ticks, v.Hit, v.Cancel)
			if *update {
				v.Expected = got
				out, err := json.MarshalIndent(v, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, append(out, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			if got != v.Expected {
				t.Fatalf("got %+v, want %+v", got, v.Expected)
			}
		})
	}
	for _, name := range requiredActionVectors {
		if !slices.Contains(names, name) {
			t.Errorf("no vector %s.json in %s", name, actionVectorsDir)
		}
	}
}
