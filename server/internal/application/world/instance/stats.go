package instance

import (
	"math"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
)

// Stats are a character's stats as the world counts them: their own stats, the derived ones, and the sheet.
type Stats struct {
	Own     gamedata.Stats
	Derived progression.Derived
	Sheet   model.Sheet
}

// Content is the game data StatsOf reads.
type Content interface {
	Item(id string) (gamedata.Item, bool)
	StatRules() gamedata.StatRules
	StartClass() gamedata.Class
	Skill(id string) (gamedata.Skill, bool)
}

// StatsOf counts a character of class at level with points and skills, wearing worn, under buffs, on a map ticking every tick: the own stats are the starting stats every character has plus the points spent, a worn item adds only while those meet its requirements, and passive skills and buffs add their bonus as the skills' share.
func StatsOf(data Content, class gamedata.Class, level int, points model.Points, skills model.Skills, worn model.Worn, buffs progression.ItemStats, tick time.Duration) Stats {
	own := progression.AddStats(data.StartClass().BaseStats, points.Spent)
	items := make([]progression.WornItem, 0, len(worn.Items))
	for _, w := range worn.Items {
		if it, ok := data.Item(w.Template); ok {
			items = append(items, progression.WornItem{Item: it, Stats: w.Stats})
		}
	}
	sheet := progression.SheetOf(data.StatRules(), class.ID, level, own, progression.Active(items, level, class, own), progression.Passives(learned(data, class, skills)).Plus(buffs))
	length := model.TicksIn(class.Attack.Length, tick)
	return Stats{Own: own, Derived: sheet.Derived, Sheet: model.Sheet{Sheet: sheet, Points: points, Skills: skills.Clone(), AttackLengthTicks: length}}
}

// stats are p's stats, their sheet listing the buffs running on them.
func (i *Instance) stats(p *player) Stats {
	s := StatsOf(i.data, p.class, p.Level, p.Points, p.Skills, p.Worn, p.buffed(), i.tuning.TickInterval)
	if i.match != nil { // the kit's HP scaled, and no regen
		s.Derived.MaxHP = int(math.Round(float64(s.Derived.MaxHP) * i.match.rules.Scale.HP))
		s.Derived.HPRegen = 0
		s.Sheet.Derived = s.Derived
	}
	now := time.Now()
	s.Sheet.Buffs = BuffViewsOf(i.timersOf(p, now), p.ID, now)
	return s
}

// movement is the player's movement constants: sped up by their move speed outside a town, with no sprint in combat.
func (i *Instance) movement(p *player) sim.Params {
	moves := i.params
	if i.m.Kind != gamedata.Town {
		moves = moves.Faster(sim.SpeedFactor(p.derived.MoveSpeed))
	}
	if i.inCombat(p) {
		return moves.Fighting()
	}
	return moves
}

// restat re-derives the player's stats after their points or gear changed, lowers HP to any new maximum, and tells the player.
func (i *Instance) restat(p *player) {
	s := i.stats(p)
	p.derived = s.Derived
	p.HP = min(p.HP, p.derived.MaxHP)
	p.changed = true
	p.private = append(p.private, model.StatsChanged{Tick: i.tick, Sheet: &s.Sheet, HP: p.HP, MaxHP: p.derived.MaxHP})
}
