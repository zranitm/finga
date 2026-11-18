package load_test

import (
	"cmp"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// wantMovement are the movement constants data/movement.yaml and the fixture both hold.
var wantMovement = sim.Params{
	Dt: 1.0 / 60, WalkSpeed: 5, JumpVelocity: 12.34, Gravity: 38, MaxFallSpeed: 22.5,
	Radius: 0.375, Height: 1.75, Step: 0.3, MaxSlope: 0.7, Snap: 0.5,
	Sprint: sim.Sprint{After: 2, Speed: 1.6},
}

// fixture is a minimal valid data folder; each rejection row breaks one thing in it.
func fixture() map[string]string {
	return map[string]string{
		"movement.yaml": `
dt: 0.016666666666666666
walk_speed: 5
jump_velocity: 12.34
gravity: 38
max_fall_speed: 22.5
radius: 0.375
height: 1.75
step: 0.3
max_slope: 0.7
snap: 0.5
sprint: {after: 2, speed: 1.6}
`,
		"loot.yaml": `
collect_delay: 1s
drop_life: 60s
room_check_every: 2s
`,
		"chat.yaml": `
line_max: 120
`,
		"actions.yaml": `
cancel: 0.85
ready: 0.85
energy: {max: 10, per_attack: 2, per_block: 2}
block: {arc: 180}
combat: 5s
`,
		"skills.yaml": `
skills:
  - id: strike
    icon: lorc/sword-wound
    kind: active
    effect: damage
    damage: physical
    shape: {kind: arc, radius: 2.375, angle: 120, height: 1.75}
    energy: 2
    cooldown: 2s
    length: 700ms
    cancelable: true
    power: {base: 1.5, per_point: 0.05}
    stages:
      - {at: 0, name: Strike, description: "A strong swing."}
      - {at: 10, name: Heavy Strike, description: "A harder swing.", power: {base: 2.2}, icon: lorc/sword-slice}
      - {at: 25, name: Crushing Blow, description: "Up to three foes.", energy: 3, targets: 3, length: 900ms, hit: 0.3, cancel: 0.6, ready: 0.7, cancelable: false}
    traits:
      - {id: swift, name: Swift, description: "Back soon."}
      - {id: concussion, name: Concussion, description: "Stuns.", status: {id: stun, time: 1s}, cooldown: 7s}
  - id: second_wind
    icon: lorc/energy-breath
    kind: passive
    effect: bonus
    bonus: {hp_regen: {base: 5, per_point: 1}}
    stages:
      - {at: 0, name: Second Wind, description: "Faster regen."}
      - {at: 10, name: Deep Breath, description: "Faster still.", bonus: {hp_regen: {base: 10}}}
`,
		"classes.yaml": `
classes:
  - id: beginner
    name: Beginner
    description: "Tries everything."
    look: {model: barbarian, tint: 0xd8c8a8}
    attack: {kind: arc, damage: physical, radius: 1.875, angle: 120, height: 1.5, length: 700ms}
    base_stats: {str: 12, dex: 5, int: 4, agi: 5, vit: 10}
    starter_items: [{item: wooden_sword, quantity: 1}]
    skills: [strike, second_wind]
  - id: warrior
    name: Warrior
    description: "Fights up close."
    parent: beginner
    look: {model: knight}
    attack: {kind: arc, damage: physical, radius: 2.375, angle: 120, height: 1.75, length: 600ms}
    auto_assign: {str: 3, vit: 2}
    promotion: {trainer: drillmaster, level: 3, stats: {str: 20, vit: 10}, skills: {strike: 2}, quests: [first_hunt], weapon: wooden_sword}
  - id: bowman
    name: Bowman
    description: "Fights from afar."
    parent: beginner
    look: {model: rogue_hooded}
    attack: {kind: shot, damage: magic, range: 13.125, width: 0.6, length: 800ms, hit: 0.25, cancel: 0.5, ready: 0.6}
    auto_assign: {dex: 3, agi: 2}
    promotion: {trainer: hunter, level: 2, weapon: short_bow}
`,
		"stats.yaml": `
points_per_level: 5
skill_points_per_level: 5
per_point:
  str: {physical_attack: 1.5, max_hp: 2}
  dex: {physical_attack: 0.5, magic_attack: 0.5, accuracy: 1, crit_damage: 0.005}
  int: {magic_attack: 1.5, magic_defense: 0.5}
  agi: {evasion: 1}
  vit: {max_hp: 10, physical_defense: 1, magic_defense: 0.5, hp_regen: 0.05}
diminishing:
  crit_rate:      {stat: dex, base: 0.05, limit: 0.60, k: 200}
  cooldown_speed: {stat: int, limit: 0.40, k: 200}
  move_speed:     {stat: agi, limit: 0.30, k: 200}
hit: {top: 0.97, k: 0.12, power: 2}
limits: {penetration: 0.50, hit_chance: [0.20, 0.99]}
hardiest: warrior
classes:
  beginner: {max_hp: {base: 50, per_level: 12}, accuracy: {base: 10}}
  warrior: {max_hp: {base: 60, per_level: 18}, accuracy: {base: 10}, crit_damage: {base: 0.5}}
  bowman:  {max_hp: {base: 50, per_level: 12}, accuracy: {base: 15}}
`,
		"exp_curve.yaml": `
level_cap: 4
exp_to_next:
  - {level: 1, exp: 20}
  - {level: 2, exp: 65}
  - {level: 3, exp: 129}
`,
		"items.yaml": `
equip_slots:
  - {id: weapon, name: Weapon, kind: weapon}
  - {id: hat, name: Hat, kind: hat}
  - {id: ring_1, name: Ring, kind: ring}
  - {id: ring_2, name: Ring, kind: ring}
items:
  - {id: wooden_sword, name: Wooden Sword, tab: equip, slot: weapon, class: warrior, min_level: 1, stars: 1, description: "A practice blade.", stats: {physical_attack: [10, 12], str: [0, 2]}}
  - {id: short_bow, name: Short Bow, tab: equip, slot: weapon, class: bowman, min_level: 1, stars: 1, description: "A small bow.", stats: {physical_attack: [8, 10]}}
  - {id: leather_cap, name: Leather Cap, tab: equip, slot: hat, stars: 1, description: "A cap.", requires: {str: 8, vit: 6}, stats: {physical_defense: [3, 4], move_speed: [5, 10], hp_regen: [1, 2]}}
  - {id: red_potion, name: Red Potion, tab: use, heal_hp: 50, stars: 1, description: "A red tonic."}
  - {id: slime_gel, name: Slime Gel, tab: etc, stars: 2, description: "Sticky."}
`,
		"statuses.yaml": `
super_armor: {hard: immune, others: 0.5}
statuses:
  - {id: stun, group: hard, blocks: [move, act]}
  - {id: freeze, group: hard, blocks: [move, act], shatter: 1.5}
  - {id: sleep, group: hard, blocks: [move, act], wakes: true}
  - {id: knockdown, group: hard, blocks: [move, act]}
  - {id: pin, group: hard, blocks: [move]}
  - {id: slow, group: soft, move_speed: 0.6}
  - {id: silence, group: soft, blocks: [skills]}
  - {id: blind, group: soft, accuracy: 0.5}
  - {id: poison, group: dot, every: 1s}
  - {id: burn, group: dot, every: 500ms}
  - {id: bleed, group: dot, every: 1s, moving: 2.0}
  - {id: armor_break, group: weaken, physical_defense: 0.7, magic_defense: 0.7}
  - {id: weaken, group: weaken, attack: 0.75}
  - {id: taunt, group: aggro}
  - {id: lose_sight, group: aggro}
`,
		"mobs.yaml": `
hit_guard: 500ms
provoke: 5s
sight: 9.375
leash: 16
engage: [0ms, 400ms]
react: {flinch: 300ms, push: 0.25, down_hits: 3, down_window: 1500ms, down: 1s, armor_after: 1s}
patrol: {speed_jitter: 0.15, pause: [600ms, 1400ms]}
attacks:
  hop: {kind: leap, windup: 600ms, distance: 2.25, travel: 400ms, spread: 0.375, recover: 300ms, cooldown: 2500ms, power: 1}
  slam: {kind: around, windup: 1000ms, spread: 1.5, recover: 600ms, cooldown: 5s, power: 1.5}
  spit: {kind: shot, windup: 600ms, range: 7.5, speed: 9.375, size: 0.375, recover: 300ms, cooldown: 2500ms, power: 1}
field_boss:
  life: 10m
  drop_share: 0.01
mobs:
  - id: green_slime
    name: Green Slime
    level: 1
    hp: 30
    attack: 6
    damage: physical
    physical_defense: 2
    magic_defense: 1
    accuracy: 8
    evasion: 2
    exp: 5
    speed: 1.25
    radius: 0.5
    height: 0.875
    behavior: patrol
    attacks: [{use: hop}]
    look: {model: green_blob}
    drops:
      - {item: slime_gel, chance: 0.6, min: 1, max: 2}
      - {item: red_potion, chance: 0.15, min: 1, max: 1}
  - id: slime_lord
    name: Slime Lord
    boss: true
    level: 5
    hp: 900
    attack: 12
    damage: physical
    physical_defense: 3
    magic_defense: 2
    accuracy: 8
    evasion: 2
    exp: 200
    speed: 0.9375
    radius: 1.5
    height: 2.625
    behavior: patrol
    super_armor: true
    attacks: [{use: hop, distance: 5, spread: 1}, {use: slam}]
    look: {model: green_blob, scale: 3}
    drops:
      - {item: red_potion, chance: 1, min: 2, max: 4}
`,
		"npcs.yaml": `
talk_reach: 1.875
npcs:
  - id: guide
    name: Guide
    look: guide
    gives: [first_hunt, gel_for_me]
    takes: [first_hunt]
  - id: hunter
    name: Hunter
    look: hunter
    trainer: bowman
    gives: [warrior_drill]
    takes: [gel_for_me, warrior_drill]
  - id: drillmaster
    name: Drillmaster
    look: trainer_warrior
    trainer: warrior
`,
		"quests.yaml": `
max_active: 20
quests:
  - id: first_hunt
    name: First Hunt
    giver: guide
    turn_in: guide
    objectives:
      - {kill: green_slime, count: 3}
      - {level: 2}
    rewards: {exp: 10, items: [{item: red_potion, quantity: 5}]}
  - id: gel_for_me
    name: Gel for Me
    giver: guide
    turn_in: hunter
    level: 2
    quests: [first_hunt]
    objectives:
      - {collect: slime_gel, count: 4}
      - {talk: hunter}
    rewards: {exp: 20, items: [{item: leather_cap, quantity: 1}]}
    repeatable: true
  - id: warrior_drill
    name: Warrior Drill
    giver: hunter
    turn_in: hunter
    class: warrior
    objectives:
      - {kill: green_slime, count: 1}
`,
		"world.yaml": `
places:
  town: [40, 50]
  field: [60, 50]
teleport:
  channel: 2500ms
`,
		"maps/pit.yaml": `
name: Pit
kind: pvp
look: {ground: sand, sky: day}
size: [20, 20]
terrain: {cell: 4}
portals: []
sides:
  - [{at: [10, 4], yaw: 0}]
  - [{at: [10, 16], yaw: 180}]
prep_wall:
  - {box: [10, 10, 20, 0.6], height: 3, model: prep_wall}
`,
		"pvp.yaml": `
modes:
  duel: {name: 1v1, team: 1, party: 1, map: pit, rated: true, rounds: {best_of: 3, most: 5, length: 60s}}
queue: {bots_after: 15s, offer: 10s, window: {start: 2, per_second: 0.2, most: 8}}
rounds: {prep: 10s, countdown: 3s, between: 5s}
result: 10s
kit: {level: 4}
scale: {hp: 1.0, damage: 1.0}
rating: {mu: 25, sigma: 8.333, beta: 4.167, tau: 0.083, shown: {base: 1000, per_point: 40}, placement: 5}
ranks:
  - {id: bronze, from: 0}
  - {id: silver, from: 1200}
bots:
  most: 600
  tiers:
    - {id: novice, mu: 15, reaction: 450ms, defend: 0.15, chain: 0.3, break_free: 1.2s, aim: 20}
    - {id: adept, mu: 20, reaction: 350ms, defend: 0.35, chain: 0.55, break_free: 0.8s, aim: 12}
  names: [N01, N02, N03, N04, N05, N06, N07, N08, N09, N10, N11, N12, N13, N14, N15,
          N16, N17, N18, N19, N20, N21, N22, N23, N24, N25, N26, N27, N28, N29, N30]
  builds:
    warrior:
      - {bar: [strike, '', '', '', '', '', '', ''], traits: {}, order: [strike]}
    bowman:
      - {bar: [strike, '', '', '', '', '', '', ''], traits: {}, order: [strike]}
`,
		"maps/town.yaml": `
name: Somewhere
kind: town
start: true
look: {ground: dirt, sky: day}
size: [40, 12]
terrain: {cell: 4}
portals:
  - {name: to_field, at: [38, 6], radius: 1.2, to: {map: field, portal: from_town}}
player_spawn: {at: [3, 6], yaw: 90}
npcs:
  - {id: guide, at: [6, 6], yaw: 180}
`,
		"maps/field.yaml": `
name: Somewhere
kind: hunting
town: town
levels: [1, 5]
look: {ground: grass, sky: day}
size: [40, 20]
terrain:
  cell: 4
  mesas:
    - {area: [24, 2, 8, 6], height: 1.2, edge: 0.5}
solids:
  - {box: [14, 14, 4, 2], height: 2, yaw: 30, model: rock}
  - {cylinder: [20, 4], radius: 0.6, height: 6, model: tree}
portals:
  - {name: from_town, at: [2, 10], radius: 1.2, to: {map: town, portal: to_field}}
spawns:
  - {name: slimes, at: [12, 6], radius: 4, mob: green_slime, max: 4, respawn: 10s}
player_spawn: {at: [3, 10], yaw: 90}
npcs:
  - {id: hunter, at: [20, 12], yaw: 180}
  - {id: drillmaster, at: [28, 5], yaw: 180}
boss: {at: [30, 14], radius: 4, mob: slime_lord, kills: 20}
`,
	}
}

func write(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestValidDataLoads(t *testing.T) {
	d, err := load.Dir(write(t, fixture()))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("the most active quests and the longest chat line", func(t *testing.T) {
		if d.MaxActiveQuests() != 20 || d.ChatLineMax() != 120 || d.TeleportChannel() != 2500*time.Millisecond {
			t.Fatalf("max active %d, chat line %d, teleport channel %v", d.MaxActiveQuests(), d.ChatLineMax(), d.TeleportChannel())
		}
	})
	t.Run("a mob's attacks from the table, with its own changes, and the rules they share", func(t *testing.T) {
		hop := gamedata.MobAttack{Name: "hop", Kind: gamedata.Leap, Windup: 600 * time.Millisecond, Travel: 400 * time.Millisecond, Recover: 300 * time.Millisecond, Cooldown: 2500 * time.Millisecond, Power: 1, Distance: 2.25, Spread: 0.375}
		big := hop
		big.Distance, big.Spread = 5, 1
		slam := gamedata.MobAttack{Name: "slam", Kind: gamedata.Around, Windup: time.Second, Recover: 600 * time.Millisecond, Cooldown: 5 * time.Second, Power: 1.5, Spread: 1.5}
		slime, _ := d.Mob("green_slime")
		lord, _ := d.Mob("slime_lord")
		if !slices.Equal(slime.Attacks, []gamedata.MobAttack{hop}) || !slices.Equal(lord.Attacks, []gamedata.MobAttack{big, slam}) {
			t.Fatalf("green_slime's attacks %+v, slime_lord's %+v", slime.Attacks, lord.Attacks)
		}
		want := gamedata.MobRules{HitGuard: 500 * time.Millisecond, Provoke: 5 * time.Second, Sight: 9.375, Leash: 16, React: gamedata.MobReact{
			Flinch: 300 * time.Millisecond, Push: 0.25, DownHits: 3, DownWindow: 1500 * time.Millisecond, Down: time.Second, ArmorAfter: time.Second,
		}, Patrol: gamedata.MobPatrol{SpeedJitter: 0.15, PauseMin: 600 * time.Millisecond, PauseMax: 1400 * time.Millisecond}, EngageMax: 400 * time.Millisecond}
		if got := d.MobRules(); got != want {
			t.Fatalf("mob rules %+v", got)
		}
	})
	t.Run("the field boss rules, and a boss marked as one, with super armor", func(t *testing.T) {
		if got := d.FieldBoss(); got != (gamedata.FieldBossRules{Life: 10 * time.Minute, DropShare: 0.01}) {
			t.Fatalf("field boss rules %+v", got)
		}
		lord, _ := d.Mob("slime_lord")
		slime, _ := d.Mob("green_slime")
		if !lord.Boss || lord.Elite || !lord.SuperArmor || slime.Boss || slime.SuperArmor {
			t.Fatalf("slime_lord %+v, green_slime %+v", lord, slime)
		}
	})
	t.Run("the class tree, with its attacks and auto-assign splits", func(t *testing.T) {
		root := d.StartClass()
		if root.ID != "beginner" || root.Parent != "" || !slices.Equal(root.Line, []kernel.Class{"beginner"}) {
			t.Fatalf("root = %+v, want the beginner", root)
		}
		if root.BaseStats.STR != 12 || len(root.StarterItems) != 1 || root.AutoAssign != (gamedata.Stats{}) {
			t.Fatalf("root stats %+v, starters %+v, split %+v", root.BaseStats, root.StarterItems, root.AutoAssign)
		}
		if _, ok := d.Class("thief"); ok {
			t.Fatal("thief is not in the fixture but was found")
		}
		w, ok := d.Class("warrior")
		if !ok || w.Parent != "beginner" || !slices.Equal(w.Line, []kernel.Class{"warrior", "beginner"}) {
			t.Fatalf("warrior = %+v, want a child of the beginner", w)
		}
		if w.Attack != (gamedata.Attack{Shape: gamedata.Shape{Kind: gamedata.ArcShape, Radius: 2.375, Angle: 120, Height: 1.75}, Damage: gamedata.Physical, Length: 600 * time.Millisecond, Cancel: 0.85, Ready: 0.85}) {
			t.Fatalf("warrior attack = %+v", w.Attack)
		}
		if w.AutoAssign != (gamedata.Stats{STR: 3, VIT: 2}) || w.BaseStats != (gamedata.Stats{}) {
			t.Fatalf("warrior stats = %+v / %+v", w.BaseStats, w.AutoAssign)
		}
		promotion := gamedata.Promotion{Trainer: "drillmaster", Level: 3, Stats: gamedata.Stats{STR: 20, VIT: 10}, Skills: map[string]int{"strike": 2}, Quests: []string{"first_hunt"}, Weapon: "wooden_sword"}
		if !reflect.DeepEqual(w.Promotion, promotion) || w.Description != "Fights up close." || !reflect.DeepEqual(root.Promotion, gamedata.Promotion{}) {
			t.Fatalf("warrior promotion %+v, description %q; root promotion %+v", w.Promotion, w.Description, root.Promotion)
		}
		b, _ := d.Class("bowman")
		if b.Attack.Shape != (gamedata.Shape{Kind: gamedata.ShotShape, Range: 13.125, Width: 0.6}) || b.Attack.Damage != gamedata.Magic || b.Attack.Timing != (gamedata.Timing{Length: 800 * time.Millisecond, Hit: 0.25, Cancel: 0.5, Ready: 0.6}) {
			t.Fatalf("bowman attack = %+v", b.Attack)
		}
	})
	t.Run("skills, each stage resolved from the ones before it", func(t *testing.T) {
		strike, ok := d.Skill("strike")
		if !ok || strike.Class != "beginner" || strike.Kind != gamedata.Active || strike.Effect != gamedata.DamageEffect || len(strike.Stages) != 3 {
			t.Fatalf("strike = %+v", strike)
		}
		heavy, crushing := strike.Stages[1], strike.Stages[2]
		if heavy.Name != "Heavy Strike" || heavy.Power != (gamedata.Scaled{Base: 2.2, PerPoint: 0.05}) || heavy.Shape != strike.Stages[0].Shape || heavy.Energy != 2 || heavy.Cooldown != 2*time.Second || heavy.Targets != 1 || heavy.Damage != gamedata.Physical {
			t.Fatalf("Heavy Strike = %+v: want its power's base changed and everything else carried", heavy)
		}
		if icons := []string{strike.Stages[0].Icon, heavy.Icon, crushing.Icon}; !slices.Equal(icons, []string{"lorc/sword-wound", "lorc/sword-slice", "lorc/sword-slice"}) {
			t.Fatalf("icons %v: want the skill's, Heavy Strike's own, and Heavy Strike's carried", icons)
		}
		if crushing.Targets != 3 || crushing.Power != heavy.Power || crushing.Energy != 3 {
			t.Fatalf("Crushing Blow = %+v: want three targets, Heavy Strike's power and its own cost of 3", crushing)
		}
		if first := strike.Stages[0]; first.Timing != (gamedata.Timing{Length: 700 * time.Millisecond, Cancel: 0.85, Ready: 0.85}) || !first.Cancelable || heavy.Timing != first.Timing || !heavy.Cancelable {
			t.Fatalf("Strike %+v, Heavy Strike %+v: want 700 ms at actions.yaml's cancel and ready points, cancelable, carried", first.Timing, heavy.Timing)
		}
		if crushing.Timing != (gamedata.Timing{Length: 900 * time.Millisecond, Hit: 0.3, Cancel: 0.6, Ready: 0.7}) || crushing.Cancelable {
			t.Fatalf("Crushing Blow timing %+v: want its own 900 ms, hit, cancel and ready points, and no longer cancelable", crushing.Timing)
		}
		if wind, _ := d.Skill("second_wind"); wind.Stages[0].Timing != (gamedata.Timing{}) || wind.Stages[0].Cancelable {
			t.Fatalf("Second Wind timing %+v: a passive has none", wind.Stages[0].Timing)
		}
		for points, want := range map[int]int{0: 0, 9: 0, 10: 1, 24: 1, 25: 2, 400: 2} {
			if got := strike.StageAt(points); got != want {
				t.Errorf("at %d points, stage %d; want %d", points, got, want)
			}
		}
		wind, _ := d.Skill("second_wind")
		deep := wind.Stages[1]
		if wind.Kind != gamedata.Passive || wind.Stages[0].Bonus.HPRegen != (gamedata.Scaled{Base: 5, PerPoint: 1}) ||
			deep.Bonus.HPRegen != (gamedata.Scaled{Base: 10, PerPoint: 1}) || wind.Stages[0].Energy != 0 {
			t.Fatalf("second wind = %+v", wind)
		}
		if got := (gamedata.Scaled{Base: 1.5, PerPoint: 0.05}).At(10); got != 2 {
			t.Fatalf("1.5 + 0.05 a point at 10 points = %v", got)
		}
		if r := d.StatRules(); r.SkillPointsPerLevel != 5 {
			t.Fatalf("skill points per level %d", r.SkillPointsPerLevel)
		}
	})
	t.Run("a class fits its own items, its ancestors' and classless ones", func(t *testing.T) {
		w, _ := d.Class("warrior")
		root := d.StartClass()
		for _, tt := range []struct {
			c    gamedata.Class
			item kernel.Class
			want bool
		}{
			{w, "", true}, {w, "warrior", true}, {w, "beginner", true}, {w, "bowman", false},
			{root, "", true}, {root, "beginner", true}, {root, "warrior", false},
		} {
			if got := tt.c.Fits(tt.item); got != tt.want {
				t.Errorf("%s fits a %q item = %v, want %v", tt.c.ID, tt.item, got, tt.want)
			}
		}
	})
	t.Run("the stat rules", func(t *testing.T) {
		r := d.StatRules()
		if r.PointsPerLevel != 5 {
			t.Fatalf("points per level %d", r.PointsPerLevel)
		}
		if want := (gamedata.Flats[float64]{PhysicalAttack: 0.5, MagicAttack: 0.5, Accuracy: 1, CritDamage: 0.005}); r.PerPoint.DEX != want {
			t.Fatalf("a point of DEX gives %+v, want %+v", r.PerPoint.DEX, want)
		}
		if want := (gamedata.Flats[float64]{MaxHP: 10, PhysicalDefense: 1, MagicDefense: 0.5, HPRegen: 0.05}); r.PerPoint.VIT != want {
			t.Fatalf("a point of VIT gives %+v, want %+v", r.PerPoint.VIT, want)
		}
		want := gamedata.Rates[gamedata.Curve]{
			CritRate:      gamedata.Curve{Stat: gamedata.DEX, Base: 0.05, Limit: 0.60, K: 200},
			CooldownSpeed: gamedata.Curve{Stat: gamedata.INT, Limit: 0.40, K: 200},
			MoveSpeed:     gamedata.Curve{Stat: gamedata.AGI, Limit: 0.30, K: 200},
		}
		if r.Curves != want {
			t.Fatalf("curves %+v, want %+v", r.Curves, want)
		}
		if r.Penetration != 0.5 || r.Hit.Min != 0.2 || r.Hit.Max != 0.99 {
			t.Fatalf("limits: penetration %v, hit %v to %v", r.Penetration, r.Hit.Min, r.Hit.Max)
		}
		w := r.Classes[kernel.Class("warrior")]
		if w.MaxHP != (gamedata.Grows{Base: 60, PerLevel: 18}) || w.CritDamage.Base != 0.5 || w.MaxHP.At(3) != 96 {
			t.Fatalf("warrior bases %+v", w)
		}
		if b := r.Classes[kernel.Class("bowman")]; b.Accuracy.Base != 15 || b.CritDamage != (gamedata.Grows{}) {
			t.Fatalf("bowman bases %+v: a stat left out has no base", b)
		}
	})
	t.Run("the energy skills cost", func(t *testing.T) {
		if got := d.Energy(); got != (gamedata.Energy{Max: 10, PerAttack: 2, PerBlock: 2}) {
			t.Fatalf("energy %+v, want a max of 10, 2 an attack and 2 a block", got)
		}
	})
	t.Run("the loot timings", func(t *testing.T) {
		if got, want := d.Loot(), (gamedata.Loot{CollectDelay: time.Second, DropLife: time.Minute, RoomCheckEvery: 2 * time.Second}); got != want {
			t.Fatalf("loot %+v, want %+v", got, want)
		}
	})
	t.Run("the EXP curve", func(t *testing.T) {
		if d.LevelCap() != 4 {
			t.Fatalf("cap %d", d.LevelCap())
		}
		for level, want := range map[int]int64{1: 20, 2: 65, 3: 129} {
			got, ok := d.ExpToNext(level)
			if !ok || got != want {
				t.Fatalf("ExpToNext(%d) = %d, %v; want %d", level, got, ok, want)
			}
		}
		if _, ok := d.ExpToNext(4); ok {
			t.Fatal("ExpToNext at the cap should be false")
		}
		if _, ok := d.ExpToNext(0); ok {
			t.Fatal("ExpToNext(0) should be false")
		}
	})
	t.Run("items and mobs", func(t *testing.T) {
		it, ok := d.Item("wooden_sword")
		if !ok || it.Tab != gamedata.Equip || it.Slot != "weapon" || it.Class != kernel.Class("warrior") || it.MaxStack != 1 || it.Stars != 1 || it.Description != "A practice blade." {
			t.Fatalf("wooden_sword = %+v", it)
		}
		if want := (gamedata.Rolls{PhysicalAttack: gamedata.Range{Min: 10, Max: 12}, STR: gamedata.Range{Max: 2}}); it.Rolls != want || it.Requires != (gamedata.Stats{}) {
			t.Fatalf("wooden_sword rolls %+v, want %+v, and no requirements", it.Rolls, want)
		}
		cap, _ := d.Item("leather_cap")
		if want := (gamedata.Rolls{PhysicalDefense: gamedata.Range{Min: 3, Max: 4}, MoveSpeed: gamedata.Range{Min: 5, Max: 10}, HPRegen: gamedata.Range{Min: 1, Max: 2}}); cap.Rolls != want || cap.Requires != (gamedata.Stats{STR: 8, VIT: 6}) || cap.Class != "" {
			t.Fatalf("leather_cap = %+v", cap)
		}
		if gel, _ := d.Item("slime_gel"); gel.Stars != 2 || gel.Rolls != (gamedata.Rolls{}) {
			t.Fatalf("slime_gel = %+v", gel)
		}
		potion, _ := d.Item("red_potion")
		if potion.Tab != gamedata.Use || potion.MaxStack != 100 || potion.HealHP != 50 {
			t.Fatalf("red_potion = %+v", potion)
		}
		m, ok := d.Mob("green_slime")
		if !ok || m.Level != 1 || m.HP != 30 || m.Damage != gamedata.Physical || m.PhysicalDefense != 2 || m.MagicDefense != 1 || len(m.Drops) != 2 || m.Drops[0].Item != "slime_gel" || m.Drops[0].Max != 2 {
			t.Fatalf("green_slime = %+v", m)
		}
		if _, ok := d.Item("nope"); ok {
			t.Fatal("unknown item found")
		}
		if _, ok := d.Mob("nope"); ok {
			t.Fatal("unknown mob found")
		}
	})
	t.Run("equip slots, in file order and by ID", func(t *testing.T) {
		var ids []string
		for _, s := range d.EquipSlots() {
			ids = append(ids, s.ID)
		}
		if !slices.Equal(ids, []string{"weapon", "hat", "ring_1", "ring_2"}) {
			t.Fatalf("slots %v", ids)
		}
		if s, ok := d.EquipSlot("ring_2"); !ok || s.Kind != "ring" || s.Name != "Ring" {
			t.Fatalf("ring_2: %+v, %v", s, ok)
		}
		if _, ok := d.EquipSlot("boots"); ok {
			t.Fatal("found a slot the data doesn't have")
		}
	})
	t.Run("movement", func(t *testing.T) {
		if d.Movement() != wantMovement {
			t.Fatalf("movement = %+v", d.Movement())
		}
	})
	t.Run("maps", func(t *testing.T) {
		town, ok := d.Map("town")
		if !ok || town.Kind != gamedata.Town || town.ID != "town" {
			t.Fatalf("town = %+v", town)
		}
		if d.StartTown().ID != "town" {
			t.Fatalf("StartTown() = %s", d.StartTown().ID)
		}
		field, _ := d.Map("field")
		if !town.Start || field.Start || town.Name != "Somewhere" || field.MinLevel != 1 || field.MaxLevel != 5 || town.MinLevel != 0 {
			t.Fatalf("town %+v, field %+v: start, name and levels", town, field)
		}
		if town.Town != "town" || field.Town != "town" {
			t.Fatalf("linked towns: town %q, field %q", town.Town, field.Town)
		}
		if town.Geometry.SizeX != 40 || town.Geometry.SizeZ != 12 || len(field.Geometry.Boxes) != 1 || len(field.Geometry.Cylinders) != 1 {
			t.Fatalf("town size %v x %v, field solids %+v %+v", town.Geometry.SizeX, town.Geometry.SizeZ, field.Geometry.Boxes, field.Geometry.Cylinders)
		}
		if want := (gamedata.Portal{Name: "to_field", At: gamedata.Point{X: 38, Z: 6}, Radius: 1.2, TargetMap: "field", TargetPortal: "from_town"}); !slices.Equal(town.Portals, []gamedata.Portal{want}) {
			t.Fatalf("town portals %+v", town.Portals)
		}
		if want := (gamedata.Spawn{Mob: "green_slime", Max: 4, Respawn: 10 * time.Second, At: gamedata.Point{X: 12, Z: 6}, Radius: 4}); !slices.Equal(field.Spawns, []gamedata.Spawn{want}) {
			t.Fatalf("field spawns %+v", field.Spawns)
		}
		if field.PlayerSpawn != (gamedata.Point{X: 3, Z: 10}) || field.SpawnYaw != math.Pi/2 {
			t.Fatalf("field's player spawn %+v facing %v, want (3, 10) facing east", field.PlayerSpawn, field.SpawnYaw)
		}
		if field.Boss == nil || *field.Boss != (gamedata.BossSpot{Mob: "slime_lord", Kills: 20, At: gamedata.Point{X: 30, Z: 14}, Radius: 4}) || town.Boss != nil {
			t.Fatalf("field's boss %+v, town's %+v", field.Boss, town.Boss)
		}
		if g := field.Grid(); g != field.Grid() {
			t.Fatal("a map's walk grid is built again at each call")
		}
		if len(d.Maps()) != 3 {
			t.Fatalf("%d maps", len(d.Maps()))
		}
	})
	t.Run("a match's map, its sides and prep wall, and pvp.yaml with each promoted class's kit", func(t *testing.T) {
		pit, ok := d.Map("pit")
		if !ok || pit.Kind != gamedata.Pvp || pit.Town != "" || len(pit.Portals) != 0 {
			t.Fatalf("pit %+v", pit)
		}
		want := [2][]gamedata.Spot{{{At: gamedata.Point{X: 10, Z: 4}}}, {{At: gamedata.Point{X: 10, Z: 16}, Yaw: math.Pi}}}
		if !reflect.DeepEqual(pit.Sides, want) || pit.PlayerSpawn != want[0][0].At {
			t.Fatalf("pit's sides %+v, its catch spot %+v; want %+v and side 1's first", pit.Sides, pit.PlayerSpawn, want)
		}
		if len(pit.Geometry.Boxes) != 0 || len(pit.Walled.Boxes) != 1 || pit.Walled.Boxes[0].HalfW != 10 || pit.Walled.Boxes[0].Top != 3 {
			t.Fatalf("pit's boxes %+v, walled %+v: want the prep wall in the walled geometry alone", pit.Geometry.Boxes, pit.Walled.Boxes)
		}
		duel, ok := d.PvpMode("duel")
		if !ok || duel != (gamedata.PvpMode{ID: "duel", Name: "1v1", Team: 1, Party: 1, Map: "pit", Rated: true, Rounds: gamedata.ModeRounds{BestOf: 3, Most: 5, Length: time.Minute}}) {
			t.Fatalf("duel %+v", duel)
		}
		r := d.Pvp()
		if r.Queue.BotsAfter != 15*time.Second || r.Queue.Window != (gamedata.QueueWindow{Start: 2, PerSecond: 0.2, Most: 8}) || r.Rounds.Countdown != 3*time.Second || r.Result != 10*time.Second {
			t.Fatalf("queue %+v, rounds %+v, result %v", r.Queue, r.Rounds, r.Result)
		}
		if r.Rating.Shown.PerPoint != 40 || r.Rating.Placement != 5 || len(r.Ranks) != 2 || r.Ranks[1] != (gamedata.Rank{ID: "silver", From: 1200}) {
			t.Fatalf("rating %+v, ranks %+v", r.Rating, r.Ranks)
		}
		if len(r.Bots.Tiers) != 2 || r.Bots.Tiers[0].Aim != 20*math.Pi/180 || len(r.Bots.Names) != 30 || len(r.Bots.Builds["warrior"]) != 1 {
			t.Fatalf("bots %+v", r.Bots)
		}
		// Level 4: the root's stats and 15 points by each split, then each slot's best item the class can wear with them.
		kits := map[kernel.Class]gamedata.Kit{
			"warrior": {Level: 4, Own: gamedata.Stats{STR: 21, DEX: 5, INT: 4, AGI: 5, VIT: 16}, Worn: []gamedata.KitItem{{Slot: "weapon", Item: "wooden_sword"}, {Slot: "hat", Item: "leather_cap"}}},
			"bowman":  {Level: 4, Own: gamedata.Stats{STR: 12, DEX: 14, INT: 4, AGI: 11, VIT: 10}, Worn: []gamedata.KitItem{{Slot: "weapon", Item: "short_bow"}, {Slot: "hat", Item: "leather_cap"}}},
		}
		for class, want := range kits {
			if got, ok := d.Kit(class); !ok || !reflect.DeepEqual(got, want) {
				t.Errorf("%s's kit %+v, want %+v", class, got, want)
			}
		}
		if _, ok := d.Kit("beginner"); ok {
			t.Error("the Beginner has a kit, though only promoted characters fight")
		}
	})
	t.Run("NPCs, placed by their maps", func(t *testing.T) {
		if d.TalkReach() != 1.875 {
			t.Fatalf("talk reach %v", d.TalkReach())
		}
		guide, ok := d.NPC("guide")
		if !ok || guide.Name != "Guide" || guide.Look != "guide" || guide.MapID != "town" || guide.At != (gamedata.Point{X: 6, Z: 6}) || guide.Yaw != math.Pi || guide.Trainer != "" {
			t.Fatalf("guide = %+v", guide)
		}
		if !slices.Equal(guide.Gives, []string{"first_hunt", "gel_for_me"}) || !slices.Equal(guide.Takes, []string{"first_hunt"}) {
			t.Fatalf("guide offers %v, takes %v", guide.Gives, guide.Takes)
		}
		hunter, _ := d.NPC("hunter")
		if hunter.MapID != "field" || hunter.Trainer != "bowman" {
			t.Fatalf("hunter = %+v, want the bowman's trainer", hunter)
		}
		if len(d.NPCs()) != 3 || d.NPCs()[0].ID != "guide" {
			t.Fatalf("NPCs() = %+v", d.NPCs())
		}
		if _, ok := d.NPC("nobody"); ok {
			t.Fatal("unknown NPC found")
		}
	})
	t.Run("quests, with each objective in one shape", func(t *testing.T) {
		first, ok := d.Quest("first_hunt")
		want := []gamedata.Objective{{Kind: gamedata.Kill, Target: "green_slime", Count: 3}, {Kind: gamedata.Reach, Count: 2}}
		if !ok || first.Name != "First Hunt" || first.Giver != "guide" || first.TurnIn != "guide" || !slices.Equal(first.Objectives, want) || first.Repeatable {
			t.Fatalf("first_hunt = %+v", first)
		}
		if first.Rewards.Exp != 10 || !slices.Equal(first.Rewards.Items, []gamedata.Grant{{Item: "red_potion", Quantity: 5}}) {
			t.Fatalf("first_hunt rewards %+v", first.Rewards)
		}
		gel, _ := d.Quest("gel_for_me")
		want = []gamedata.Objective{{Kind: gamedata.Collect, Target: "slime_gel", Count: 4}, {Kind: gamedata.TalkTo, Target: "hunter", Count: 1}}
		if gel.Level != 2 || !slices.Equal(gel.Requires, []string{"first_hunt"}) || !slices.Equal(gel.Objectives, want) || !gel.Repeatable || gel.TurnIn != "hunter" {
			t.Fatalf("gel_for_me = %+v", gel)
		}
		if drill, _ := d.Quest("warrior_drill"); drill.Class != kernel.Class("warrior") || drill.Rewards.Exp != 0 || drill.Rewards.Items != nil {
			t.Fatalf("warrior_drill = %+v", drill)
		}
		if len(d.Quests()) != 3 || d.Quests()[2].ID != "warrior_drill" {
			t.Fatalf("Quests() = %+v", d.Quests())
		}
		if _, ok := d.Quest("nope"); ok {
			t.Fatal("unknown quest found")
		}
	})
}

func TestRejections(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		old     string
		replace string
		wantAll []string // substrings the error must contain
	}{
		{"a drop table naming an unknown item", "mobs.yaml", `item: slime_gel`, `item: dragon_scale`, []string{"mobs.yaml", "green_slime", "dragon_scale"}},
		{"an equipment drop with a quantity above one", "mobs.yaml", `{item: red_potion, chance: 0.15, min: 1, max: 1}`, `{item: wooden_sword, chance: 0.15, min: 1, max: 2}`, []string{"mobs.yaml", "green_slime", "wooden_sword"}},
		{"a drop chance above one", "mobs.yaml", `chance: 0.6`, `chance: 1.5`, []string{"mobs.yaml", "green_slime", "chance"}},
		{"mobs.yaml without a hit guard", "mobs.yaml", "hit_guard: 500ms\n", "", []string{"mobs.yaml", "hit_guard"}},
		{"a provoke of zero", "mobs.yaml", "provoke: 5s", "provoke: 0s", []string{"mobs.yaml", "provoke"}},
		{"mobs.yaml without a sight", "mobs.yaml", "sight: 9.375\n", "", []string{"mobs.yaml", "sight"}},
		{"a sight that isn't a number", "mobs.yaml", "sight: 9.375", "sight: .nan", []string{"mobs.yaml", "sight"}},
		{"a push that isn't a number", "mobs.yaml", "push: 0.25", "push: .nan", []string{"mobs.yaml", "react", "push"}},
		{"a speed jitter that isn't a number", "mobs.yaml", "speed_jitter: 0.15", "speed_jitter: .nan", []string{"mobs.yaml", "patrol", "speed_jitter"}},
		{"an attack of endless distance", "mobs.yaml", "{use: hop, distance: 5", "{use: hop, distance: .inf", []string{"mobs.yaml", "slime_lord", "hop", "distance"}},
		{"a drop share that isn't a number", "mobs.yaml", "drop_share: 0.01", "drop_share: .nan", []string{"mobs.yaml", "field_boss", "drop_share"}},
		{"a drop chance that isn't a number", "mobs.yaml", "chance: 0.6", "chance: .nan", []string{"mobs.yaml", "green_slime", "chance"}},
		{"an endless mob width", "mobs.yaml", "    radius: 1.5\n", "    radius: .inf\n", []string{"mobs.yaml", "slime_lord"}},
		{"a cancel point that isn't a number", "actions.yaml", "cancel: 0.85", "cancel: .nan", []string{"actions.yaml", "cancel"}},
		{"an endless gravity", "movement.yaml", "gravity: 38", "gravity: .inf", []string{"movement.yaml", "gravity"}},
		{"a talk reach that isn't a number", "npcs.yaml", "talk_reach: 1.875", "talk_reach: .nan", []string{"npcs.yaml", "talk_reach"}},
		{"an endless melee attack", "classes.yaml", "radius: 2.375, angle: 120, height: 1.75, length: 600ms", "radius: .inf, angle: 120, height: 1.75, length: 600ms", []string{"classes.yaml", "warrior", "radius"}},
		{"a ranged attack's range that isn't a number", "classes.yaml", "range: 13.125", "range: .nan", []string{"classes.yaml", "bowman", "range"}},
		{"a skill's power that isn't a number", "skills.yaml", "power: {base: 1.5", "power: {base: .nan", []string{"skills.yaml", "strike", "power"}},
		{"an endless skill shape", "skills.yaml", "shape: {kind: arc, radius: 2.375", "shape: {kind: arc, radius: .inf", []string{"skills.yaml", "strike", "arc"}},
		{"a bonus that isn't a number", "skills.yaml", "hp_regen: {base: 5", "hp_regen: {base: .nan", []string{"skills.yaml", "second_wind", "bonus"}},
		{"an endless point", "stats.yaml", "  agi: {evasion: 1}", "  agi: {evasion: .inf}", []string{"stats.yaml", "agi", "evasion"}},
		{"a curve's k that isn't a number", "stats.yaml", "limit: 0.60, k: 200", "limit: 0.60, k: .nan", []string{"stats.yaml", "crit_rate", "k"}},
		{"an endless curve limit", "stats.yaml", "limit: 0.60", "limit: .inf", []string{"stats.yaml", "crit_rate", "limit"}},
		{"an endless hit curve", "stats.yaml", "k: 0.12", "k: .inf", []string{"stats.yaml", "hit"}},
		{"a penetration that isn't a number", "stats.yaml", "penetration: 0.50", "penetration: .nan", []string{"stats.yaml", "penetration"}},
		{"a hit chance that isn't a number", "stats.yaml", "hit_chance: [0.20, 0.99]", "hit_chance: [.nan, 0.99]", []string{"stats.yaml", "hit_chance"}},
		{"a class base that isn't a number", "stats.yaml", "accuracy: {base: 15}", "accuracy: {base: .nan}", []string{"stats.yaml", "bowman", "accuracy"}},
		{"mobs.yaml without its reactions", "mobs.yaml", "react: {flinch: 300ms, push: 0.25, down_hits: 3, down_window: 1500ms, down: 1s, armor_after: 1s}\n", "", []string{"mobs.yaml", "react"}},
		{"a knockdown at no hits", "mobs.yaml", "down_hits: 3", "down_hits: 0", []string{"mobs.yaml", "react", "down_hits"}},
		{"a negative push", "mobs.yaml", "push: 0.25", "push: -0.25", []string{"mobs.yaml", "react", "push"}},
		{"mobs.yaml without a patrol", "mobs.yaml", "patrol: {speed_jitter: 0.15, pause: [600ms, 1400ms]}\n", "", []string{"mobs.yaml", "patrol"}},
		{"a speed jitter of a half", "mobs.yaml", "speed_jitter: 0.15", "speed_jitter: 0.5", []string{"mobs.yaml", "patrol", "speed_jitter"}},
		{"a negative speed jitter", "mobs.yaml", "speed_jitter: 0.15", "speed_jitter: -0.1", []string{"mobs.yaml", "patrol", "speed_jitter"}},
		{"a pause running backwards", "mobs.yaml", "pause: [600ms, 1400ms]", "pause: [1400ms, 600ms]", []string{"mobs.yaml", "patrol", "pause"}},
		{"a pause of zero", "mobs.yaml", "pause: [600ms, 1400ms]", "pause: [0s, 1400ms]", []string{"mobs.yaml", "patrol", "pause"}},
		{"a pause of one value", "mobs.yaml", "pause: [600ms, 1400ms]", "pause: [600ms]", []string{"mobs.yaml", "patrol", "pause"}},
		{"mobs.yaml without an engage", "mobs.yaml", "engage: [0ms, 400ms]\n", "", []string{"mobs.yaml", "engage"}},
		{"an engage of one value", "mobs.yaml", "engage: [0ms, 400ms]", "engage: [400ms]", []string{"mobs.yaml", "engage"}},
		{"a negative engage", "mobs.yaml", "engage: [0ms, 400ms]", "engage: [-100ms, 400ms]", []string{"mobs.yaml", "engage"}},
		{"an engage running backwards", "mobs.yaml", "engage: [0ms, 400ms]", "engage: [400ms, 100ms]", []string{"mobs.yaml", "engage"}},
		{"a mob with no attack", "mobs.yaml", "    attacks: [{use: hop}]\n", "", []string{"mobs.yaml", "green_slime", "no attack"}},
		{"a mob using an attack not in the table", "mobs.yaml", "attacks: [{use: hop}]", "attacks: [{use: bite}]", []string{"mobs.yaml", "green_slime", "bite"}},
		{"an attack of an unknown kind", "mobs.yaml", "kind: around", "kind: kick", []string{"mobs.yaml", "slam", "kick"}},
		{"an attack missing a value its kind needs", "mobs.yaml", "spread: 1.5, ", "", []string{"mobs.yaml", "slam", "spread"}},
		{"an attack with a value its kind doesn't use", "mobs.yaml", "spread: 1.5, ", "spread: 1.5, reach: 0.5, ", []string{"mobs.yaml", "slam", "reach"}},
		{"an attack of no power", "mobs.yaml", "power: 1.5", "power: 0", []string{"mobs.yaml", "slam", "power"}},
		{"a mob's change to a negative distance", "mobs.yaml", "{use: hop, distance: 5", "{use: hop, distance: -1", []string{"mobs.yaml", "slime_lord", "hop", "distance"}},
		{"a cooldown shorter than the attack", "mobs.yaml", "cooldown: 5s", "cooldown: 1s", []string{"mobs.yaml", "slam", "cooldown"}},
		{"a spit whose range is shorter than its size", "mobs.yaml", "range: 7.5", "range: 0.25", []string{"mobs.yaml", "spit", "range"}},
		{"a mob changing an attack's kind", "mobs.yaml", "{use: slam}", "{use: slam, kind: leap}", []string{"mobs.yaml", "kind"}},
		{"an unblockable attack on a mob that isn't a boss", "mobs.yaml", "attacks: [{use: hop}]", "attacks: [{use: hop, unblockable: true}]", []string{"mobs.yaml", "green_slime", "hop", "only a boss's attack is unblockable"}},
		{"field_boss without a life", "mobs.yaml", "  life: 10m\n", "", []string{"mobs.yaml", "field_boss"}},
		{"a drop share above one", "mobs.yaml", "drop_share: 0.01", "drop_share: 1.5", []string{"mobs.yaml", "field_boss", "drop_share"}},
		{"a mob both a boss and an elite", "mobs.yaml", "    boss: true\n", "    boss: true\n    elite: true\n", []string{"mobs.yaml", "slime_lord", "elite"}},
		{"duplicate item IDs", "items.yaml", `id: slime_gel`, `id: red_potion`, []string{"items.yaml", "red_potion", "duplicate"}},
		{"duplicate mob IDs", "mobs.yaml", `mobs:`, "mobs:\n  - {id: green_slime, name: X, level: 1, hp: 1, attack: 1, damage: physical, physical_defense: 1, magic_defense: 1, accuracy: 1, evasion: 1, exp: 1, speed: 1, radius: 1, height: 1, behavior: patrol, attacks: [{use: hop}], look: {model: green_blob}}", []string{"mobs.yaml", "green_slime", "duplicate"}},
		{"a mob with an unknown damage type", "mobs.yaml", `damage: physical`, `damage: fire`, []string{"mobs.yaml", "green_slime", "fire"}},
		{"a mob with a negative defense", "mobs.yaml", `magic_defense: 1`, `magic_defense: -1`, []string{"mobs.yaml", "green_slime", "negative"}},
		{"duplicate class IDs", "classes.yaml", `id: bowman`, `id: warrior`, []string{"classes.yaml", "warrior", "duplicate"}},
		{"a mob tinted past RGB", "mobs.yaml", `look: {model: green_blob}`, `look: {model: green_blob, tint: 0x1000000}`, []string{"mobs.yaml", "green_slime", "tint"}},
		{"a mob scaled to nothing", "mobs.yaml", `look: {model: green_blob}`, `look: {model: green_blob, scale: 0}`, []string{"mobs.yaml", "green_slime", "scale"}},
		{"a mob with no model", "mobs.yaml", `look: {model: green_blob}`, `look: {tint: 0x5fbf4a}`, []string{"mobs.yaml", "green_slime", "model"}},
		{"the EXP curve must rise strictly", "exp_curve.yaml", `{level: 3, exp: 129}`, `{level: 3, exp: 65}`, []string{"exp_curve.yaml", "level 3"}},
		{"the EXP curve must cover every level below the cap", "exp_curve.yaml", "  - {level: 3, exp: 129}\n", "", []string{"exp_curve.yaml", "level 3"}},
		{"the EXP curve must not go past the cap", "exp_curve.yaml", `level_cap: 4`, `level_cap: 3`, []string{"exp_curve.yaml", "level 3"}},
		{"points per level below one", "stats.yaml", `points_per_level: 5`, `points_per_level: 0`, []string{"stats.yaml", "points_per_level"}},
		{"a point of a stat that isn't primary", "stats.yaml", `  agi: {evasion: 1}`, `  luk: {evasion: 1}`, []string{"stats.yaml", "luk"}},
		{"a point giving a rate, which has its curve", "stats.yaml", `  agi: {evasion: 1}`, `  agi: {evasion: 1, crit_rate: 0.01}`, []string{"stats.yaml", "agi", "crit_rate"}},
		{"a point giving a negative amount", "stats.yaml", `  agi: {evasion: 1}`, `  agi: {evasion: -1}`, []string{"stats.yaml", "agi", "evasion", "negative"}},
		{"a curve over a stat that isn't primary", "stats.yaml", `{stat: dex, base: 0.05`, `{stat: luk, base: 0.05`, []string{"stats.yaml", "crit_rate", "luk"}},
		{"a limit at its base", "stats.yaml", `base: 0.05, limit: 0.60`, `base: 0.05, limit: 0.05`, []string{"stats.yaml", "crit_rate", "limit"}},
		{"a limit below its base", "stats.yaml", `base: 0.05, limit: 0.60`, `base: 0.05, limit: 0.01`, []string{"stats.yaml", "crit_rate", "limit"}},
		{"a k of zero", "stats.yaml", `limit: 0.40, k: 200`, `limit: 0.40, k: 0`, []string{"stats.yaml", "cooldown_speed", "k"}},
		{"a k below zero", "stats.yaml", `limit: 0.40, k: 200`, `limit: 0.40, k: -5`, []string{"stats.yaml", "cooldown_speed", "k"}},
		{"a rate without a curve", "stats.yaml", "  move_speed:     {stat: agi, limit: 0.30, k: 200}\n", "", []string{"stats.yaml", "move_speed"}},
		{"a curve for a rate that doesn't exist", "stats.yaml", "diminishing:\n", "diminishing:\n  luck_rate: {stat: agi, limit: 0.3, k: 1}\n", []string{"stats.yaml", "luck_rate"}},
		{"an attack speed curve, which is gone", "stats.yaml", "diminishing:\n", "diminishing:\n  attack_speed: {stat: agi, limit: 0.5, k: 150}\n", []string{"stats.yaml", "attack_speed"}},
		{"a point giving attack speed, which is gone", "stats.yaml", `  agi: {evasion: 1}`, `  agi: {evasion: 1, attack_speed: 0.01}`, []string{"stats.yaml", "agi", "attack_speed"}},
		{"gear rolling attack speed, which is gone", "items.yaml", `stats: {physical_attack: [8, 10]}`, `stats: {physical_attack: [8, 10], attack_speed: [5, 10]}`, []string{"items.yaml", "short_bow", "attack_speed"}},
		{"a bonus to attack speed, which is gone", "skills.yaml", "bonus: {hp_regen: {base: 5, per_point: 1}}", "bonus: {attack_speed: {base: 5}}", []string{"skills.yaml", "second_wind", "attack_speed"}},
		{"penetration above one", "stats.yaml", `penetration: 0.50`, `penetration: 1.5`, []string{"stats.yaml", "penetration"}},
		{"a hit chance whose least is above its most", "stats.yaml", `hit_chance: [0.20, 0.99]`, `hit_chance: [0.99, 0.20]`, []string{"stats.yaml", "hit_chance"}},
		{"no hit curve", "stats.yaml", "hit: {top: 0.97, k: 0.12, power: 2}\n", "", []string{"stats.yaml", "hit"}},
		{"a hit curve's top above one", "stats.yaml", "top: 0.97", "top: 1.2", []string{"stats.yaml", "hit", "top"}},
		{"a hit curve without k", "stats.yaml", "k: 0.12, ", "", []string{"stats.yaml", "hit", "k"}},
		{"a hit curve with no power", "stats.yaml", "power: 2", "power: 0", []string{"stats.yaml", "hit", "power"}},
		{"class bases for a class that doesn't exist", "stats.yaml", "  bowman:", "  thief: {max_hp: {base: 1}}\n  bowman:", []string{"stats.yaml", "thief"}},
		{"a class base for a rate", "stats.yaml", `accuracy: {base: 15}`, `accuracy: {base: 15}, crit_rate: {base: 0.1}`, []string{"stats.yaml", "bowman", "crit_rate"}},
		{"a class's base below its parent's", "stats.yaml", "accuracy: {base: 15}", "accuracy: {base: 9}", []string{"stats.yaml", "bowman", "accuracy", "beginner"}},
		{"a class's growth below its parent's", "stats.yaml", "bowman:  {max_hp: {base: 50, per_level: 12}", "bowman:  {max_hp: {base: 50, per_level: 11}", []string{"stats.yaml", "bowman", "max_hp", "beginner"}},
		{"no hardiest class", "stats.yaml", "hardiest: warrior\n", "", []string{"stats.yaml", "hardiest"}},
		{"a hardiest class that doesn't exist", "stats.yaml", "hardiest: warrior", "hardiest: paladin", []string{"stats.yaml", "hardiest", "paladin"}},
		{"a class whose max HP grows faster than the hardiest's", "stats.yaml", "bowman:  {max_hp: {base: 50, per_level: 12}", "bowman:  {max_hp: {base: 50, per_level: 19}", []string{"stats.yaml", "bowman", "max_hp", "warrior"}},
		{"a class without bases", "stats.yaml", "  bowman:  {max_hp: {base: 50, per_level: 12}, accuracy: {base: 15}}\n", "", []string{"stats.yaml", "bowman"}},
		{"a class with an unknown attack kind", "classes.yaml", `kind: arc`, `kind: magic`, []string{"classes.yaml", "beginner", "magic"}},
		{"an attack with an unknown damage type", "classes.yaml", `damage: physical`, `damage: fire`, []string{"classes.yaml", "beginner", "fire"}},
		{"an auto-assign split that doesn't add up to the points a level gives", "classes.yaml", `auto_assign: {str: 3, vit: 2}`, `auto_assign: {str: 3, vit: 3}`, []string{"classes.yaml", "warrior", "auto_assign", "5"}},
		{"an auto-assign split with a negative stat", "classes.yaml", `auto_assign: {str: 3, vit: 2}`, `auto_assign: {str: 6, vit: -1}`, []string{"classes.yaml", "warrior", "auto_assign"}},
		{"a class renamed away from its bases", "classes.yaml", `id: bowman`, `id: archer`, []string{"stats.yaml", "bowman", "classes.yaml"}},
		{"a first class with a parent, so no root", "classes.yaml", "    name: Beginner\n", "    name: Beginner\n    parent: warrior\n", []string{"classes.yaml", "beginner", "root"}},
		{"a second root", "classes.yaml", "    parent: beginner\n    look: {model: rogue_hooded}", "    look: {model: rogue_hooded}", []string{"classes.yaml", "bowman", "root"}},
		{"a parent that isn't above it in the file", "classes.yaml", "    parent: beginner\n    look: {model: knight}", "    parent: bowman\n    look: {model: knight}", []string{"classes.yaml", "warrior", "bowman"}},
		{"a parent that doesn't exist", "classes.yaml", "    parent: beginner\n    look: {model: knight}", "    parent: paladin\n    look: {model: knight}", []string{"classes.yaml", "warrior", "paladin"}},
		{"a class without a description", "classes.yaml", "    description: \"Fights from afar.\"\n", "", []string{"classes.yaml", "class 3", "description"}},
		{"a root with a promotion", "classes.yaml", "    skills: [strike, second_wind]\n", "    skills: [strike, second_wind]\n    promotion: {trainer: hunter, level: 2, weapon: short_bow}\n", []string{"classes.yaml", "beginner", "promotion"}},
		{"a class without a promotion", "classes.yaml", "    promotion: {trainer: hunter, level: 2, weapon: short_bow}\n", "", []string{"classes.yaml", "bowman", "promotion"}},
		{"a promotion without a weapon", "classes.yaml", ", weapon: short_bow}", "}", []string{"classes.yaml", "bowman", "weapon"}},
		{"a promotion below level 1", "classes.yaml", "level: 2, weapon: short_bow", "level: 0, weapon: short_bow", []string{"classes.yaml", "bowman", "level"}},
		{"a promotion asking for a negative stat", "classes.yaml", "stats: {str: 20, vit: 10}", "stats: {str: -1}", []string{"classes.yaml", "warrior", "negative"}},
		{"a promotion asking for no points in a skill", "classes.yaml", "skills: {strike: 2}", "skills: {strike: 0}", []string{"classes.yaml", "warrior", "strike"}},
		{"a promotion trainer who isn't an NPC", "classes.yaml", "trainer: drillmaster,", "trainer: nobody,", []string{"classes.yaml", "warrior", "nobody"}},
		{"a promotion trainer who trains another class", "classes.yaml", "trainer: drillmaster,", "trainer: hunter,", []string{"classes.yaml", "warrior", "hunter"}},
		{"a trainer whose class names another trainer", "npcs.yaml", "    look: guide\n", "    look: guide\n    trainer: warrior\n", []string{"npcs.yaml", "guide", "drillmaster"}},
		{"a promotion asking for a skill the parent can't learn", "classes.yaml", "skills: {strike: 2}", "skills: {smash: 2}", []string{"classes.yaml", "warrior", "smash"}},
		{"a promotion asking for an unknown quest", "classes.yaml", "quests: [first_hunt]", "quests: [trial]", []string{"classes.yaml", "warrior", "trial", "quests.yaml"}},
		{"a promotion weapon of another class", "classes.yaml", "weapon: wooden_sword}", "weapon: short_bow}", []string{"classes.yaml", "warrior", "short_bow"}},
		{"a promotion weapon that isn't a weapon", "classes.yaml", "weapon: wooden_sword}", "weapon: leather_cap}", []string{"classes.yaml", "warrior", "leather_cap"}},
		{"a promotion weapon above the promotion's level", "items.yaml", "class: bowman, min_level: 1", "class: bowman, min_level: 3", []string{"classes.yaml", "bowman", "short_bow", "level 2"}},
		{"starting stats on a class a character is promoted to", "classes.yaml", "    auto_assign: {str: 3, vit: 2}\n", "    auto_assign: {str: 3, vit: 2}\n    base_stats: {str: 9}\n", []string{"classes.yaml", "warrior", "base_stats"}},
		{"starter items on a class a character is promoted to", "classes.yaml", "    auto_assign: {str: 3, vit: 2}\n", "    auto_assign: {str: 3, vit: 2}\n    starter_items: [{item: red_potion, quantity: 1}]\n", []string{"classes.yaml", "warrior", "starter_items"}},
		{"no starting stats on the root", "classes.yaml", "    base_stats: {str: 12, dex: 5, int: 4, agi: 5, vit: 10}\n", "", []string{"classes.yaml", "beginner", "base_stats"}},
		{"an auto-assign split on the root", "classes.yaml", "    base_stats: {str: 12", "    auto_assign: {str: 5}\n    base_stats: {str: 12", []string{"classes.yaml", "beginner", "auto_assign"}},
		{"a class a character is promoted to, with no split", "classes.yaml", "    auto_assign: {str: 3, vit: 2}\n", "", []string{"classes.yaml", "warrior", "auto_assign"}},
		{"a class without a look", "classes.yaml", "    look: {model: knight}\n", "", []string{"classes.yaml", "warrior", "look"}},
		{"a class tinted past RGB", "classes.yaml", "look: {model: knight}", "look: {model: knight, tint: 0x1000000}", []string{"classes.yaml", "warrior", "tint"}},
		{"a starter item that doesn't exist", "classes.yaml", `starter_items: [{item: wooden_sword, quantity: 1}]`, `starter_items: [{item: excalibur, quantity: 1}]`, []string{"classes.yaml", "beginner", "excalibur"}},
		{"a starter stack above the stack size", "classes.yaml", `starter_items: [{item: wooden_sword, quantity: 1}]`, `starter_items: [{item: wooden_sword, quantity: 2}]`, []string{"classes.yaml", "beginner", "wooden_sword"}},
		{"an item with an unknown tab", "items.yaml", `tab: etc`, `tab: misc`, []string{"items.yaml", "slime_gel", "misc"}},
		{"equipment without a slot", "items.yaml", `slot: weapon, `, ``, []string{"items.yaml", "wooden_sword", "slot"}},
		{"equipment naming a slot kind no equip slot has", "items.yaml", `slot: weapon, `, `slot: cape, `, []string{"items.yaml", "wooden_sword", "cape"}},
		{"no stars", "items.yaml", `tab: etc, stars: 2, `, `tab: etc, `, []string{"items.yaml", "slime_gel", "stars"}},
		{"stars above seven", "items.yaml", `stars: 2`, `stars: 8`, []string{"items.yaml", "slime_gel", "stars"}},
		{"no description", "items.yaml", `, description: "Sticky."`, ``, []string{"items.yaml", "slime_gel", "description"}},
		{"equipment without stats", "items.yaml", `, stats: {physical_attack: [10, 12], str: [0, 2]}`, ``, []string{"items.yaml", "wooden_sword", "stats"}},
		{"an unknown stat", "items.yaml", `str: [0, 2]`, `luck: [0, 2]`, []string{"items.yaml", "wooden_sword", "luck"}},
		{"a range whose minimum is above its maximum", "items.yaml", `physical_attack: [10, 12]`, `physical_attack: [12, 10]`, []string{"items.yaml", "wooden_sword", "physical_attack"}},
		{"a range below zero", "items.yaml", `str: [0, 2]`, `str: [-1, 2]`, []string{"items.yaml", "wooden_sword", "str"}},
		{"a range that only rolls zero", "items.yaml", `str: [0, 2]`, `str: [0, 0]`, []string{"items.yaml", "wooden_sword", "str"}},
		{"a range that isn't two numbers", "items.yaml", `physical_attack: [10, 12]`, `physical_attack: [10]`, []string{"items.yaml", "wooden_sword", "physical_attack"}},
		{"the old name of physical attack", "items.yaml", `physical_attack: [10, 12]`, `attack: [10, 12]`, []string{"items.yaml", "wooden_sword", "attack"}},
		{"a class on armor: only weapons keep one", "items.yaml", `slot: hat, `, `slot: hat, class: warrior, `, []string{"items.yaml", "leather_cap", "weapon"}},
		{"a requirement on a stat that isn't primary", "items.yaml", `requires: {str: 8, vit: 6}`, `requires: {str: 8, luk: 6}`, []string{"items.yaml", "leather_cap", "luk"}},
		{"a requirement on a derived stat", "items.yaml", `requires: {str: 8, vit: 6}`, `requires: {str: 8, max_hp: 6}`, []string{"items.yaml", "leather_cap", "max_hp"}},
		{"a requirement below one", "items.yaml", `requires: {str: 8, vit: 6}`, `requires: {str: 8, vit: 0}`, []string{"items.yaml", "leather_cap", "vit"}},
		{"requirements on a use item", "items.yaml", `heal_hp: 50, `, `heal_hp: 50, requires: {str: 1}, `, []string{"items.yaml", "red_potion", "requirements"}},
		{"stats on a use item", "items.yaml", `heal_hp: 50, `, `heal_hp: 50, stats: {max_hp: [1, 2]}, `, []string{"items.yaml", "red_potion", "stats"}},
		{"duplicate equip slots", "items.yaml", `id: ring_2`, `id: ring_1`, []string{"items.yaml", "ring_1", "duplicate"}},
		{"an equip slot without a kind", "items.yaml", `{id: hat, name: Hat, kind: hat}`, `{id: hat, name: Hat}`, []string{"items.yaml", "hat", "kind"}},
		{"an item restricted to an unknown class", "items.yaml", `class: warrior`, `class: paladin`, []string{"items.yaml", "wooden_sword", "paladin"}},
		{"an unknown key is rejected", "items.yaml", `tab: etc`, `tab: etc, colour: green`, []string{"items.yaml", "colour"}},
		{"movement constants must be positive", "movement.yaml", `gravity: 38`, `gravity: 0`, []string{"movement.yaml", "gravity"}},
		{"a casting_speed, gone since a skill leaves movement alone", "movement.yaml", "snap: 0.5\n", "snap: 0.5\ncasting_speed: 0.5\n", []string{"movement.yaml", "casting_speed"}},
		{"a sprint with no walk before it", "movement.yaml", "after: 2,", "after: 0,", []string{"movement.yaml", "sprint.after"}},
		{"a sprint no faster than the walk", "movement.yaml", "speed: 1.6}", "speed: 1}", []string{"movement.yaml", "sprint.speed"}},
		{"no active quest allowed", "quests.yaml", `max_active: 20`, `max_active: 0`, []string{"quests.yaml", "max_active"}},
		{"no chat line allowed", "chat.yaml", `line_max: 120`, `line_max: 0`, []string{"chat.yaml", "line_max"}},
		{"loot timings must be positive", "loot.yaml", `collect_delay: 1s`, `collect_delay: 0s`, []string{"loot.yaml", "collect_delay"}},
		{"a drop must fly before it is lost", "loot.yaml", `collect_delay: 1s`, `collect_delay: 60s`, []string{"loot.yaml", "drop_life"}},
		{"a full inventory must be asked again before the drop is lost", "loot.yaml", `room_check_every: 2s`, `room_check_every: 90s`, []string{"loot.yaml", "drop_life"}},
		{"a loot timing that isn't a duration", "loot.yaml", `drop_life: 60s`, `drop_life: a minute`, []string{"loot.yaml"}},
		{"an NPC placed nowhere", "npcs.yaml", "npcs:\n", "npcs:\n  - {id: ghost, name: Ghost, look: ghost}\n", []string{"ghost", "no map"}},
		{"duplicate NPC IDs", "npcs.yaml", `id: hunter`, `id: guide`, []string{"npcs.yaml", "guide", "duplicate"}},
		{"an NPC without a look", "npcs.yaml", "    look: hunter\n", "", []string{"npcs.yaml", "hunter", "look"}},
		{"an NPC with an empty line", "npcs.yaml", "    look: hunter\n", "    look: hunter\n    lines: [\"\"]\n", []string{"npcs.yaml", "hunter", "line"}},
		{"an NPC saying a line twice", "npcs.yaml", "    look: hunter\n", "    look: hunter\n    lines: [Hi., Hi.]\n", []string{"npcs.yaml", "hunter", "twice"}},
		{"skill points per level below one", "stats.yaml", `skill_points_per_level: 5`, `skill_points_per_level: 0`, []string{"stats.yaml", "skill_points_per_level"}},
		{"a class teaching a skill skills.yaml doesn't have", "classes.yaml", `skills: [strike, second_wind]`, `skills: [strike, second_wind, fireball]`, []string{"classes.yaml", "fireball"}},
		{"a skill no class teaches", "classes.yaml", `skills: [strike, second_wind]`, `skills: [strike]`, []string{"skills.yaml", "second_wind", "teaches"}},
		{"a skill two classes teach", "classes.yaml", "    auto_assign: {str: 3, vit: 2}\n", "    auto_assign: {str: 3, vit: 2}\n    skills: [strike]\n", []string{"classes.yaml", "strike", "beginner", "warrior"}},
		{"a cancel point past the end", "actions.yaml", "cancel: 0.85", "cancel: 1.2", []string{"actions.yaml", "cancel"}},
		{"actions.yaml without a ready point", "actions.yaml", "ready: 0.85\n", "", []string{"actions.yaml", "ready"}},
		{"a ready point past the end", "actions.yaml", "ready: 0.85", "ready: 1.2", []string{"actions.yaml", "ready"}},
		{"an early_press, gone since a press at any time goes at the cancel point", "actions.yaml", "cancel: 0.85\n", "cancel: 0.85\nearly_press: 100ms\n", []string{"actions.yaml", "early_press"}},
		{"an attack without a length", "classes.yaml", "radius: 1.875, angle: 120, height: 1.5, length: 700ms}", "radius: 1.875, angle: 120, height: 1.5}", []string{"classes.yaml", "beginner", "length"}},
		{"an attack keeping its old cooldown", "classes.yaml", "length: 700ms}", "cooldown: 700ms}", []string{"classes.yaml", "cooldown"}},
		{"an attack with a negative length", "classes.yaml", "length: 700ms}", "length: -700ms}", []string{"classes.yaml", "beginner", "positive"}},
		{"an attack whose hit share is above one", "classes.yaml", "hit: 0.25, cancel: 0.5", "hit: 1.5, cancel: 0.5", []string{"classes.yaml", "bowman", "shares"}},
		{"an attack whose hit lands after its cancel point", "classes.yaml", "hit: 0.25, cancel: 0.5", "hit: 0.75, cancel: 0.5", []string{"classes.yaml", "bowman", "cancel point"}},
		{"an attack ready before its hit", "classes.yaml", "ready: 0.6}", "ready: 0.2}", []string{"classes.yaml", "bowman", "ready point"}},
		{"an active skill without a length", "skills.yaml", "    length: 700ms\n", "", []string{"skills.yaml", "strike", "length"}},
		{"a stage whose hit lands after its cancel point", "skills.yaml", "hit: 0.3, cancel: 0.6", "hit: 0.7, cancel: 0.6", []string{"skills.yaml", "strike", "Crushing Blow", "cancel point"}},
		{"a stage whose cancel share is above one", "skills.yaml", "hit: 0.3, cancel: 0.6", "hit: 0.3, cancel: 1.6", []string{"skills.yaml", "strike", "Crushing Blow", "shares"}},
		{"a stage whose ready share is below none", "skills.yaml", "ready: 0.7", "ready: -0.1", []string{"skills.yaml", "strike", "Crushing Blow", "shares"}},
		{"a passive skill with a ready point", "skills.yaml", "    effect: bonus\n", "    effect: bonus\n    ready: 0.5\n", []string{"skills.yaml", "second_wind", "passive", "ready"}},
		{"a passive skill with a length", "skills.yaml", "    effect: bonus\n", "    effect: bonus\n    length: 1s\n", []string{"skills.yaml", "second_wind", "passive", "length"}},
		{"a passive stage made cancelable", "skills.yaml", `{at: 10, name: Deep Breath, description: "Faster still.",`, `{at: 10, name: Deep Breath, description: "Faster still.", cancelable: true,`, []string{"skills.yaml", "second_wind", "cancelable"}},
		{"a passive skill with a cooldown", "skills.yaml", "    effect: bonus\n", "    effect: bonus\n    cooldown: 2s\n", []string{"skills.yaml", "second_wind", "cooldown"}},
		{"an active skill with a passive effect", "skills.yaml", "    effect: damage\n", "    effect: bonus\n", []string{"skills.yaml", "strike", "effect"}},
		{"an active skill without a cost", "skills.yaml", "    energy: 2\n", "", []string{"skills.yaml", "strike", "energy"}},
		{"a skill without an icon", "skills.yaml", "    icon: lorc/energy-breath\n", "", []string{"skills.yaml", "second_wind", "icon"}},
		{"an icon that isn't author/name", "skills.yaml", "icon: lorc/sword-slice}", "icon: sword-slice}", []string{"skills.yaml", "strike", "Heavy Strike", "icon", "sword-slice"}},
		{"an icon naming its file", "skills.yaml", "icon: lorc/sword-wound", "icon: lorc/sword-wound.svg", []string{"skills.yaml", "strike", "icon", "sword-wound.svg"}},
		{"an icon climbing out of its folder", "skills.yaml", "icon: lorc/sword-wound", "icon: ../sword-wound", []string{"skills.yaml", "strike", "icon"}},
		{"an active skill costing more than a player holds", "skills.yaml", "    energy: 2\n", "    energy: 11\n", []string{"skills.yaml", "strike", "energy", "10"}},
		{"a stage costing less than nothing", "skills.yaml", "energy: 3, targets: 3", "energy: -1, targets: 3", []string{"skills.yaml", "strike", "Crushing Blow", "energy"}},
		{"a stage costing more than a player holds", "skills.yaml", "energy: 3, targets: 3", "energy: 12, targets: 3", []string{"skills.yaml", "strike", "Crushing Blow", "energy"}},
		{"a passive skill with a cost", "skills.yaml", "    effect: bonus\n", "    effect: bonus\n    energy: 2\n", []string{"skills.yaml", "second_wind", "energy"}},
		{"actions.yaml without energy", "actions.yaml", "energy: {max: 10, per_attack: 2, per_block: 2}\n", "", []string{"actions.yaml", "energy"}},
		{"actions.yaml without combat", "actions.yaml", "combat: 5s\n", "", []string{"actions.yaml", "combat"}},
		{"actions.yaml with no combat time", "actions.yaml", "combat: 5s\n", "combat: 0s\n", []string{"actions.yaml", "combat"}},
		{"an energy max below one", "actions.yaml", "max: 10, per_attack: 2", "max: 0, per_attack: 2", []string{"actions.yaml", "energy", "max"}},
		{"energy an attack of zero", "actions.yaml", "max: 10, per_attack: 2", "max: 10, per_attack: 0", []string{"actions.yaml", "per_attack"}},
		{"energy an attack above the max", "actions.yaml", "max: 10, per_attack: 2", "max: 10, per_attack: 11", []string{"actions.yaml", "per_attack"}},
		{"a heal shaped as an arc", "skills.yaml", "effect: damage\n    damage: physical\n", "effect: heal\n", []string{"skills.yaml", "strike", "area"}},
		{"a skill costing MP, which is gone", "skills.yaml", "    energy: 2\n", "    energy: 2\n    mp: {base: 4}\n", []string{"skills.yaml", "mp"}},
		{"a point giving max MP, which is gone", "stats.yaml", `  agi: {evasion: 1}`, `  agi: {evasion: 1, max_mp: 3}`, []string{"stats.yaml", "agi", "max_mp"}},
		{"a point giving MP regen, which is gone", "stats.yaml", `  agi: {evasion: 1}`, `  agi: {evasion: 1, mp_regen: 0.05}`, []string{"stats.yaml", "agi", "mp_regen"}},
		{"a class base for max MP, which is gone", "stats.yaml", "  bowman:  {max_hp:", "  bowman:  {max_mp: {base: 20}, max_hp:", []string{"stats.yaml", "bowman", "max_mp"}},
		{"gear rolling max MP, which is gone", "items.yaml", `stats: {physical_attack: [8, 10]}`, `stats: {physical_attack: [8, 10], max_mp: [5, 10]}`, []string{"items.yaml", "short_bow", "max_mp"}},
		{"gear rolling MP regen, which is gone", "items.yaml", `stats: {physical_attack: [8, 10]}`, `stats: {physical_attack: [8, 10], mp_regen: [5, 10]}`, []string{"items.yaml", "short_bow", "mp_regen"}},
		{"a potion restoring MP, which is gone", "items.yaml", `heal_hp: 50, `, `heal_hp: 50, heal_mp: 30, `, []string{"items.yaml", "heal_mp"}},
		{"a bonus to MP regen, which is gone", "skills.yaml", "bonus: {hp_regen: {base: 5, per_point: 1}}", "bonus: {mp_regen: {base: 5}}", []string{"skills.yaml", "second_wind", "mp_regen"}},
		{"a damage skill without power", "skills.yaml", "    power: {base: 1.5, per_point: 0.05}\n", "", []string{"skills.yaml", "strike", "power"}},
		{"a damage skill without a damage type", "skills.yaml", "    damage: physical\n    shape", "    shape", []string{"skills.yaml", "strike", "damage type"}},
		{"an arc with a range", "skills.yaml", "shape: {kind: arc, radius: 2.375, angle: 120, height: 1.75}", "shape: {kind: arc, radius: 2.375, angle: 120, height: 1.75, range: 9}", []string{"skills.yaml", "strike", "arc"}},
		{"an arc wider than a circle", "skills.yaml", "angle: 120, height: 1.75}\n    energy", "angle: 400, height: 1.75}\n    energy", []string{"skills.yaml", "strike", "angle"}},
		{"a shot without a width", "skills.yaml", "shape: {kind: arc, radius: 2.375, angle: 120, height: 1.75}", "shape: {kind: shot, range: 9}", []string{"skills.yaml", "strike", "shot", "width"}},
		{"an area with an angle", "skills.yaml", "shape: {kind: arc, radius: 2.375, angle: 120, height: 1.75}", "shape: {kind: area, radius: 2, angle: 90}", []string{"skills.yaml", "strike", "area"}},
		{"a basic attack that is an area", "classes.yaml", "{kind: arc, damage: physical, radius: 1.875, angle: 120, height: 1.5,", "{kind: area, damage: physical, radius: 1.875,", []string{"classes.yaml", "beginner", "arc or a shot"}},
		{"a class without a model", "classes.yaml", "look: {model: knight}", "look: {}", []string{"classes.yaml", "warrior", "model"}},
		{"mobs.yaml without a leash", "mobs.yaml", "leash: 16\n", "", []string{"mobs.yaml", "leash"}},
		{"a leash of zero", "mobs.yaml", "leash: 16", "leash: 0", []string{"mobs.yaml", "leash"}},
		{"actions.yaml with the auto-aim's cone", "actions.yaml", "ready: 0.85\n", "ready: 0.85\naim_cone: 70\n", []string{"actions.yaml", "aim_cone"}},
		{"actions.yaml with the lock's old range", "actions.yaml", "ready: 0.85\n", "ready: 0.85\nlock_range: 12\n", []string{"actions.yaml", "lock_range"}},
		{"a map with no place on the world chart", "world.yaml", "  field: [60, 50]\n", "", []string{"world.yaml", "field", "no place"}},
		{"a place for a pvp map", "world.yaml", "  field: [60, 50]\n", "  field: [60, 50]\n  pit: [10, 10]\n", []string{"world.yaml", "pit", "pvp"}},
		{"pvp.yaml without modes", "pvp.yaml", "  duel: {name: 1v1, team: 1, party: 1, map: pit, rated: true, rounds: {best_of: 3, most: 5, length: 60s}}\n", "", []string{"pvp.yaml", "no modes"}},
		{"a mode on a hunting map", "pvp.yaml", "map: pit", "map: field", []string{"pvp.yaml", "duel", "field", "no pvp map"}},
		{"a mode's team past its map's spots", "pvp.yaml", "team: 1, party: 1", "team: 2, party: 1", []string{"pvp.yaml", "duel", "fewer than 2 spots"}},
		{"a party past the team", "pvp.yaml", "team: 1, party: 1", "team: 1, party: 2", []string{"pvp.yaml", "duel", "party"}},
		{"an even best_of", "pvp.yaml", "best_of: 3", "best_of: 2", []string{"pvp.yaml", "duel", "odd best_of"}},
		{"most rounds under best_of", "pvp.yaml", "most: 5", "most: 2", []string{"pvp.yaml", "duel", "most"}},
		{"an unrated mode with no rating to read", "pvp.yaml", "rated: true", "rated: false", []string{"pvp.yaml", "duel", "rating_from"}},
		{"an offer that never lapses", "pvp.yaml", "offer: 10s", "offer: 0s", []string{"pvp.yaml", "offer"}},
		{"a window whose most is under its start", "pvp.yaml", "most: 8}", "most: 1}", []string{"pvp.yaml", "window"}},
		{"no countdown", "pvp.yaml", "countdown: 3s", "countdown: 0s", []string{"pvp.yaml", "countdown"}},
		{"a kit past the level cap", "pvp.yaml", "kit: {level: 4}", "kit: {level: 5}", []string{"pvp.yaml", "kit", "cap 4"}},
		{"a hit scaled to nothing", "pvp.yaml", "damage: 1.0}", "damage: 0}", []string{"pvp.yaml", "scale"}},
		{"a rating with no skill gap", "pvp.yaml", "beta: 4.167", "beta: 0", []string{"pvp.yaml", "rating", "beta"}},
		{"ranks not from 0", "pvp.yaml", "{id: bronze, from: 0}", "{id: bronze, from: 100}", []string{"pvp.yaml", "bronze", "rise from 0"}},
		{"ranks that don't rise", "pvp.yaml", "{id: silver, from: 1200}", "{id: silver, from: 0}", []string{"pvp.yaml", "silver", "rise"}},
		{"tiers whose mu doesn't rise", "pvp.yaml", "{id: adept, mu: 20,", "{id: adept, mu: 15,", []string{"pvp.yaml", "adept", "mu must rise"}},
		{"a tier that defends past always", "pvp.yaml", "defend: 0.35", "defend: 1.5", []string{"pvp.yaml", "adept", "defend"}},
		{"too few names", "pvp.yaml", " N30]", "]", []string{"pvp.yaml", "29 names"}},
		{"a name twice", "pvp.yaml", " N30]", " N29]", []string{"pvp.yaml", "N29"}},
		{"a promoted class with no build", "pvp.yaml", "    bowman:\n      - {bar: [strike, '', '', '', '', '', '', ''], traits: {}, order: [strike]}\n", "", []string{"pvp.yaml", "bowman", "no build"}},
		{"builds for the Beginner", "pvp.yaml", "    bowman:\n", "    beginner:\n      - {bar: [strike, '', '', '', '', '', '', ''], traits: {}, order: [strike]}\n    bowman:\n", []string{"pvp.yaml", "beginner", "no promoted class"}},
		{"a build with seven keys", "pvp.yaml", "    warrior:\n      - {bar: [strike, '', '', '', '', '', '', '']", "    warrior:\n      - {bar: [strike, '', '', '', '', '', '']", []string{"pvp.yaml", "warrior", "7 keys"}},
		{"a build with a passive", "pvp.yaml", "    warrior:\n      - {bar: [strike, '',", "    warrior:\n      - {bar: [strike, second_wind,", []string{"pvp.yaml", "warrior", "second_wind", "no active skill"}},
		{"a build with a skill twice", "pvp.yaml", "    warrior:\n      - {bar: [strike, '',", "    warrior:\n      - {bar: [strike, strike,", []string{"pvp.yaml", "warrior", "another key"}},
		{"a trait for no skill on the bar", "pvp.yaml", "    warrior:\n      - {bar: [strike, '', '', '', '', '', '', ''], traits: {}", "    warrior:\n      - {bar: [strike, '', '', '', '', '', '', ''], traits: {second_wind: x}", []string{"pvp.yaml", "warrior", "second_wind"}},
		{"an order with a skill off the bar", "pvp.yaml", "    warrior:\n      - {bar: [strike, '', '', '', '', '', '', ''], traits: {}, order: [strike]}", "    warrior:\n      - {bar: ['', '', '', '', '', '', '', ''], traits: {}, order: [strike]}", []string{"pvp.yaml", "warrior", "not on a key"}},
		{"a place for no map", "world.yaml", "  field: [60, 50]\n", "  field: [60, 50]\n  marsh: [10, 10]\n", []string{"world.yaml", "marsh", "no map"}},
		{"a map placed twice", "world.yaml", "  field: [60, 50]\n", "  field: [60, 50]\n  town: [20, 20]\n", []string{"world.yaml", "town"}},
		{"two maps on the same spot", "world.yaml", "field: [60, 50]", "field: [44, 53]", []string{"world.yaml", "field", "town", "closer"}},
		{"a place off the chart", "world.yaml", "field: [60, 50]", "field: [60, 101]", []string{"world.yaml", "field", "chart"}},
		{"world.yaml without a teleport channel", "world.yaml", "teleport:\n  channel: 2500ms\n", "", []string{"world.yaml", "teleport.channel"}},
		{"a teleport channel of zero", "world.yaml", "channel: 2500ms", "channel: 0s", []string{"world.yaml", "teleport.channel", "over 0"}},
		{"a teleport channel below zero", "world.yaml", "channel: 2500ms", "channel: -1s", []string{"world.yaml", "teleport.channel"}},
		{"a teleport channel over 10 s", "world.yaml", "channel: 2500ms", "channel: 10001ms", []string{"world.yaml", "teleport.channel", "at most 10s"}},
		{"a teleport channel without a unit", "world.yaml", "channel: 2500ms", "channel: 3", []string{"world.yaml"}},
		{"a shape of an unknown kind", "skills.yaml", "shape: {kind: arc, radius: 2.375, angle: 120, height: 1.75}", "shape: {kind: cone, radius: 2.375, angle: 120, height: 1.75}", []string{"skills.yaml", "strike", "cone"}},
		{"stages whose totals don't rise", "skills.yaml", "{at: 25, name: Crushing Blow", "{at: 10, name: Crushing Blow", []string{"skills.yaml", "strike", "Crushing Blow", "rise"}},
		{"a first stage above 0 points", "skills.yaml", "{at: 0, name: Strike,", "{at: 1, name: Strike,", []string{"skills.yaml", "strike", "0 points"}},
		{"a stage without a description", "skills.yaml", `description: "A harder swing."`, `description: ""`, []string{"skills.yaml", "strike", "description"}},
		{"a stage giving targets to a passive", "skills.yaml", `{at: 10, name: Deep Breath, description: "Faster still.",`, `{at: 10, name: Deep Breath, description: "Faster still.", targets: 2,`, []string{"skills.yaml", "second_wind", "Deep Breath", "targets"}},
		{"a bonus to an unknown stat", "skills.yaml", "bonus: {hp_regen: {base: 5, per_point: 1}}", "bonus: {luck: {base: 5}}", []string{"skills.yaml", "second_wind", "luck"}},
		{"a bonus that takes away", "skills.yaml", "bonus: {hp_regen: {base: 5, per_point: 1}}", "bonus: {hp_regen: {base: -5}}", []string{"skills.yaml", "second_wind", "takes away"}},
		{"a passive without a bonus", "skills.yaml", "    bonus: {hp_regen: {base: 5, per_point: 1}}\n", "", []string{"skills.yaml", "second_wind", "bonus"}},
		{"a trainer of a class that doesn't exist", "npcs.yaml", `trainer: bowman`, `trainer: paladin`, []string{"npcs.yaml", "hunter", "paladin"}},
		{"a trainer of the root, which nobody is promoted to", "npcs.yaml", `trainer: bowman`, `trainer: beginner`, []string{"npcs.yaml", "hunter", "beginner"}},
		{"a talk reach of zero", "npcs.yaml", `talk_reach: 1.875`, `talk_reach: 0`, []string{"npcs.yaml", "talk_reach"}},
		{"a quest its giver doesn't list under gives", "npcs.yaml", `gives: [first_hunt, gel_for_me]`, `gives: [first_hunt]`, []string{"quests.yaml", "gel_for_me", "guide", "gives"}},
		{"a quest its turn-in NPC doesn't list under takes", "npcs.yaml", `takes: [gel_for_me, warrior_drill]`, `takes: [warrior_drill]`, []string{"quests.yaml", "gel_for_me", "hunter", "takes"}},
		{"an NPC giving a quest another NPC gives", "npcs.yaml", `gives: [warrior_drill]`, `gives: [warrior_drill, first_hunt]`, []string{"npcs.yaml", "hunter", "first_hunt"}},
		{"an NPC taking a quest handed in elsewhere", "npcs.yaml", `takes: [first_hunt]`, `takes: [first_hunt, warrior_drill]`, []string{"npcs.yaml", "guide", "warrior_drill"}},
		{"an NPC giving a quest that doesn't exist", "npcs.yaml", `gives: [warrior_drill]`, `gives: [warrior_drill, dragon_hunt]`, []string{"npcs.yaml", "hunter", "dragon_hunt"}},
		{"a quest given by an unknown NPC", "quests.yaml", `giver: hunter`, `giver: mayor`, []string{"quests.yaml", "warrior_drill", "mayor"}},
		{"a quest handed in to an unknown NPC", "quests.yaml", `turn_in: hunter`, `turn_in: mayor`, []string{"quests.yaml", "gel_for_me", "mayor"}},
		{"a kill of an unknown mob", "quests.yaml", `{kill: green_slime, count: 3}`, `{kill: dragon, count: 3}`, []string{"quests.yaml", "first_hunt", "dragon"}},
		{"a collect of an unknown item", "quests.yaml", `{collect: slime_gel, count: 4}`, `{collect: dragon_scale, count: 4}`, []string{"quests.yaml", "gel_for_me", "dragon_scale"}},
		{"a talk to an unknown NPC", "quests.yaml", `{talk: hunter}`, `{talk: mayor}`, []string{"quests.yaml", "gel_for_me", "mayor"}},
		{"a reward of an unknown item", "quests.yaml", `{item: red_potion, quantity: 5}`, `{item: elixir, quantity: 5}`, []string{"quests.yaml", "first_hunt", "elixir"}},
		{"a quest for an unknown class", "quests.yaml", `class: warrior`, `class: paladin`, []string{"quests.yaml", "warrior_drill", "paladin"}},
		{"a quest requiring an unknown quest", "quests.yaml", `quests: [first_hunt]`, `quests: [lost_quest]`, []string{"quests.yaml", "gel_for_me", "lost_quest"}},
		{"quests requiring each other", "quests.yaml", "    turn_in: guide\n    objectives:", "    turn_in: guide\n    quests: [gel_for_me]\n    objectives:", []string{"quests.yaml", "cycle", "first_hunt", "gel_for_me"}},
		{"a quest requiring itself", "quests.yaml", `class: warrior`, "class: warrior\n    quests: [warrior_drill]", []string{"quests.yaml", "cycle", "warrior_drill"}},
		{"a kill count below one", "quests.yaml", `{kill: green_slime, count: 3}`, `{kill: green_slime, count: 0}`, []string{"quests.yaml", "first_hunt", "count"}},
		{"a collect without a count", "quests.yaml", `{collect: slime_gel, count: 4}`, `{collect: slime_gel}`, []string{"quests.yaml", "gel_for_me", "count"}},
		{"a talk with a count", "quests.yaml", `{talk: hunter}`, `{talk: hunter, count: 2}`, []string{"quests.yaml", "gel_for_me", "count"}},
		{"an objective of two kinds", "quests.yaml", `{kill: green_slime, count: 3}`, `{kill: green_slime, collect: slime_gel, count: 3}`, []string{"quests.yaml", "first_hunt", "exactly one"}},
		{"an objective of no kind", "quests.yaml", `{kill: green_slime, count: 3}`, `{count: 3}`, []string{"quests.yaml", "first_hunt", "exactly one"}},
		{"a level objective past the cap", "quests.yaml", `{level: 2}`, `{level: 5}`, []string{"quests.yaml", "first_hunt", "level"}},
		{"a quest without objectives", "quests.yaml", "    objectives:\n      - {kill: green_slime, count: 1}\n", "", []string{"quests.yaml", "warrior_drill", "objective"}},
		{"a quest level past the cap", "quests.yaml", "level: 2\n    quests:", "level: 5\n    quests:", []string{"quests.yaml", "gel_for_me", "level"}},
		{"an equipment reward with a quantity above one", "quests.yaml", `{item: leather_cap, quantity: 1}`, `{item: leather_cap, quantity: 2}`, []string{"quests.yaml", "gel_for_me", "leather_cap", "quantity 1"}},
		{"a reward above its stack size", "quests.yaml", `{item: red_potion, quantity: 5}`, `{item: red_potion, quantity: 101}`, []string{"quests.yaml", "first_hunt", "red_potion"}},
		{"negative reward EXP", "quests.yaml", `exp: 10`, `exp: -10`, []string{"quests.yaml", "first_hunt", "exp"}},
		{"duplicate quest IDs", "quests.yaml", `id: warrior_drill`, `id: first_hunt`, []string{"quests.yaml", "first_hunt", "duplicate"}},
		{"an unknown key in a quest", "quests.yaml", `repeatable: true`, `repeat: true`, []string{"quests.yaml", "repeat"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := fixture()
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

func TestRejectionsNeedingTheirOwnMaps(t *testing.T) {
	t.Run("a boss no map places", func(t *testing.T) {
		t.Parallel()
		files := fixture()
		files["mobs.yaml"] = strings.Replace(files["mobs.yaml"], "  - id: slime_lord\n", "  - {id: slime_king, name: Slime King, boss: true, level: 5, hp: 900, attack: 12, damage: physical, physical_defense: 3, magic_defense: 2, accuracy: 8, evasion: 2, exp: 200, speed: 0.9375, radius: 1.5, height: 2.625, behavior: patrol, attacks: [{use: hop}], look: {model: green_blob}}\n  - id: slime_lord\n", 1)
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "slime_king") || !strings.Contains(err.Error(), "no map") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("two start towns", func(t *testing.T) {
		t.Parallel()
		files := fixture()
		files["maps/village.yaml"] = strings.NewReplacer("name: to_field", "name: to_field_2", "  - {id: guide, at: [6, 6], yaw: 180}\n", "").Replace(files["maps/town.yaml"])
		files["maps/village.yaml"] = strings.Replace(files["maps/village.yaml"], "npcs:\n", "", 1)
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "village") || !strings.Contains(err.Error(), "start") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a map file that isn't YAML", func(t *testing.T) {
		t.Parallel()
		files := fixture()
		files["maps/field.yaml"] = "{not yaml"
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "field.yaml") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a missing data file", func(t *testing.T) {
		t.Parallel()
		files := fixture()
		delete(files, "mobs.yaml")
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "mobs.yaml") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestEliteMobs(t *testing.T) {
	king := "\n  - {id: king_slime, name: King Slime, level: 2, elite: true, hp: 90, attack: 8, damage: physical, physical_defense: 2, magic_defense: 1, accuracy: 8, evasion: 2, exp: 12, speed: 0.9375, radius: 0.75, height: 1.25, behavior: patrol, attacks: [{use: hop}], look: {model: green_blob}}\n"
	slimes := "  - {name: slimes, at: [12, 6], radius: 4, mob: green_slime, max: 4, respawn: 10s}\n"
	kingSpawn := "  - {name: king, at: [12, 6], radius: 4, mob: king_slime, max: 1, respawn: 30s}\n"
	t.Run("an elite beside its map's normal mob loads, marked elite", func(t *testing.T) {
		files := fixture()
		files["mobs.yaml"] = strings.TrimRight(files["mobs.yaml"], "\n") + king
		files["maps/field.yaml"] = strings.Replace(files["maps/field.yaml"], slimes, slimes+kingSpawn, 1)
		d, err := load.Dir(write(t, files))
		if err != nil {
			t.Fatal(err)
		}
		if k, _ := d.Mob("king_slime"); !k.Elite {
			t.Fatalf("king_slime = %+v, want elite", k)
		}
		if s, _ := d.Mob("green_slime"); s.Elite {
			t.Fatal("green_slime is elite")
		}
	})
	t.Run("an elite alone on its map", func(t *testing.T) {
		files := fixture()
		files["mobs.yaml"] = strings.Replace(files["mobs.yaml"], "    level: 1\n", "    level: 1\n    elite: true\n", 1)
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "field.yaml") || !strings.Contains(err.Error(), "green_slime") || !strings.Contains(err.Error(), "normal") {
			t.Fatalf("err = %v", err)
		}
	})
}

// realItems are the item IDs data/items.yaml lists.
func realItems(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../../../data/items.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Items []struct {
			ID string `yaml:"id"`
		} `yaml:"items"`
	}
	if err := yaml.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(f.Items))
	for i, it := range f.Items {
		ids[i] = it.ID
	}
	return ids
}

// reaches reports whether the portals lead from map from to map to, through any maps between.
func reaches(d *gamedata.Data, from, to string) bool {
	seen := map[string]bool{from: true}
	next := []string{from}
	for len(next) > 0 {
		id := next[0]
		next = next[1:]
		if id == to {
			return true
		}
		m, _ := d.Map(id)
		for _, p := range m.Portals {
			if !seen[p.TargetMap] {
				seen[p.TargetMap] = true
				next = append(next, p.TargetMap)
			}
		}
	}
	return false
}

func allLoaded(d *gamedata.Data, ids []string) bool {
	for _, id := range ids {
		if _, ok := d.Item(id); !ok {
			return false
		}
	}
	return true
}

func mobDamage(d *gamedata.Data, id string) gamedata.DamageType {
	m, _ := d.Mob(id)
	return m.Damage
}

// C21: an attack marked unblockable in the table is so on the boss using it, and a use may say otherwise; the rest stay blockable.
func TestABosssAttackMayBeUnblockable(t *testing.T) {
	files := fixture()
	before := files["mobs.yaml"]
	files["mobs.yaml"] = strings.Replace(before, "cooldown: 5s, power: 1.5}", "cooldown: 5s, power: 1.5, unblockable: true}", 1)
	if files["mobs.yaml"] == before {
		t.Fatal("the slam's table entry not found")
	}
	d, err := load.Dir(write(t, files))
	if err != nil {
		t.Fatal(err)
	}
	lord, _ := d.Mob("slime_lord")
	if got := []bool{lord.Attacks[0].Unblockable, lord.Attacks[1].Unblockable}; !slices.Equal(got, []bool{false, true}) {
		t.Errorf("the lord's hop and slam unblockable %v; want only the slam", got)
	}
	files["mobs.yaml"] = strings.Replace(files["mobs.yaml"], "{use: slam}", "{use: slam, unblockable: false}", 1)
	if d, err = load.Dir(write(t, files)); err != nil {
		t.Fatal(err)
	}
	if lord, _ = d.Mob("slime_lord"); lord.Attacks[1].Unblockable {
		t.Error("a use saying unblockable: false kept the table's true")
	}
}

// The real content is checked on every run, so a broken reference fails CI.
func TestRealDataLoads(t *testing.T) {
	d, err := load.Dir("../../../../data")
	if err != nil {
		t.Fatal(err)
	}
	items := realItems(t)
	tests := []struct {
		name string
		ok   bool
	}{
		{"level cap 30", d.LevelCap() == 30},
		{"new characters start in a town", d.StartTown() != nil && d.StartTown().Kind == gamedata.Town},
		{"every listed item loaded", len(items) > 0 && allLoaded(d, items)},
		{"the expected movement constants", d.Movement() == wantMovement},
		{"cave bats deal magic damage, the forest's mobs physical", mobDamage(d, "cave_bat") == gamedata.Magic && mobDamage(d, "green_slime") == gamedata.Physical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatal("check failed")
			}
		})
	}
	t.Run("skills cost energy as C17's table gives it, the first ones 2 for the quick single-target ones and the Magician's, 3 for the rest, out of 10, and an attack gives 2, a block 2 (C21)", func(t *testing.T) {
		if got := d.Energy(); got != (gamedata.Energy{Max: 10, PerAttack: 2, PerBlock: 2}) {
			t.Fatalf("energy %+v", got)
		}
		costs := map[string]int{
			"strike": 2, "throw": 2, "double_stab": 2, "power_shot": 2, "spark": 2, "fire_arrow": 2,
			"slash_blast": 3, "heal": 3, "iron_body": 3, "haste": 3, "focus": 3,
			"lunge": 2, "roll_back": 2,
			"charge": 3, "ground_slam": 3, "war_cry": 3, "taunt": 2,
			"shadow_step": 3, "smoke_bomb": 2, "flurry": 3, "poison_blade": 3,
			"disengage": 2, "pinning_shot": 2, "arrow_rain": 3, "eagle_eye": 3,
			"blink": 2, "frost_nova": 3, "barrier": 3, "meteor": 3,
		}
		for _, s := range d.Skills() {
			for _, st := range s.Stages {
				if want := costs[s.ID]; st.Energy != want {
					t.Errorf("%s's %s costs %d, want %d", s.ID, st.Name, st.Energy, want)
				}
			}
		}
	})
	t.Run("only a break-free's leap passes through mobs; every other dash and leap, the escapes among them, stops at the first body it meets", func(t *testing.T) {
		var through []string
		for _, s := range d.Skills() {
			for _, st := range s.Stages {
				m := st.Move
				if m.Kind == gamedata.BlinkMove || m.Kind == "" {
					continue
				}
				if m.Through {
					through = append(through, s.ID)
				}
				if m.Through != s.BreaksControl || m.Stop == m.Through {
					t.Errorf("%s's %s: through %v, stop %v, a break-free %v", s.ID, st.Name, m.Through, m.Stop, s.BreaksControl)
				}
			}
		}
		if !slices.Equal(through, []string{"vanish", "recoil"}) {
			t.Errorf("through mobs: %v; want Vanish's and Recoil's leaps", through)
		}
	})
	t.Run("C21: every field boss's slam is unblockable, its family attack blockable, and no other mob's attack unblockable", func(t *testing.T) {
		bosses := 0
		for _, m := range d.Mobs() {
			for _, a := range m.Attacks {
				if want := m.Boss && a.Name == "slam"; a.Unblockable != want {
					t.Errorf("%s's %s unblockable %v; want %v", m.ID, a.Name, a.Unblockable, want)
				}
			}
			if m.Boss {
				bosses++
			}
		}
		if bosses == 0 {
			t.Error("no field boss in the data")
		}
	})
	t.Run("attacks are cut at actions.yaml's 0.3 and look ready at 0.85; every active skill lasts 1 s, but C17's escapes, Taunt, the break-frees (0.4 s) and C21's blocks (their guard's time, and Deflect's leap after it), its look filling it, the cancelable ones cut at 0.45", func(t *testing.T) {
		for _, c := range d.Classes() {
			if c.Attack.Cancel != 0.3 || c.Attack.Ready != 0.85 {
				t.Errorf("%s's attack cancels at %v, ready at %v", c.ID, c.Attack.Cancel, c.Attack.Ready)
			}
		}
		short := map[string]time.Duration{"roll_back": 600 * time.Millisecond, "taunt": 600 * time.Millisecond, "smoke_bomb": 600 * time.Millisecond, "disengage": 600 * time.Millisecond, "blink": 400 * time.Millisecond}
		for id, block := range blocks {
			short[id] = block.length
		}
		for _, id := range []string{"break_free", "unyielding", "vanish", "recoil", "arcane_release"} {
			short[id] = 400 * time.Millisecond
			if s, _ := d.Skill(id); !s.BreaksControl || !s.Granted || s.Stages[0].Energy != 0 || s.Stages[0].Cooldown != 32*time.Second {
				t.Errorf("%s: breaks control %v, granted %v, %d energy, %v cooldown; want a break-free, granted, free, 32 s", id, s.BreaksControl, s.Granted, s.Stages[0].Energy, s.Stages[0].Cooldown)
			}
		}
		var uncut []string
		for _, s := range d.Skills() {
			if s.Kind != gamedata.Active {
				continue
			}
			if !s.Stages[0].Cancelable {
				uncut = append(uncut, s.ID)
			}
			length := cmp.Or(short[s.ID], time.Second)
			for _, st := range s.Stages {
				if st.Length != length || st.Ready != 1 || st.Cancelable && st.Cancel != 0.45 {
					t.Errorf("%s lasts %v, cancels at %v, ready at %v; want %v, 0.45 if cancelable, and 1", st.Name, st.Length, st.Cancel, st.Ready, length)
				}
			}
		}
		if want := []string{"iron_body", "haste", "focus", "heal", "roll_back", "break_free", "guard", "ground_slam", "war_cry", "taunt", "unyielding", "bastion", "smoke_bomb", "poison_blade", "vanish", "parry", "disengage", "arrow_rain", "eagle_eye", "recoil", "deflect", "blink", "frost_nova", "barrier", "meteor", "arcane_release", "ward"}; !slices.Equal(uncut, want) {
			t.Errorf("the skills nothing cuts are %v, want %v", uncut, want)
		}
	})
	t.Run("C21: each class's block is section 3's table, granted with its class, free, uncut, and its counter follows 1.5 s after a block", func(t *testing.T) {
		if b := d.Block(); b.Arc != 180 {
			t.Errorf("a block covers %v°, want 180", b.Arc)
		}
		for id, want := range blocks {
			s, _ := d.Skill(id)
			st := s.Stages[0]
			if s.Effect != gamedata.BlockEffect || !s.Granted || s.Class != want.class || len(s.Stages) != 1 || st.Block != want.guard || st.Length != want.length || st.Cooldown != want.cooldown || st.Energy != 0 || st.Cancelable || st.Move.Distance != want.leap {
				t.Errorf("%s: %+v, want a %v block of %s's in %v, %v back, leaping %v m", id, s, want.guard, want.class, want.length, want.cooldown, want.leap)
			}
			if want.leap > 0 && st.Move.Delay != st.Block {
				t.Errorf("%s leaps %v after the press, want once its %v guard is down", id, st.Move.Delay, st.Block)
			}
			c, _ := d.Class(want.class)
			if f, ok := c.FollowUp(id, want.counter); !ok || f.When.Blocked != 1500*time.Millisecond || len(c.Chains[slices.IndexFunc(c.Chains, func(ch gamedata.Chain) bool { return ch.Root == id })].Follow) != 1 {
				t.Errorf("%s: %s's chain from it %+v, want %s alone, 1.5 s after a block", id, want.class, c.Chains, want.counter)
			}
		}
		var found []string
		for _, s := range d.Skills() {
			if s.Effect == gamedata.BlockEffect {
				found = append(found, s.ID)
			}
		}
		if len(found) != len(blocks) {
			t.Errorf("block skills %v, want the five", found)
		}
	})
	t.Run("every elite and boss has super armor, and no normal mob", func(t *testing.T) {
		for _, m := range d.Mobs() {
			if m.SuperArmor != (m.Elite || m.Boss) {
				t.Errorf("%s: super armor %v, elite %v, boss %v", m.ID, m.SuperArmor, m.Elite, m.Boss)
			}
		}
	})
	t.Run("every character starts a Beginner, promoted to one of four classes", func(t *testing.T) {
		var children []kernel.Class
		for _, c := range d.Classes()[1:] {
			if c.Parent != d.StartClass().ID {
				t.Errorf("%s: parent %q, want the beginner", c.ID, c.Parent)
			}
			children = append(children, c.ID)
		}
		if d.StartClass().ID != "beginner" || !slices.Equal(children, []kernel.Class{"warrior", "thief", "bowman", "magician"}) {
			t.Fatalf("root %s, children %v", d.StartClass().ID, children)
		}
	})
	// The product page's starting conditions; the trainer hands over the class's level 1 weapon.
	t.Run("each first class is promoted to at level 10 with its stat at 30, 10 points in a Beginner skill and its trial, and teaches its first two skills, C17's five, its break-free and its block", func(t *testing.T) {
		want := map[kernel.Class]gamedata.Promotion{
			"warrior":  {Trainer: "warrior_trainer", Level: 10, Stats: gamedata.Stats{STR: 30}, Skills: map[string]int{"strike": 10}, Quests: []string{"warriors_trial"}, Weapon: "wooden_sword"},
			"thief":    {Trainer: "thief_trainer", Level: 10, Stats: gamedata.Stats{AGI: 30}, Skills: map[string]int{"strike": 10}, Quests: []string{"thiefs_trial"}, Weapon: "rusty_dagger"},
			"bowman":   {Trainer: "bowman_trainer", Level: 10, Stats: gamedata.Stats{DEX: 30}, Skills: map[string]int{"throw": 10}, Quests: []string{"bowmans_trial"}, Weapon: "short_bow"},
			"magician": {Trainer: "magician_trainer", Level: 10, Stats: gamedata.Stats{INT: 30}, Skills: map[string]int{"spark": 10}, Quests: []string{"magicians_trial"}, Weapon: "oak_wand"},
		}
		for _, c := range d.Classes()[1:] {
			if !reflect.DeepEqual(c.Promotion, want[c.ID]) || len(c.Skills) != 9 {
				t.Errorf("%s: promotion %+v, skills %v", c.ID, c.Promotion, c.Skills)
			}
		}
	})
	t.Run("the Beginner teaches Strike, Throw, Spark and Second Wind, each evolving at 10 points, the actives again at 25; every later skill evolves once, at 10, but a granted one, which takes no points", func(t *testing.T) {
		first := []string{"strike", "throw", "spark", "second_wind"}
		if got := d.StartClass().Skills; !slices.Equal(got, append(first, "lunge", "roll_back", "break_free", "guard")) {
			t.Fatalf("beginner skills %v", got)
		}
		for _, s := range d.Skills() {
			want := []int{0, 10}
			if slices.Contains(first, s.ID) && s.Kind == gamedata.Active {
				want = append(want, 25)
			}
			if s.ID == "iron_body" || s.ID == "haste" || s.ID == "focus" || s.ID == "heal" || s.Granted {
				want = want[:1] // the first classes' buffs and heal don't evolve yet
			}
			id := s.ID
			var got []int
			for _, st := range s.Stages {
				got = append(got, st.At)
			}
			if !slices.Equal(got, want) {
				t.Errorf("%s evolves at %v, want %v", id, got, want)
			}
		}
	})
	t.Run("the EXP curve is round(20 × level^1.7)", func(t *testing.T) {
		for level := 1; level < d.LevelCap(); level++ {
			want := int64(math.Round(20 * math.Pow(float64(level), 1.7)))
			got, ok := d.ExpToNext(level)
			if !ok || got != want {
				t.Fatalf("level %d: %d, want %d", level, got, want)
			}
		}
	})
	// Systems → Items, content: in each level band, gear for every armor and accessory kind and a weapon per class.
	t.Run("each level band has gear for every slot kind and a weapon per class", func(t *testing.T) {
		in := func(lo, hi int, ok func(gamedata.Item) bool) bool {
			for _, id := range items {
				if it, _ := d.Item(id); it.Tab == gamedata.Equip && it.MinLevel >= lo && it.MinLevel <= hi && ok(it) {
					return true
				}
			}
			return false
		}
		for _, band := range [][2]int{{1, 9}, {10, 19}, {20, d.LevelCap()}} {
			for _, s := range d.EquipSlots() {
				if s.Kind != "weapon" && !in(band[0], band[1], func(it gamedata.Item) bool { return it.Slot == s.Kind }) {
					t.Errorf("levels %d to %d: nothing for the %s slot", band[0], band[1], s.ID)
				}
			}
			for _, c := range d.Classes()[1:] {
				if !in(band[0], band[1], func(it gamedata.Item) bool { return it.Slot == "weapon" && it.Class == c.ID }) {
					t.Errorf("levels %d to %d: no weapon for the %s", band[0], band[1], c.ID)
				}
			}
		}
	})
	// A class's level 1 weapon is its trainer's gift at promotion.
	t.Run("every piece of equipment drops from a mob, or is a starter or a class's first weapon", func(t *testing.T) {
		got := map[string]bool{}
		for _, m := range d.Mobs() {
			for _, dr := range m.Drops {
				got[dr.Item] = true
			}
		}
		for _, c := range d.Classes() {
			for _, g := range c.StarterItems {
				got[g.Item] = true
			}
			got[c.Promotion.Weapon] = true
		}
		for _, id := range items {
			it, _ := d.Item(id)
			if it.Tab == gamedata.Equip && !got[id] {
				t.Errorf("%s: nobody can get it", id)
			}
		}
	})
	t.Run("every map can be walked to from the start town, and every hunting map back to its town; a match's map, which a match alone reaches, aside", func(t *testing.T) {
		for _, m := range d.Maps() {
			if m.Kind == gamedata.Pvp {
				continue
			}
			if !reaches(d, d.StartTown().ID, m.ID) {
				t.Errorf("%s: no portals lead there from %s", m.ID, d.StartTown().ID)
			}
			if !reaches(d, m.ID, m.Town) {
				t.Errorf("%s: no portals lead back to its town %s", m.ID, m.Town)
			}
		}
	})
	t.Run("a mob's EXP never drops as its level rises, normal mobs, elites and bosses each among their own", func(t *testing.T) {
		kind := func(m gamedata.Mob) [2]bool { return [2]bool{m.Elite, m.Boss} }
		for _, k := range [][2]bool{{false, false}, {true, false}, {false, true}} {
			var mobs []gamedata.Mob
			for _, m := range d.Mobs() {
				if kind(m) == k {
					mobs = append(mobs, m)
				}
			}
			slices.SortFunc(mobs, func(a, b gamedata.Mob) int { return cmp.Compare(a.Level, b.Level) })
			for k := 1; k < len(mobs); k++ {
				if lo, hi := mobs[k-1], mobs[k]; lo.Level < hi.Level && lo.Exp > hi.Exp {
					t.Errorf("%s (level %d) gives %d EXP, more than %s (level %d) at %d", lo.ID, lo.Level, lo.Exp, hi.ID, hi.Level, hi.Exp)
				}
			}
		}
	})
	t.Run("every hunting map's portals lead somewhere and back", func(t *testing.T) {
		for _, m := range d.Maps() {
			if m.Kind == gamedata.Pvp {
				continue // a match's map has no portals
			}
			if len(m.Portals) == 0 {
				t.Fatalf("map %s has no portals", m.ID)
			}
			for _, p := range m.Portals {
				target, ok := d.Map(p.TargetMap)
				if !ok {
					t.Fatalf("%s: %s -> %s missing", m.ID, p.Name, p.TargetMap)
				}
				back := false
				for _, q := range target.Portals {
					if q.TargetMap == m.ID {
						back = true
					}
				}
				if !back {
					t.Fatalf("%s -> %s has no way back", m.ID, target.ID)
				}
			}
		}
	})

	t.Run("each class's chains are C18's table, its break-free first on its escape root, its block's counter last (C21), and every promotion's skill stands on its own key", func(t *testing.T) {
		text := func(w gamedata.ChainCondition) string {
			switch {
			case w.ControlledSelf:
				return "held"
			case w.After > 0:
				return "after " + w.After.String()
			case w.Dodged > 0:
				return "dodged " + w.Dodged.String()
			case w.Blocked > 0:
				return "blocked " + w.Blocked.String()
			case w.Far > 0:
				return fmt.Sprintf("far %g", w.Far)
			}
			return "controlled " + strings.Join(w.Controlled, ",")
		}
		controlled := "controlled stun,freeze,pin,knockdown"
		want := map[kernel.Class][]string{
			"beginner": {"strike: lunge far 3", "guard: strike blocked 1.5s", "roll_back: break_free held; lunge dodged 1.5s"},
			"warrior":  {"strike: ground_slam controlled stun,freeze; charge far 3", "roll_back: unyielding held; lunge dodged 1.5s", "war_cry: iron_body after 3s", "bastion: ground_slam blocked 1.5s"},
			"thief":    {"double_stab: flurry after 2s; shadow_step far 3", "smoke_bomb: vanish held; shadow_step dodged 1.5s", "poison_blade: haste after 3s", "parry: flurry blocked 1.5s"},
			"bowman":   {"power_shot: arrow_rain " + controlled, "disengage: recoil held; power_shot dodged 1.5s", "focus: eagle_eye after 3s", "deflect: power_shot blocked 1.5s"},
			"magician": {"fire_arrow: meteor " + controlled, "frost_nova: meteor controlled stun,freeze", "blink: arcane_release held; fire_arrow dodged 1.5s", "heal: barrier after 3s", "ward: frost_nova blocked 1.5s"},
		}
		for _, c := range d.Classes() {
			var got []string
			for _, ch := range c.Chains {
				var follow []string
				for _, f := range ch.Follow {
					follow = append(follow, f.Skill+" "+text(f.When))
				}
				got = append(got, ch.Root+": "+strings.Join(follow, "; "))
			}
			if !slices.Equal(got, want[c.ID]) {
				t.Errorf("%s's chains %q, want %q", c.ID, got, want[c.ID])
			}
			for skill := range c.Promotion.Skills {
				if !d.StartClass().Keyable(skill) {
					t.Errorf("%s's promotion asks %s, which a Beginner can't put on a key", c.ID, skill)
				}
			}
		}
	})
}
