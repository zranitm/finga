package load

import (
	"fmt"

	"finga/internal/gamedata"
)

const mobsFile = "mobs.yaml"

type mobsYAML struct {
	Mobs []mobYAML `yaml:"mobs"`
}

type mobYAML struct {
	ID              string     `yaml:"id"`
	Name            string     `yaml:"name"`
	Level           int        `yaml:"level"`
	Elite           bool       `yaml:"elite"`
	HP              int        `yaml:"hp"`
	Attack          int        `yaml:"attack"`
	Damage          string     `yaml:"damage"`
	PhysicalDefense int        `yaml:"physical_defense"`
	MagicDefense    int        `yaml:"magic_defense"`
	Accuracy        int        `yaml:"accuracy"`
	Evasion         int        `yaml:"evasion"`
	Exp             int64      `yaml:"exp"`
	Speed           float64    `yaml:"speed"`
	Width           float64    `yaml:"width"`
	Height          float64    `yaml:"height"`
	Behavior        string     `yaml:"behavior"`
	Look            lookYAML   `yaml:"look"`
	Drops           []dropYAML `yaml:"drops"`
}

// lookYAML is the client's art for a mob: a sprite it draws, tinted; the server only checks it is there.
type lookYAML struct {
	Sprite string `yaml:"sprite"`
	Tint   *int   `yaml:"tint"`
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
	for _, raw := range file.Mobs {
		if raw.ID == "" {
			return fmt.Errorf("%s: a mob has no id", mobsFile)
		}
		if l.mobs.has(raw.ID) {
			return fmt.Errorf("%s: duplicate mob %s", mobsFile, raw.ID)
		}
		if raw.Level < 1 || raw.HP < 1 || raw.Width <= 0 || raw.Height <= 0 || raw.Speed < 0 || raw.Exp < 0 {
			return fmt.Errorf("%s: mob %s: level and hp must be at least 1, width and height positive, speed and exp not negative", mobsFile, raw.ID)
		}
		if raw.Look.Sprite == "" || raw.Look.Tint != nil && (*raw.Look.Tint < 0 || *raw.Look.Tint > 0xffffff) {
			return fmt.Errorf("%s: mob %s: look needs a sprite, and a tint is an RGB color", mobsFile, raw.ID)
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
			ID: raw.ID, Name: raw.Name, Level: raw.Level, Elite: raw.Elite, HP: raw.HP, Attack: raw.Attack, Damage: damage,
			PhysicalDefense: raw.PhysicalDefense, MagicDefense: raw.MagicDefense,
			Accuracy: raw.Accuracy, Evasion: raw.Evasion, Exp: raw.Exp, Speed: raw.Speed, Width: raw.Width, Height: raw.Height,
			Behavior: raw.Behavior,
		}
		for _, dr := range raw.Drops {
			item, ok := l.items.get(dr.Item)
			if !ok {
				return fmt.Errorf("%s: mob %s: drop %s: unknown item", mobsFile, raw.ID, dr.Item)
			}
			if dr.Chance <= 0 || dr.Chance > 1 {
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
