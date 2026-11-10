package instance

import (
	"finga/internal/gamedata"
	"finga/internal/progression"
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
}

// StatsOf counts a character of class at level with points, wearing worn: the own stats are the class's starting stats plus the points spent, and a worn item adds only while those meet its requirements.
func StatsOf(data Content, class gamedata.Class, level int, points model.Points, worn model.Worn) Stats {
	own := progression.AddStats(class.BaseStats, points.Spent)
	items := make([]progression.WornItem, 0, len(worn.Items))
	for _, w := range worn.Items {
		if it, ok := data.Item(w.Template); ok {
			items = append(items, progression.WornItem{Item: it, Stats: w.Stats})
		}
	}
	sheet := progression.SheetOf(data.StatRules(), class.ID, level, own, progression.Active(items, level, class.ID, own))
	return Stats{Own: own, Derived: sheet.Derived, Sheet: model.Sheet{Sheet: sheet, Points: points}}
}

func (i *Instance) stats(p *player) Stats { return StatsOf(i.data, p.class, p.Level, p.Points, p.Worn) }

// restat re-derives the player's stats after their points or gear changed, lowers HP and MP to any new maximum, and tells the player.
func (i *Instance) restat(p *player) {
	s := i.stats(p)
	p.derived = s.Derived
	p.HP, p.MP = min(p.HP, p.derived.MaxHP), min(p.MP, p.derived.MaxMP)
	p.changed = true
	p.private = append(p.private, model.StatsChanged{Tick: i.tick, Sheet: s.Sheet, HP: p.HP, MaxHP: p.derived.MaxHP, MP: p.MP, MaxMP: p.derived.MaxMP})
}
