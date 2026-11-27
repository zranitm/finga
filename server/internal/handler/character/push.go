package character

import (
	"maps"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
)

// Encoders registers the encoders of the pushes the module owns.
func Encoders(e *transport.Encoders) error {
	e.WelcomeSection(func(w model.Welcome, out *clientv1.Welcome) {
		out.Character, out.SkillTimers = characterView(w.Character), skillTimers(w.Timers)
	})
	return nil
}

func characterView(c model.CharacterView) *clientv1.CharacterView {
	return &clientv1.CharacterView{
		Id: c.ID, Name: c.Name, Class: c.Class, Level: int32(c.Level), Exp: c.Exp, Stats: stats(c.Stats),
		Hp: int32(c.HP), MaxHp: int32(c.MaxHP), Energy: uint32(c.Energy), Sheet: statSheet(c.Sheet), //nolint:gosec // HP fits, and energy is 0 to the data's max
	}
}

func stats(st model.Stats) *clientv1.Stats {
	return &clientv1.Stats{Str: int32(st.STR), Dex: int32(st.DEX), Int: int32(st.INT), Agi: int32(st.AGI), Vit: int32(st.VIT)}
}

func statSheet(s model.StatSheet) *clientv1.StatSheet {
	return &clientv1.StatSheet{
		Primary: stats(s.Primary), PrimaryWorn: stats(s.PrimaryWorn), Derived: derived(s.Derived), DerivedWorn: derived(s.DerivedWorn),
		PrimarySkills: stats(s.PrimarySkills), DerivedSkills: derived(s.DerivedSkills), Skills: skillPoints(s.Skills),
		Points:            &clientv1.Points{Unspent: int32(s.Points.Unspent), Spent: stats(s.Points.Spent), FreeResetLeft: s.Points.FreeResetLeft},
		AttackLengthTicks: uint32(s.AttackLengthTicks),
		Buffs:             buffViews(s.Buffs),
	}
}

func buffViews(bs []model.BuffView) []*clientv1.BuffView {
	out := make([]*clientv1.BuffView, 0, len(bs))
	for _, b := range bs {
		out = append(out, &clientv1.BuffView{Skill: b.Skill, LeftMs: b.Left.Milliseconds(), Points: int32(b.Points), From: b.From}) //nolint:gosec // points are small
	}
	return out
}

func skillPoints(s model.SkillPoints) *clientv1.SkillPoints {
	out := &clientv1.SkillPoints{Unspent: int32(s.Unspent), Invested: make(map[string]int32, len(s.Invested)), Traits: maps.Clone(s.Traits)}
	for skill, n := range s.Invested {
		out.Invested[skill] = int32(n)
	}
	return out
}

func derived(d model.DerivedStats) *clientv1.DerivedStats {
	return &clientv1.DerivedStats{
		PhysicalAttack: d.PhysicalAttack, MagicAttack: d.MagicAttack, PhysicalDefense: d.PhysicalDefense, MagicDefense: d.MagicDefense,
		Accuracy: d.Accuracy, Evasion: d.Evasion, CritRate: d.CritRate, CritDamage: d.CritDamage, Penetration: d.Penetration,
		HpRegen: d.HPRegen, CooldownSpeed: d.CooldownSpeed, MoveSpeed: d.MoveSpeed,
		MaxHp: int32(d.MaxHP),
	}
}

var timerKinds = map[model.TimerKind]clientv1.SkillTimerKind{
	model.CooldownTimer: clientv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN,
	model.BuffTimer:     clientv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF,
}

func skillTimers(ts []model.SkillTimer) []*clientv1.SkillTimer {
	if len(ts) == 0 {
		return nil
	}
	out := make([]*clientv1.SkillTimer, 0, len(ts))
	for _, t := range ts {
		out = append(out, &clientv1.SkillTimer{Skill: t.Skill, Kind: timerKinds[t.Kind], LeftMs: t.Left.Milliseconds(), Points: int32(t.Points), Trait: t.Trait}) //nolint:gosec // points are small
	}
	return out
}
