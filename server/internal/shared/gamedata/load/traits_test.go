package load_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
)

var update = flag.Bool("update", false, "rewrite testdata/traits/extras.json from the shipped data")

// extrasFile is each extra trait's resolved stage, as gen-gamedata.ts writes it, which its own test reads back.
const extrasFile = "../../../../testdata/traits/extras.json"

// C20 §2: the loader and gen-gamedata.ts resolve each extra alike; this writes the loader's side, web/src/sim/gamedata.test.ts checks the generator's.
func TestEachExtraResolvesAsTheClientResolvesIt(t *testing.T) {
	d, err := load.Dir("../../../../data")
	if err != nil {
		t.Fatal(err)
	}
	extras := map[string]map[string]any{}
	for _, s := range d.Skills() {
		if len(s.Traits) == 2 {
			extras[s.ID] = map[string]any{s.Traits[1].ID: asGenerated(s.Traits[1].Stage)}
		}
	}
	got, err := json.MarshalIndent(extras, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *update {
		if err := os.MkdirAll("../../../../testdata/traits", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(extrasFile, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(extrasFile)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the loader's extras (%v); rerun with -update and check web's gamedata test", extrasFile, err)
	}
}

// asGenerated is a stage in gen-gamedata.ts's form: times in ms, a field no stage named as null, a shape with only what it names, and a bonus without its zero stats.
func asGenerated(st gamedata.Stage) map[string]any {
	ms := func(d time.Duration) int64 { return d.Milliseconds() }
	scaled := func(s gamedata.Scaled) map[string]any { return map[string]any{"base": s.Base, "per_point": s.PerPoint} }
	inflict := func(in gamedata.SkillInflict) any {
		if in == (gamedata.SkillInflict{}) {
			return nil
		}
		return map[string]any{"status": in.Status, "time_ms": ms(in.Time), "power": scaled(in.Power)}
	}
	out := map[string]any{
		"at": st.At, "name": st.Name, "description": st.Description, "icon": st.Icon, "damage": nil, "shape": nil,
		"energy": st.Energy, "cooldown_ms": ms(st.Cooldown), "power": scaled(st.Power), "targets": st.Targets,
		"duration_ms": ms(st.Duration), "bonus": map[string]any{}, "length_ms": ms(st.Length), "hit": st.Hit, "cancel": st.Cancel,
		"ready": st.Ready, "cancelable": st.Cancelable, "move": nil, "dodge_ms": ms(st.Dodge), "block_ms": ms(st.Block), "status": inflict(st.Status),
		"push": st.Push, "hits": nil, "reach_party": st.ReachParty, "on_hit": inflict(st.OnHit), "shield": nil,
	}
	if st.Damage != "" {
		out["damage"] = string(st.Damage)
	}
	if sh := st.Shape; sh != (gamedata.Shape{}) {
		shape := map[string]any{"kind": string(sh.Kind)}
		for k, v := range map[string]float64{"radius": sh.Radius, "angle": sh.Angle, "height": sh.Height, "range": sh.Range, "width": sh.Width, "reach": sh.Reach} {
			if v != 0 {
				shape[k] = v
			}
		}
		if sh.At != gamedata.AtUser {
			shape["at"] = string(sh.At)
		}
		out["shape"] = shape
	}
	bonus := reflect.ValueOf(st.Bonus)
	for k := range bonus.NumField() {
		if v := bonus.Field(k).Interface().(gamedata.Scaled); v != (gamedata.Scaled{}) {
			out["bonus"].(map[string]any)[snake(bonus.Type().Field(k).Name)] = scaled(v)
		}
	}
	if mv := st.Move; mv != (gamedata.Move{}) {
		out["move"] = map[string]any{"kind": string(mv.Kind), "distance": mv.Distance, "time_ms": ms(mv.Time), "delay_ms": ms(mv.Delay), "toward": string(mv.Toward), "stop": mv.Stop, "through": mv.Through}
	}
	if st.Hits != (gamedata.Hits{}) {
		out["hits"] = map[string]any{"count": st.Hits.Count, "every_ms": ms(st.Hits.Every)}
	}
	if st.Shield != (gamedata.Shield{}) {
		out["shield"] = map[string]any{"power": scaled(st.Shield.Power)}
	}
	return out
}

var (
	wordStart  = regexp.MustCompile(`([a-z])([A-Z])`)
	acronymEnd = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
)

// snake is a stat field's YAML key: PhysicalAttack physical_attack, MaxHP max_hp, HPRegen hp_regen.
func snake(field string) string {
	return strings.ToLower(wordStart.ReplaceAllString(acronymEnd.ReplaceAllString(field, "${1}_${2}"), "${1}_${2}"))
}

// C20 §2: the extra's changes go on top of the last stage, which keeps its name; the default changes nothing.
func TestTraitsResolveOnTheLastStage(t *testing.T) {
	d, err := load.Dir(write(t, fixture()))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := d.Skill("strike")
	if len(s.Traits) != 2 || s.Traits[0].ID != "swift" || s.Traits[0].Stage != (gamedata.Stage{}) || s.Traits[1].ID != "concussion" {
		t.Fatalf("traits %+v", s.Traits)
	}
	want := s.Stages[2]
	want.Status = gamedata.SkillInflict{Status: "stun", Time: time.Second}
	want.Cooldown = 7 * time.Second
	if got := s.Traits[1].Stage; got != want {
		t.Fatalf("Concussion's stage\n got %+v\nwant %+v", got, want)
	}
	if got := s.Traits[1].Stage; got.Name != "Crushing Blow" || got.Targets != 3 || got.Energy != 3 {
		t.Fatalf("Concussion keeps Crushing Blow's name, targets and cost: %+v", got)
	}
}

func TestTraitRejections(t *testing.T) {
	traits := "\n    traits:\n      - {id: a, name: A, description: \"A.\"}\n      - {id: b, name: B, description: \"B.\"}"
	concussion := `{id: concussion, name: Concussion, description: "Stuns.", status: {id: stun, time: 1s}, cooldown: 7s}`
	tests := []struct {
		name, file, old, replace string
		wantAll                  []string
	}{
		{"traits on a passive", "skills.yaml", `bonus: {hp_regen: {base: 10}}}`, `bonus: {hp_regen: {base: 10}}}` + traits, []string{"second_wind", "only an active skill that takes points"}},
		{"traits on a granted skill", "skills.yaml", `{at: 0, name: Break Free, description: "Ends the hold."}`, `{at: 0, name: Break Free, description: "Ends the hold."}` + traits, []string{"break_free", "only an active skill that takes points"}},
		{"a default alone", "skills.yaml", "\n      - " + concussion, "", []string{"strike", "1 traits", "a default and an extra"}},
		{"three traits", "skills.yaml", concussion, concussion + "\n      - {id: daze, name: Daze, description: \"Dazes.\"}", []string{"strike", "3 traits"}},
		{"a default changing something", "skills.yaml", `{id: swift, name: Swift, description: "Back soon."}`, `{id: swift, name: Swift, description: "Back soon.", cooldown: 3s}`, []string{"strike", "swift", "changes nothing"}},
		{"two traits with one id", "skills.yaml", "{id: concussion,", "{id: swift,", []string{"strike", "two traits named swift"}},
		{"a trait without a description", "skills.yaml", `name: Concussion, description: "Stuns.", `, "name: Concussion, ", []string{"strike", "an id, a name and a description"}},
		{"stages on a trait", "skills.yaml", "cooldown: 7s}", "cooldown: 7s, stages: [{at: 0}]}", []string{"skills.yaml", "stages"}},
		{"an extra failing a stage's checks", "skills.yaml", "status: {id: stun, time: 1s}, cooldown: 7s}", "status: {id: stun}, cooldown: 7s}", []string{"strike", "trait concussion", "stun lasts a time"}},
		{"an extra a party buff can't be", "skills.yaml", "status: {id: stun, time: 1s}, cooldown: 7s}", "reach_party: true, cooldown: 7s}", []string{"strike", "trait concussion", "only a buff"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := freed()
			before := files[tt.file]
			files[tt.file] = strings.Replace(before, tt.old, tt.replace, 1)
			if files[tt.file] == before {
				t.Fatalf("replacement %q not found in %s", tt.old, tt.file)
			}
			_, err := load.Dir(write(t, files))
			if err == nil {
				t.Fatal("no error")
			}
			for _, want := range tt.wantAll {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// C20 §2 and §3, on the shipped data: every active skill that takes points has a default and an extra, and the extras resolve on top of the last stage.
func TestTheShippedTraits(t *testing.T) {
	d, err := load.Dir("../../../../data")
	if err != nil {
		t.Fatal(err)
	}
	extras := 0
	for _, s := range d.Skills() {
		takes := s.Kind == gamedata.Active && !s.Granted
		if takes != (len(s.Traits) == 2) || !takes && len(s.Traits) != 0 {
			t.Errorf("%s: %d traits; an active skill that takes points has two, any other none", s.ID, len(s.Traits))
		}
		if takes {
			extras++
		}
	}
	if extras != 29 {
		t.Errorf("%d skills with traits, want C20's 29", extras)
	}
	extra := func(skill string) gamedata.Stage {
		s, _ := d.Skill(skill)
		return s.Traits[1].Stage
	}
	last := func(skill string) gamedata.Stage {
		s, _ := d.Skill(skill)
		return s.Stages[len(s.Stages)-1]
	}
	tests := []struct {
		name string
		ok   bool
	}{
		{"Concussion stuns Crushing Blow's three targets for 1 s, ready again in 7 s", extra("strike").Name == "Crushing Blow" && extra("strike").Targets == 3 && extra("strike").Status == gamedata.SkillInflict{Status: "stun", Time: time.Second} && extra("strike").Cooldown == 7*time.Second},
		{"Puncture breaks armour in place of Bleeding Stab's bleed", last("double_stab").Status.Status == "bleed" && extra("double_stab").Status == gamedata.SkillInflict{Status: "armor_break", Time: 5 * time.Second}},
		{"Blur drops Haste's speed and triples its evasion", last("haste").Bonus.MoveSpeed != gamedata.Scaled{} && extra("haste").Bonus.MoveSpeed == gamedata.Scaled{} && extra("haste").Bonus.Evasion == gamedata.Scaled{Base: 30, PerPoint: 3}},
		{"Iron Wall turns Iron Body into a party buff within 6 m, for 15 s", !last("iron_body").ReachParty && extra("iron_body").ReachParty && extra("iron_body").Shape.Radius == 6 && extra("iron_body").Duration == 15*time.Second},
		{"Fury keeps War Cry to the Warrior alone", last("war_cry").ReachParty && !extra("war_cry").ReachParty},
		{"Ice Arrow slows in place of Searing Arrow's burn", last("fire_arrow").Status.Status == "burn" && extra("fire_arrow").Status == gamedata.SkillInflict{Status: "slow", Time: 4 * time.Second}},
		{"Dive rolls along the heading", last("roll_back").Move.Toward == gamedata.TowardBack && extra("roll_back").Move.Toward == gamedata.TowardHeading},
		{"Ice Prison centres at the lock", extra("frost_nova").Shape == gamedata.Shape{Kind: gamedata.AreaShape, Radius: 3, At: gamedata.AtLock, Reach: 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatal("check failed")
			}
		})
	}
}
