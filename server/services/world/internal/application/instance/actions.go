package instance

import (
	"slices"

	"finga/internal/gamedata"
	"finga/services/world/internal/model"
)

// actionKind is what keeps a player busy: nothing, a basic attack or an active skill.
type actionKind uint8

const (
	noAction actionKind = iota
	attackAction
	skillAction
)

// action is what a player is busy with, in this map's ticks. Only the map holds it, so a map change, a death or a join starts with none.
type action struct {
	kind       actionKind
	cancelable bool   // the other kind may cut it off: every attack, and a skill its data marks
	cancelAt   uint64 // from this tick the other kind may start, if it may cut it off
	end        uint64
}

// nextStart is the first tick an action of kind may start: the cancel point when it cuts this one off, else the end.
func (a action) nextStart(kind actionKind) uint64 {
	if a.kind != kind && a.cancelable {
		return a.cancelAt
	}
	return a.end
}

// windUp is an action's hit still to land, in its tick, from where the player stands then.
type windUp struct {
	at   uint64
	land func()
}

// begin starts p's action at start, landing its hit at once if it falls there, else when due, and says whether it landed now; a skill up to castSlack early still counts from start.
func (i *Instance) begin(p *player, kind actionKind, start, length uint64, t gamedata.Timing, cancelable bool, hit func()) bool {
	hitAt, cancelAt := model.ActionPoints(length, t.Hit, t.Cancel)
	p.action = action{kind: kind, cancelable: cancelable, cancelAt: start + cancelAt, end: start + length}
	if hitAt == 0 {
		hit()
		return true
	}
	p.windUps = append(p.windUps, windUp{at: start + hitAt, land: hit})
	return false
}

// landWindUps lands each hit whose tick has come, before this tick's skills and attacks.
func (i *Instance) landWindUps() {
	for _, id := range i.order {
		p := i.players[id]
		if len(p.windUps) == 0 {
			continue
		}
		due := slices.DeleteFunc(slices.Clone(p.windUps), func(w windUp) bool { return w.at > i.tick })
		p.windUps = slices.DeleteFunc(p.windUps, func(w windUp) bool { return w.at <= i.tick })
		for _, w := range due {
			w.land()
		}
	}
}
