package instance

import (
	"time"

	"finga/internal/gamedata"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/services/world/internal/model"
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

// StatsOf counts a character of class at level with points and skills, wearing worn, on a map ticking every tick: the own stats are the starting stats every character has plus the points spent, a worn item adds only while those meet its requirements, and passive skills add their bonus.
func StatsOf(data Content, class gamedata.Class, level int, points model.Points, skills model.Skills, worn model.Worn, tick time.Duration) Stats {
	own := progression.AddStats(data.StartClass().BaseStats, points.Spent)
	items := make([]progression.WornItem, 0, len(worn.Items))
	for _, w := range worn.Items {
		if it, ok := data.Item(w.Template); ok {
			items = append(items, progression.WornItem{Item: it, Stats: w.Stats})
		}
	}
	sheet := progression.SheetOf(data.StatRules(), class.ID, level, own, progression.Active(items, level, class, own), progression.Passives(learned(data, class, skills)))
	cooldown := model.AttackCooldownTicks(class.Attack.Cooldown, sheet.Derived.AttackSpeed, tick)
	return Stats{Own: own, Derived: sheet.Derived, Sheet: model.Sheet{Sheet: sheet, Points: points, Skills: skills.Clone(), AttackCooldownTicks: cooldown}}
}

func (i *Instance) stats(p *player) Stats {
	return StatsOf(i.data, p.class, p.Level, p.Points, p.Skills, p.Worn, i.tuning.TickInterval)
}

// movement is the player's movement constants: sped up by their move speed in a hunting map, as they are in a town.
func (i *Instance) movement(p *player) sim.Params {
	if i.m.Kind == gamedata.Town {
		return i.params
	}
	return i.params.Faster(sim.SpeedFactor(p.derived.MoveSpeed))
}

// restat re-derives the player's stats after their points or gear changed, lowers HP and MP to any new maximum, and tells the player.
func (i *Instance) restat(p *player) {
	s := i.stats(p)
	p.derived = s.Derived
	p.HP, p.MP = min(p.HP, p.derived.MaxHP), min(p.MP, p.derived.MaxMP)
	p.changed = true
	p.private = append(p.private, model.StatsChanged{Tick: i.tick, Sheet: &s.Sheet, HP: p.HP, MaxHP: p.derived.MaxHP, MP: p.MP, MaxMP: p.derived.MaxMP})
}
