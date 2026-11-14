// Package progression holds the stat and level rules shared by the character service and the world.
package progression

import "finga/internal/gamedata"

// Progress is a level and the EXP within it.
type Progress struct {
	Level int
	Exp   int64
}

// Derived are the thirteen computed stats: HP is whole points, regen is points a second, and rates are fractions.
type Derived struct {
	MaxHP           int     `json:"max_hp"`
	PhysicalAttack  float64 `json:"physical_attack"`
	MagicAttack     float64 `json:"magic_attack"`
	PhysicalDefense float64 `json:"physical_defense"`
	MagicDefense    float64 `json:"magic_defense"`
	Accuracy        float64 `json:"accuracy"`
	Evasion         float64 `json:"evasion"`
	CritRate        float64 `json:"crit_rate"`
	CritDamage      float64 `json:"crit_damage"`
	Penetration     float64 `json:"penetration"`
	HPRegen         float64 `json:"hp_regen"`
	CooldownSpeed   float64 `json:"cooldown_speed"`
	MoveSpeed       float64 `json:"move_speed"`
}

// minus is d less o, stat by stat.
func (d Derived) minus(o Derived) Derived {
	return Derived{
		MaxHP:          d.MaxHP - o.MaxHP,
		PhysicalAttack: d.PhysicalAttack - o.PhysicalAttack, MagicAttack: d.MagicAttack - o.MagicAttack,
		PhysicalDefense: d.PhysicalDefense - o.PhysicalDefense, MagicDefense: d.MagicDefense - o.MagicDefense,
		Accuracy: d.Accuracy - o.Accuracy, Evasion: d.Evasion - o.Evasion,
		CritRate: d.CritRate - o.CritRate, CritDamage: d.CritDamage - o.CritDamage, Penetration: d.Penetration - o.Penetration,
		HPRegen:       d.HPRegen - o.HPRegen,
		CooldownSpeed: d.CooldownSpeed - o.CooldownSpeed, MoveSpeed: d.MoveSpeed - o.MoveSpeed,
	}
}

// ItemStats are what equipment gives, the five primary and the thirteen derived in Rolls' order, a whole number each: rates, crit damage and penetration in tenths of a percent, regen in tenths of a point a second. One copy's roll, or all a character wears.
type ItemStats struct {
	STR             int `json:"str,omitempty"`
	DEX             int `json:"dex,omitempty"`
	INT             int `json:"int,omitempty"`
	AGI             int `json:"agi,omitempty"`
	VIT             int `json:"vit,omitempty"`
	PhysicalAttack  int `json:"physical_attack,omitempty"`
	MagicAttack     int `json:"magic_attack,omitempty"`
	PhysicalDefense int `json:"physical_defense,omitempty"`
	MagicDefense    int `json:"magic_defense,omitempty"`
	MaxHP           int `json:"max_hp,omitempty"`
	Accuracy        int `json:"accuracy,omitempty"`
	Evasion         int `json:"evasion,omitempty"`
	CritRate        int `json:"crit_rate,omitempty"`
	CritDamage      int `json:"crit_damage,omitempty"`
	Penetration     int `json:"penetration,omitempty"`
	HPRegen         int `json:"hp_regen,omitempty"`
	CooldownSpeed   int `json:"cooldown_speed,omitempty"`
	MoveSpeed       int `json:"move_speed,omitempty"`
}

// values are the stats in their fixed order.
func (s *ItemStats) values() [18]*int {
	return [18]*int{
		&s.STR, &s.DEX, &s.INT, &s.AGI, &s.VIT,
		&s.PhysicalAttack, &s.MagicAttack, &s.PhysicalDefense, &s.MagicDefense,
		&s.MaxHP, &s.Accuracy, &s.Evasion,
		&s.CritRate, &s.CritDamage, &s.Penetration, &s.HPRegen,
		&s.CooldownSpeed, &s.MoveSpeed,
	}
}

// Primary are the primary stats among them.
func (s ItemStats) Primary() gamedata.Stats {
	return gamedata.Stats{STR: s.STR, DEX: s.DEX, INT: s.INT, AGI: s.AGI, VIT: s.VIT}
}

// Plus is the two summed, as what a character wears adds up.
func (s ItemStats) Plus(o ItemStats) ItemStats {
	add := o.values()
	for k, v := range s.values() {
		*v += *add[k]
	}
	return s
}

// ExpCurve is what the EXP rules need from game data.
type ExpCurve interface {
	ExpToNext(level int) (int64, bool)
	LevelCap() int
}
