package load_test

import (
	"strings"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
)

// roster is the fixture with a skill for each of C17's fields, the Beginner teaching them all.
func roster() map[string]string {
	files := fixture()
	files["classes.yaml"] = strings.Replace(files["classes.yaml"], "skills: [strike, second_wind]", "skills: [strike, second_wind, lunge, roll_back, shadow_step, taunt, meteor, war_cry, poison_blade, barrier, disengage]", 1)
	files["skills.yaml"] += `  - id: lunge
    icon: lorc/sprint
    kind: active
    effect: damage
    damage: physical
    level: 3
    shape: {kind: arc, radius: 1.875, angle: 120, height: 1.75}
    energy: 2
    cooldown: 6s
    length: 1s
    power: {base: 1.2, per_point: 0.04}
    move: {kind: dash, distance: 4, time: 200ms, toward: lock}
    control: {kind: stun, time: 1500ms}
    hits: {count: 3, every: 100ms}
    stages:
      - {at: 0, name: Lunge, description: "Lunges at the lock."}
      - {at: 10, name: Long Lunge, description: "Farther, and stuns longer.", move: {distance: 5}, control: {time: 2s}, hits: {count: 4}}
  - id: roll_back
    icon: lorc/dodging
    kind: active
    effect: move
    level: 4
    energy: 2
    cooldown: 8s
    length: 600ms
    move: {kind: leap, distance: 4, time: 300ms, toward: back, stop: false}
    dodge: 400ms
    stages:
      - {at: 0, name: Roll Back, description: "Rolls back."}
  - id: shadow_step
    icon: lorc/ninja-heroic-stance
    kind: active
    effect: damage
    damage: physical
    shape: {kind: arc, radius: 1.875, angle: 90, height: 1.75}
    energy: 3
    cooldown: 10s
    length: 1s
    power: {base: 1.6, per_point: 0.05}
    move: {kind: blink, distance: 8, toward: lock}
    stages:
      - {at: 0, name: Shadow Step, description: "Steps behind the lock."}
  - id: taunt
    icon: lorc/shouting
    kind: active
    effect: control
    shape: {kind: area, radius: 6}
    energy: 2
    cooldown: 15s
    length: 600ms
    control: {kind: taunt, time: 5s}
    stages:
      - {at: 0, name: Taunt, description: "Every mob near attacks you."}
  - id: meteor
    icon: lorc/fire-ring
    kind: active
    effect: damage
    damage: magic
    shape: {kind: area, radius: 3, at: lock, reach: 12}
    energy: 3
    cooldown: 15s
    length: 1s
    power: {base: 3.2, per_point: 0.08}
    targets: 6
    control: {kind: knockdown}
    stages:
      - {at: 0, name: Meteor, description: "A meteor on the lock."}
      - {at: 10, name: Comet, description: "Knocks back instead.", control: {kind: push, distance: 2}}
  - id: war_cry
    icon: lorc/shouting
    kind: active
    effect: buff
    shape: {kind: area, radius: 8}
    energy: 3
    cooldown: 60s
    length: 1s
    duration: 30s
    reach_party: true
    bonus: {physical_attack: {base: 8, per_point: 1}}
    stages:
      - {at: 0, name: War Cry, description: "The party hits harder."}
  - id: poison_blade
    icon: lorc/poison-bottle
    kind: active
    effect: buff
    shape: {kind: area, radius: 1}
    energy: 3
    cooldown: 45s
    length: 1s
    duration: 20s
    poison: {time: 4s, power: {base: 0.2, per_point: 0.01}}
    stages:
      - {at: 0, name: Poison Blade, description: "Hits poison."}
      - {at: 10, name: Venom Blade, description: "Stronger poison.", poison: {power: {base: 0.3}}}
  - id: barrier
    icon: lorc/bolt-shield
    kind: active
    effect: buff
    shape: {kind: area, radius: 1}
    energy: 3
    cooldown: 30s
    length: 1s
    duration: 10s
    shield: {power: {base: 2, per_point: 0.1}}
    stages:
      - {at: 0, name: Barrier, description: "A shield."}
  - id: disengage
    icon: lorc/run
    kind: active
    effect: damage
    damage: physical
    shape: {kind: arc, radius: 3, angle: 90, height: 1.75}
    energy: 2
    cooldown: 12s
    length: 600ms
    power: {base: 1, per_point: 0}
    move: {kind: leap, distance: 5, time: 300ms, toward: back}
    dodge: 400ms
    control: {kind: push, distance: 2}
    stages:
      - {at: 0, name: Disengage, description: "Leaps back, pushing."}
`
	return files
}

// C17: each new field resolves, with a stage changing only what it names, and a new kind of control starting afresh.
func TestTheRosterFieldsLoad(t *testing.T) {
	d, err := load.Dir(write(t, roster()))
	if err != nil {
		t.Fatal(err)
	}
	skill := func(id string) gamedata.Skill {
		s, ok := d.Skill(id)
		if !ok {
			t.Fatalf("no skill %s", id)
		}
		return s
	}
	if strike, lunge := skill("strike"), skill("lunge"); strike.Level != 1 || lunge.Level != 3 {
		t.Errorf("levels %d and %d, want 1 when absent and lunge's 3", strike.Level, lunge.Level)
	}
	lunge := skill("lunge").Stages
	if want := (gamedata.Move{Kind: gamedata.DashMove, Distance: 4, Time: 200 * time.Millisecond, Toward: gamedata.TowardLock, Stop: true}); lunge[0].Move != want {
		t.Errorf("lunge's move %+v, want %+v, stopping at a body by default", lunge[0].Move, want)
	}
	if lunge[1].Move.Distance != 5 || lunge[1].Move.Time != 200*time.Millisecond || lunge[1].Control != (gamedata.Control{Kind: gamedata.Stun, Time: 2 * time.Second}) || lunge[1].Hits != (gamedata.Hits{Count: 4, Every: 100 * time.Millisecond}) {
		t.Errorf("Long Lunge %+v %+v %+v; want only distance, stun time and count changed", lunge[1].Move, lunge[1].Control, lunge[1].Hits)
	}
	roll := skill("roll_back")
	if roll.Effect != gamedata.MoveEffect || roll.Stages[0].Move.Stop || roll.Stages[0].Dodge != 400*time.Millisecond || roll.Stages[0].Shape != (gamedata.Shape{}) {
		t.Errorf("roll back %+v", roll.Stages[0])
	}
	if step := skill("shadow_step").Stages[0].Move; step != (gamedata.Move{Kind: gamedata.BlinkMove, Distance: 8, Toward: gamedata.TowardLock}) {
		t.Errorf("shadow step's move %+v, a blink toward the lock with no time or stop", step)
	}
	if taunt := skill("taunt"); taunt.Effect != gamedata.ControlEffect || taunt.Stages[0].Control != (gamedata.Control{Kind: gamedata.Taunt, Time: 5 * time.Second}) || taunt.Stages[0].Power != (gamedata.Scaled{}) {
		t.Errorf("taunt %+v", taunt.Stages[0])
	}
	meteor := skill("meteor").Stages
	if meteor[0].Shape.At != gamedata.AtLock || meteor[0].Shape.Reach != 12 || meteor[0].Control != (gamedata.Control{Kind: gamedata.Knockdown}) {
		t.Errorf("meteor %+v %+v", meteor[0].Shape, meteor[0].Control)
	}
	if meteor[1].Control != (gamedata.Control{Kind: gamedata.Push, Distance: 2}) {
		t.Errorf("Comet's control %+v, want a push alone", meteor[1].Control)
	}
	if cry := skill("war_cry").Stages[0]; !cry.ReachParty || cry.Bonus.PhysicalAttack != (gamedata.Scaled{Base: 8, PerPoint: 1}) {
		t.Errorf("war cry %+v", cry)
	}
	blade := skill("poison_blade").Stages
	if blade[0].Poison != (gamedata.Poison{Time: 4 * time.Second, Power: gamedata.Scaled{Base: 0.2, PerPoint: 0.01}}) || blade[1].Poison != (gamedata.Poison{Time: 4 * time.Second, Power: gamedata.Scaled{Base: 0.3, PerPoint: 0.01}}) {
		t.Errorf("poison blade's poisons %+v, %+v", blade[0].Poison, blade[1].Poison)
	}
	if shield := skill("barrier").Stages[0].Shield; shield != (gamedata.Shield{Power: gamedata.Scaled{Base: 2, PerPoint: 0.1}}) {
		t.Errorf("barrier's shield %+v", shield)
	}
	if dis := skill("disengage").Stages[0]; dis.Dodge != 400*time.Millisecond || dis.Control != (gamedata.Control{Kind: gamedata.Push, Distance: 2}) || dis.Move.Toward != gamedata.TowardBack {
		t.Errorf("disengage %+v", dis)
	}
}

// C17: each field refuses what its skill's kind, effect or own kind doesn't have.
func TestTheRosterFieldsRejections(t *testing.T) {
	tests := []struct {
		name, file, old, replace string
		wantAll                  []string
	}{
		{"a level below 1", "skills.yaml", "    level: 3\n", "    level: -1\n", []string{"skills.yaml", "lunge", "level"}},
		{"a level past the cap", "skills.yaml", "    level: 3\n", "    level: 5\n", []string{"skills.yaml", "lunge", "level 5", "cap, 4"}},
		{"a move with no distance", "skills.yaml", "{kind: dash, distance: 4, time: 200ms", "{kind: dash, time: 200ms", []string{"skills.yaml", "lunge", "distance"}},
		{"a dash with no time", "skills.yaml", "{kind: dash, distance: 4, time: 200ms, toward: lock}", "{kind: dash, distance: 4, toward: lock}", []string{"skills.yaml", "lunge", "dash has a time"}},
		{"a move of an unknown kind", "skills.yaml", "{kind: dash, distance: 4", "{kind: teleport, distance: 4", []string{"skills.yaml", "lunge", "teleport"}},
		{"a move with no way", "skills.yaml", "time: 200ms, toward: lock}", "time: 200ms}", []string{"skills.yaml", "lunge", "toward"}},
		{"a blink with a time", "skills.yaml", "{kind: blink, distance: 8, toward: lock}", "{kind: blink, distance: 8, time: 100ms, toward: lock}", []string{"skills.yaml", "shadow_step", "blink has no time"}},
		{"a blink naming stop", "skills.yaml", "{kind: blink, distance: 8, toward: lock}", "{kind: blink, distance: 8, toward: lock, stop: false}", []string{"skills.yaml", "shadow_step", "stop"}},
		{"a stun with no time", "skills.yaml", "control: {kind: stun, time: 1500ms}", "control: {kind: stun}", []string{"skills.yaml", "lunge", "stun", "time"}},
		{"a stun with a distance", "skills.yaml", "control: {kind: stun, time: 1500ms}", "control: {kind: stun, time: 1500ms, distance: 2}", []string{"skills.yaml", "lunge", "stun", "no distance"}},
		{"a push with no distance", "skills.yaml", "    control: {kind: push, distance: 2}\n    stages:\n      - {at: 0, name: Disengage", "    control: {kind: push}\n    stages:\n      - {at: 0, name: Disengage", []string{"skills.yaml", "disengage", "push"}},
		{"a push with a time", "skills.yaml", "    control: {kind: push, distance: 2}\n    stages:\n      - {at: 0, name: Disengage", "    control: {kind: push, distance: 2, time: 1s}\n    stages:\n      - {at: 0, name: Disengage", []string{"skills.yaml", "disengage", "push", "no time"}},
		{"a knockdown with a time", "skills.yaml", "control: {kind: knockdown}", "control: {kind: knockdown, time: 1s}", []string{"skills.yaml", "meteor", "knockdown"}},
		{"a control of an unknown kind", "skills.yaml", "control: {kind: taunt, time: 5s}", "control: {kind: sleep, time: 5s}", []string{"skills.yaml", "taunt", "sleep"}},
		{"a control skill without a control", "skills.yaml", "    control: {kind: taunt, time: 5s}\n", "", []string{"skills.yaml", "taunt", "control"}},
		{"a control on a buff", "skills.yaml", "    reach_party: true\n", "    reach_party: true\n    control: {kind: stun, time: 1s}\n", []string{"skills.yaml", "war_cry", "control"}},
		{"a move skill without a move", "skills.yaml", "    move: {kind: leap, distance: 4, time: 300ms, toward: back, stop: false}\n", "", []string{"skills.yaml", "roll_back", "move"}},
		{"a move skill with a shape", "skills.yaml", "    effect: move\n", "    effect: move\n    shape: {kind: area, radius: 2}\n", []string{"skills.yaml", "roll_back", "shape"}},
		{"hits only once", "skills.yaml", "hits: {count: 3, every: 100ms}", "hits: {count: 1, every: 100ms}", []string{"skills.yaml", "lunge", "twice"}},
		{"hits with no time apart", "skills.yaml", "hits: {count: 3, every: 100ms}", "hits: {count: 3}", []string{"skills.yaml", "lunge", "twice"}},
		{"hits on a control skill", "skills.yaml", "control: {kind: taunt, time: 5s}", "control: {kind: taunt, time: 5s}\n    hits: {count: 2, every: 100ms}", []string{"skills.yaml", "taunt", "damage skill repeats"}},
		{"an arc at the lock", "skills.yaml", "shape: {kind: arc, radius: 1.875, angle: 90, height: 1.75}", "shape: {kind: arc, radius: 1.875, angle: 90, height: 1.75, at: lock, reach: 5}", []string{"skills.yaml", "shadow_step", "arc"}},
		{"an area at the lock with no reach", "skills.yaml", "at: lock, reach: 12}", "at: lock}", []string{"skills.yaml", "meteor", "reach"}},
		{"an area with a reach but no lock", "skills.yaml", "at: lock, reach: 12}", "reach: 12}", []string{"skills.yaml", "meteor", "reach"}},
		{"an area at an unknown centre", "skills.yaml", "at: lock, reach: 12}", "at: ground, reach: 12}", []string{"skills.yaml", "meteor", "ground"}},
		{"reach_party on a damage skill", "skills.yaml", "    control: {kind: knockdown}\n", "    control: {kind: knockdown}\n    reach_party: true\n", []string{"skills.yaml", "meteor", "reach_party"}},
		{"reach_party off an area", "skills.yaml", "shape: {kind: area, radius: 8}", "shape: {kind: arc, radius: 8, angle: 90, height: 2}", []string{"skills.yaml", "war_cry", "reach_party"}},
		{"a poison on a damage skill", "skills.yaml", "    control: {kind: knockdown}\n", "    control: {kind: knockdown}\n    poison: {time: 4s, power: {base: 0.2}}\n", []string{"skills.yaml", "meteor", "poison"}},
		{"a poison with no time", "skills.yaml", "poison: {time: 4s, power:", "poison: {power:", []string{"skills.yaml", "poison_blade", "poison"}},
		{"a poison with no power", "skills.yaml", "poison: {time: 4s, power: {base: 0.2, per_point: 0.01}}", "poison: {time: 4s}", []string{"skills.yaml", "poison_blade", "poison"}},
		{"a shield with a power falling with points", "skills.yaml", "shield: {power: {base: 2, per_point: 0.1}}", "shield: {power: {base: 2, per_point: -0.1}}", []string{"skills.yaml", "barrier", "shield"}},
		{"a buff giving nothing", "skills.yaml", "    shield: {power: {base: 2, per_point: 0.1}}\n", "", []string{"skills.yaml", "barrier", "bonus, a poison or a shield"}},
		{"a bonus on a control skill", "skills.yaml", "control: {kind: taunt, time: 5s}", "control: {kind: taunt, time: 5s}\n    bonus: {physical_attack: {base: 1}}", []string{"skills.yaml", "taunt", "bonus"}},
		{"a dodge on a passive", "skills.yaml", "    bonus: {hp_regen: {base: 5, per_point: 1}}\n", "    bonus: {hp_regen: {base: 5, per_point: 1}}\n    dodge: 300ms\n", []string{"skills.yaml", "second_wind", "dodge"}},
		{"a dodge below zero", "skills.yaml", "    dodge: 400ms\n    stages:\n      - {at: 0, name: Roll Back", "    dodge: -400ms\n    stages:\n      - {at: 0, name: Roll Back", []string{"skills.yaml", "roll_back", "dodge"}},
		{"a stage giving a blink a stop", "skills.yaml", `{at: 0, name: Shadow Step, description: "Steps behind the lock."}`, `{at: 0, name: Shadow Step, description: "Steps behind the lock.", move: {stop: true}}`, []string{"skills.yaml", "shadow_step", "Shadow Step", "stop"}},
		{"a stage turning a stun into a push without a distance", "skills.yaml", `move: {distance: 5}, control: {time: 2s}`, `move: {distance: 5}, control: {kind: push, time: 2s}`, []string{"skills.yaml", "lunge", "Long Lunge", "push"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := roster()
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
