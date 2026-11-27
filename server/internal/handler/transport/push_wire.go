package transport

import (
	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/progression"
)

// ItemStats is an item's roll as the client reads it; an inventory's items and a pouch's both show one.
func ItemStats(s *progression.ItemStats) *clientv1.ItemStats {
	if s == nil {
		return nil
	}
	return &clientv1.ItemStats{
		Str: int32(s.STR), Dex: int32(s.DEX), Int: int32(s.INT), Agi: int32(s.AGI), Vit: int32(s.VIT),
		PhysicalAttack: int32(s.PhysicalAttack), MagicAttack: int32(s.MagicAttack), PhysicalDefense: int32(s.PhysicalDefense), MagicDefense: int32(s.MagicDefense),
		MaxHp: int32(s.MaxHP), Accuracy: int32(s.Accuracy), Evasion: int32(s.Evasion),
		CritRate: int32(s.CritRate), CritDamage: int32(s.CritDamage), Penetration: int32(s.Penetration), HpRegen: int32(s.HPRegen),
		CooldownSpeed: int32(s.CooldownSpeed), MoveSpeed: int32(s.MoveSpeed),
	}
}
