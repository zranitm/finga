package load

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"time"

	"finga/internal/gamedata"
)

const mobsFile = "mobs.yaml"

type mobsYAML struct {
	HitGuard  time.Duration            `yaml:"hit_guard"`
	Provoke   time.Duration            `yaml:"provoke"`
	Sight     float64                  `yaml:"sight"`
	Engage    []time.Duration          `yaml:"engage"`
	React     reactYAML                `yaml:"react"`
	Patrol    patrolYAML               `yaml:"patrol"`
	Attacks   map[string]mobAttackYAML `yaml:"attacks"`
	FieldBoss fieldBossYAML            `yaml:"field_boss"`
	Mobs      []mobYAML                `yaml:"mobs"`
}

// mobAttackValues are an attack's values, each unset until the table or a mob's use sets it.
type mobAttackValues struct {
	Windup   *time.Duration `yaml:"windup"`
	Travel   *time.Duration `yaml:"travel"`
	Recover  *time.Duration `yaml:"recover"`
	Cooldown *time.Duration `yaml:"cooldown"`
	Power    *float64       `yaml:"power"`
	Reach    *float64       `yaml:"reach"`
	Distance *float64       `yaml:"distance"`
	Spread   *float64       `yaml:"spread"`
	Range    *float64       `yaml:"range"`
	Speed    *float64       `yaml:"speed"`
	Size     *float64       `yaml:"size"`
}

// mobAttackYAML is an entry of the attacks table.
type mobAttackYAML struct {
	Kind            string `yaml:"kind"`
	mobAttackValues `yaml:",inline"`
}

// mobUseYAML is one of a mob's attacks: the table's entry it names, with any values it changes.
type mobUseYAML struct {
	Use             string `yaml:"use"`
	mobAttackValues `yaml:",inline"`
}

type reactYAML struct {
	Flinch     time.Duration `yaml:"flinch"`
	Push       float64       `yaml:"push"`
	DownHits   int           `yaml:"down_hits"`
	DownWindow time.Duration `yaml:"down_window"`
	Down       time.Duration `yaml:"down"`
	ArmorAfter time.Duration `yaml:"armor_after"`
}

type patrolYAML struct {
	SpeedJitter float64         `yaml:"speed_jitter"`
	Pause       []time.Duration `yaml:"pause"`
}

type fieldBossYAML struct {
	Life      time.Duration `yaml:"life"`
	DropShare float64       `yaml:"drop_share"`
}

type mobYAML struct {
	ID              string       `yaml:"id"`
	Name            string       `yaml:"name"`
	Level           int          `yaml:"level"`
	Elite           bool         `yaml:"elite"`
	Boss            bool         `yaml:"boss"`
	HP              int          `yaml:"hp"`
	Attack          int          `yaml:"attack"`
	Damage          string       `yaml:"damage"`
	PhysicalDefense int          `yaml:"physical_defense"`
	MagicDefense    int          `yaml:"magic_defense"`
	Accuracy        int          `yaml:"accuracy"`
	Evasion         int          `yaml:"evasion"`
	Exp             int64        `yaml:"exp"`
	Speed           float64      `yaml:"speed"`
	Width           float64      `yaml:"width"`
	Height          float64      `yaml:"height"`
	Behavior        string       `yaml:"behavior"`
	SuperArmor      bool         `yaml:"super_armor"`
	Attacks         []mobUseYAML `yaml:"attacks"`
	Look            lookYAML     `yaml:"look"`
	Drops           []dropYAML   `yaml:"drops"`
}

// lookYAML is the client's art for a mob: a sprite it draws, tinted and scaled; the server only checks it is there.
type lookYAML struct {
	Sprite string   `yaml:"sprite"`
	Tint   *int     `yaml:"tint"`
	Scale  *float64 `yaml:"scale"`
}

type dropYAML struct {
	Item   string  `yaml:"item"`
	Chance float64 `yaml:"chance"`
	Min    int     `yaml:"min"`
	Max    int     `yaml:"max"`
}

func (l *loader) loadMobs(dir string) error {
	var file mobsYAML
	if err := readYAML(dir, mobsFile, &file); err != nil {
		return err
	}
	if file.HitGuard <= 0 || file.Provoke <= 0 {
		return fmt.Errorf("%s: hit_guard and provoke must be positive", mobsFile)
	}
	if !positive(file.Sight) {
		return fmt.Errorf("%s: sight must be positive", mobsFile)
	}
	if r := file.React; r.Flinch <= 0 || !nonNegative(r.Push) || r.DownHits < 1 || r.DownWindow <= 0 || r.Down <= 0 || r.ArmorAfter <= 0 {
		return fmt.Errorf("%s: react needs positive flinch, down_window, down and armor_after, a push not negative and down_hits of at least 1", mobsFile)
	}
	if p := file.Patrol; !(p.SpeedJitter >= 0 && p.SpeedJitter < 0.5) || len(p.Pause) != 2 || p.Pause[0] <= 0 || p.Pause[0] > p.Pause[1] {
		return fmt.Errorf("%s: patrol needs a speed_jitter from 0 to under 0.5 and a pause of [min, max], both positive, min not above max", mobsFile)
	}
	if e := file.Engage; len(e) != 2 || e[0] < 0 || e[0] > e[1] {
		return fmt.Errorf("%s: engage needs [min, max], min not negative and not above max", mobsFile)
	}
	l.mobRules = gamedata.MobRules{
		HitGuard: file.HitGuard, Provoke: file.Provoke, Sight: file.Sight, React: gamedata.MobReact(file.React),
		Patrol:    gamedata.MobPatrol{SpeedJitter: file.Patrol.SpeedJitter, PauseMin: file.Patrol.Pause[0], PauseMax: file.Patrol.Pause[1]},
		EngageMin: file.Engage[0], EngageMax: file.Engage[1],
	}
	for _, name := range slices.Sorted(maps.Keys(file.Attacks)) {
		if _, err := attackOf(name, file.Attacks[name], mobAttackValues{}); err != nil {
			return fmt.Errorf("%s: %w", mobsFile, err)
		}
	}
	if fb := file.FieldBoss; fb.Life <= 0 || !(fb.DropShare >= 0 && fb.DropShare <= 1) {
		return fmt.Errorf("%s: field_boss needs a positive life and a drop_share from 0 to 1", mobsFile)
	}
	l.fieldBoss = gamedata.FieldBossRules(file.FieldBoss)
	for _, raw := range file.Mobs {
		if raw.ID == "" {
			return fmt.Errorf("%s: a mob has no id", mobsFile)
		}
		if l.mobs.has(raw.ID) {
			return fmt.Errorf("%s: duplicate mob %s", mobsFile, raw.ID)
		}
		if raw.Level < 1 || raw.HP < 1 || !positive(raw.Width) || !positive(raw.Height) || !nonNegative(raw.Speed) || raw.Exp < 0 {
			return fmt.Errorf("%s: mob %s: level and hp must be at least 1, width and height positive, speed and exp not negative", mobsFile, raw.ID)
		}
		if raw.Look.Sprite == "" || raw.Look.Tint != nil && (*raw.Look.Tint < 0 || *raw.Look.Tint > 0xffffff) || raw.Look.Scale != nil && !positive(*raw.Look.Scale) {
			return fmt.Errorf("%s: mob %s: look needs a sprite, a tint is an RGB color and a scale is positive", mobsFile, raw.ID)
		}
		if raw.Boss && raw.Elite {
			return fmt.Errorf("%s: mob %s: a mob is a boss or an elite, not both", mobsFile, raw.ID)
		}
		if raw.Behavior != "patrol" {
			return fmt.Errorf("%s: mob %s: unknown behavior %q (Stage 1 has patrol)", mobsFile, raw.ID, raw.Behavior)
		}
		damage, err := parseDamage(raw.Damage)
		if err != nil {
			return fmt.Errorf("%s: mob %s: %w", mobsFile, raw.ID, err)
		}
		if raw.Attack < 0 || raw.PhysicalDefense < 0 || raw.MagicDefense < 0 || raw.Accuracy < 0 || raw.Evasion < 0 {
			return fmt.Errorf("%s: mob %s: attack, defenses, accuracy and evasion must not be negative", mobsFile, raw.ID)
		}
		mob := gamedata.Mob{
			ID: raw.ID, Name: raw.Name, Level: raw.Level, Elite: raw.Elite, Boss: raw.Boss, HP: raw.HP, Attack: raw.Attack, Damage: damage,
			PhysicalDefense: raw.PhysicalDefense, MagicDefense: raw.MagicDefense,
			Accuracy: raw.Accuracy, Evasion: raw.Evasion, Exp: raw.Exp, Speed: raw.Speed, Width: raw.Width, Height: raw.Height,
			Behavior: raw.Behavior, SuperArmor: raw.SuperArmor,
		}
		if len(raw.Attacks) == 0 {
			return fmt.Errorf("%s: mob %s: no attack", mobsFile, raw.ID)
		}
		for _, use := range raw.Attacks {
			entry, ok := file.Attacks[use.Use]
			if !ok {
				return fmt.Errorf("%s: mob %s: unknown attack %q", mobsFile, raw.ID, use.Use)
			}
			a, err := attackOf(use.Use, entry, use.mobAttackValues)
			if err != nil {
				return fmt.Errorf("%s: mob %s: %w", mobsFile, raw.ID, err)
			}
			mob.Attacks = append(mob.Attacks, a)
		}
		for _, dr := range raw.Drops {
			item, ok := l.items.get(dr.Item)
			if !ok {
				return fmt.Errorf("%s: mob %s: drop %s: unknown item", mobsFile, raw.ID, dr.Item)
			}
			if !(dr.Chance > 0 && dr.Chance <= 1) {
				return fmt.Errorf("%s: mob %s: drop %s: chance must be within (0, 1]", mobsFile, raw.ID, dr.Item)
			}
			if dr.Min < 1 || dr.Max < dr.Min {
				return fmt.Errorf("%s: mob %s: drop %s: need 1 <= min <= max", mobsFile, raw.ID, dr.Item)
			}
			if item.Tab == gamedata.Equip && dr.Max != 1 {
				return fmt.Errorf("%s: mob %s: drop %s: equipment never stacks, max must be 1", mobsFile, raw.ID, dr.Item)
			}
			mob.Drops = append(mob.Drops, gamedata.Drop(dr))
		}
		l.mobs.add(mob.ID, mob)
	}
	return nil
}

// kindNeeds are the values each kind of attack needs besides the timings and power every one does; it takes no others.
var kindNeeds = map[gamedata.MobAttackKind][]string{
	gamedata.Front:  {"reach"},
	gamedata.Leap:   {"distance", "travel", "spread"},
	gamedata.Rush:   {"distance", "travel"},
	gamedata.Shot:   {"range", "speed", "size"},
	gamedata.Around: {"spread"},
}

// attackOf is the table's entry named name with a mob's changes over it, checked for its kind.
func attackOf(name string, entry mobAttackYAML, changes mobAttackValues) (gamedata.MobAttack, error) {
	kind := gamedata.MobAttackKind(entry.Kind)
	needs, ok := kindNeeds[kind]
	if !ok {
		return gamedata.MobAttack{}, fmt.Errorf("attack %s: unknown kind %q", name, entry.Kind)
	}
	v := entry.over(changes)
	set := v.set()
	for _, field := range append([]string{"windup", "recover", "cooldown", "power"}, needs...) {
		x, ok := set[field]
		if !ok {
			return gamedata.MobAttack{}, fmt.Errorf("attack %s: a %s needs %s", name, kind, field)
		}
		if !positive(x) {
			return gamedata.MobAttack{}, fmt.Errorf("attack %s: %s must be positive", name, field)
		}
		delete(set, field)
	}
	if len(set) > 0 {
		return gamedata.MobAttack{}, fmt.Errorf("attack %s: a %s has no %s", name, kind, slices.Sorted(maps.Keys(set))[0])
	}
	a := gamedata.MobAttack{
		Name: name, Kind: kind,
		Windup: deref(v.Windup), Travel: deref(v.Travel), Recover: deref(v.Recover), Cooldown: deref(v.Cooldown),
		Power: deref(v.Power), Reach: deref(v.Reach), Distance: deref(v.Distance), Spread: deref(v.Spread),
		Range: deref(v.Range), Speed: deref(v.Speed), Size: deref(v.Size),
	}
	if a.Kind == gamedata.Shot && a.Range < a.Size {
		return gamedata.MobAttack{}, fmt.Errorf("attack %s: range %g is shorter than its size %g", name, a.Range, a.Size)
	}
	if a.Cooldown < a.Busy() {
		return gamedata.MobAttack{}, fmt.Errorf("attack %s: cooldown %s is shorter than its windup, travel and recover, %s", name, a.Cooldown, a.Busy())
	}
	return a, nil
}

// over is v with each value c sets in its place.
func (v mobAttackValues) over(c mobAttackValues) mobAttackValues {
	return mobAttackValues{
		Windup: cmp.Or(c.Windup, v.Windup), Travel: cmp.Or(c.Travel, v.Travel), Recover: cmp.Or(c.Recover, v.Recover), Cooldown: cmp.Or(c.Cooldown, v.Cooldown),
		Power: cmp.Or(c.Power, v.Power), Reach: cmp.Or(c.Reach, v.Reach), Distance: cmp.Or(c.Distance, v.Distance), Spread: cmp.Or(c.Spread, v.Spread),
		Range: cmp.Or(c.Range, v.Range), Speed: cmp.Or(c.Speed, v.Speed), Size: cmp.Or(c.Size, v.Size),
	}
}

// set are the values given, by their names in the file; a time counts in seconds.
func (v mobAttackValues) set() map[string]float64 {
	out := map[string]float64{}
	for name, d := range map[string]*time.Duration{"windup": v.Windup, "travel": v.Travel, "recover": v.Recover, "cooldown": v.Cooldown} {
		if d != nil {
			out[name] = d.Seconds()
		}
	}
	for name, x := range map[string]*float64{"power": v.Power, "reach": v.Reach, "distance": v.Distance, "spread": v.Spread, "range": v.Range, "speed": v.Speed, "size": v.Size} {
		if x != nil {
			out[name] = *x
		}
	}
	return out
}

// deref is what p points to, or the zero value for nil.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
