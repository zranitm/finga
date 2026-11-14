// Package itemroll converts an equipment roll to and from the item service's messages, for the world and the dev tools that add items.
package itemroll

import (
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/progression"
)

// Proto is a roll as the item service takes it, nil for none.
func Proto(s *progression.ItemStats) *itemv1.ItemStats {
	if s == nil {
		return nil
	}
	//nolint:gosec // stats are small
	return &itemv1.ItemStats{
		Str: int32(s.STR), Dex: int32(s.DEX), Int: int32(s.INT), Agi: int32(s.AGI), Vit: int32(s.VIT),
		PhysicalAttack: int32(s.PhysicalAttack), MagicAttack: int32(s.MagicAttack), PhysicalDefense: int32(s.PhysicalDefense), MagicDefense: int32(s.MagicDefense),
		MaxHp: int32(s.MaxHP), Accuracy: int32(s.Accuracy), Evasion: int32(s.Evasion),
		CritRate: int32(s.CritRate), CritDamage: int32(s.CritDamage), Penetration: int32(s.Penetration), HpRegen: int32(s.HPRegen),
		CooldownSpeed: int32(s.CooldownSpeed), MoveSpeed: int32(s.MoveSpeed),
	}
}

// FromProto is a roll as the item service sends it, nil for none.
func FromProto(s *itemv1.ItemStats) *progression.ItemStats {
	if s == nil {
		return nil
	}
	return &progression.ItemStats{
		STR: int(s.GetStr()), DEX: int(s.GetDex()), INT: int(s.GetInt()), AGI: int(s.GetAgi()), VIT: int(s.GetVit()),
		PhysicalAttack: int(s.GetPhysicalAttack()), MagicAttack: int(s.GetMagicAttack()), PhysicalDefense: int(s.GetPhysicalDefense()), MagicDefense: int(s.GetMagicDefense()),
		MaxHP: int(s.GetMaxHp()), Accuracy: int(s.GetAccuracy()), Evasion: int(s.GetEvasion()),
		CritRate: int(s.GetCritRate()), CritDamage: int(s.GetCritDamage()), Penetration: int(s.GetPenetration()), HPRegen: int(s.GetHpRegen()),
		CooldownSpeed: int(s.GetCooldownSpeed()), MoveSpeed: int(s.GetMoveSpeed()),
	}
}
