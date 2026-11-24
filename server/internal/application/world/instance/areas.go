package instance

import (
	model "finga/internal/model/world"
)

// areaID is the id of the region's area at index a, empty for none.
func (i *Instance) areaID(a int) string {
	if a < 0 {
		return ""
	}
	return i.m.Areas[a].ID
}

// trackAreas tells each player who stepped into another of the region's areas which one.
func (i *Instance) trackAreas() {
	if len(i.m.Areas) == 0 {
		return
	}
	for _, id := range i.order {
		p := i.players[id]
		if a := i.m.AreaAt(p.state.X, p.state.Z); a != p.area {
			p.area = a
			up, down := i.bossState(p)
			p.private = append(p.private, model.AreaEntered{Tick: i.tick, Area: i.areaID(a), FieldBoss: up, FieldBossGone: down})
		}
	}
}
