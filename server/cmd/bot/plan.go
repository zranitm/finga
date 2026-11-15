package main

import (
	"cmp"
	"maps"
	"math"
	"slices"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/nav"
)

// spotMargin is how far outside its circle a mob still counts as on a spot, as one coming after the bot steps out of it.
const spotMargin = 3.0

// spot is where a bot hunts: a circle on a map.
type spot struct {
	mapID  string
	at     nav.Point
	radius float64
}

// holds reports whether p is on the spot, within spotMargin of its circle.
func (s spot) holds(p nav.Point) bool { return nav.Distance(p, s.at) <= s.radius+spotMargin }

// world is the part of the game the bots walk: the start town and every map a portal leads to that is a town or hunts at a promotion's level or under.
type world struct {
	data   *gamedata.Data
	maps   map[string]*gamedata.Map
	bosses map[string]bool // the field bosses' mob types, never prey
}

func newWorld(data *gamedata.Data) *world {
	top := 0
	for _, c := range data.Classes() {
		top = max(top, c.Promotion.Level)
	}
	start := data.StartTown()
	w := &world{data: data, maps: map[string]*gamedata.Map{start.ID: start}, bosses: map[string]bool{}}
	for _, m := range data.Maps() {
		if m.Boss != nil {
			w.bosses[m.Boss.Mob] = true
		}
	}
	for queue := []*gamedata.Map{start}; len(queue) > 0; queue = queue[1:] {
		for _, p := range queue[0].Portals {
			next, ok := data.Map(p.TargetMap)
			if !ok || w.maps[next.ID] != nil || (next.Kind == gamedata.Hunting && next.MinLevel > top) {
				continue
			}
			w.maps[next.ID] = next
			queue = append(queue, next)
		}
	}
	return w
}

// portalTo is the portal on map from that starts the fewest hops to map to, among the bots' maps.
func (w *world) portalTo(from, to string) (gamedata.Portal, bool) {
	type hop struct {
		id    string
		first gamedata.Portal
	}
	if w.maps[from] == nil {
		return gamedata.Portal{}, false
	}
	seen := map[string]bool{from: true}
	for queue := []hop{{id: from}}; len(queue) > 0; queue = queue[1:] {
		h := queue[0]
		for _, p := range w.maps[h.id].Portals {
			if seen[p.TargetMap] || w.maps[p.TargetMap] == nil {
				continue
			}
			first := h.first
			if h.id == from {
				first = p
			}
			if p.TargetMap == to {
				return first, true
			}
			seen[p.TargetMap] = true
			queue = append(queue, hop{p.TargetMap, first})
		}
	}
	return gamedata.Portal{}, false
}

// arrival is where a player comes out through portal p: the centre of the portal it names on its map.
func (w *world) arrival(p gamedata.Portal) (nav.Point, bool) {
	m, ok := w.maps[p.TargetMap]
	if !ok {
		return nav.Point{}, false
	}
	for _, q := range m.Portals {
		if q.Name == p.TargetPortal {
			return point(q.At), true
		}
	}
	return nav.Point{}, false
}

// spots are where a mob is hunted on the bots' maps: each spawn circle of it, the widest first.
func (w *world) spots(mob string) []spot {
	var out []spot
	for _, id := range slices.Sorted(maps.Keys(w.maps)) {
		for _, s := range w.maps[id].Spawns {
			if s.Mob == mob {
				out = append(out, spot{mapID: id, at: nav.Point{X: s.At.X, Z: s.At.Z}, radius: s.Radius})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b spot) int { return cmp.Compare(b.radius, a.radius) })
	return out
}

// levelSpot is where a bot of a level hunts for EXP: on the map of the strongest normal mob under its level, or at the first, so it seldom dies, the richer on a tie; a circle round every spawn there of a mob it may hunt, so the whole map's mobs feed a party that shares their EXP.
func (w *world) levelSpot(lvl int) (spot, bool) {
	top := max(lvl-1, 1)
	var best *gamedata.Mob
	var at spot
	for _, m := range w.data.Mobs() {
		if m.Elite || m.Level > top || (best != nil && (m.Level < best.Level || (m.Level == best.Level && m.Exp <= best.Exp))) {
			continue
		}
		if s := w.spots(m.ID); len(s) > 0 {
			best, at = &m, s[0]
		}
	}
	if best == nil {
		return spot{}, false
	}
	var all []spot
	for _, m := range w.data.Mobs() {
		if !m.Elite && m.Level <= top {
			for _, s := range w.spots(m.ID) {
				if s.mapID == at.mapID {
					all = append(all, s)
				}
			}
		}
	}
	return round(at.mapID, all), true
}

// round is the circle about the middle of spots' bounds that holds them all.
func round(mapID string, spots []spot) spot {
	x0, z0, x1, z1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, s := range spots {
		x0, z0 = min(x0, s.at.X-s.radius), min(z0, s.at.Z-s.radius)
		x1, z1 = max(x1, s.at.X+s.radius), max(z1, s.at.Z+s.radius)
	}
	out := spot{mapID: mapID, at: nav.Point{X: (x0 + x1) / 2, Z: (z0 + z1) / 2}}
	for _, s := range spots {
		out.radius = max(out.radius, nav.Distance(out.at, s.at)+s.radius)
	}
	return out
}

// nearest is the closest living mob to from of a prey kind, or of any with no prey named, on a spot and not skipped; the lower ID on a tie, so the choice holds.
func nearest(mobs map[uint32]*clientv1.MobView, s spot, prey []string, from nav.Point, skip func(*clientv1.MobView) bool) *clientv1.MobView {
	var best *clientv1.MobView
	bd := math.Inf(1)
	for _, m := range mobs {
		at := nav.Point{X: m.GetX(), Z: m.GetZ()}
		if (prey != nil && !slices.Contains(prey, m.GetType())) || m.GetHp() <= 0 || !s.holds(at) || (skip != nil && skip(m)) {
			continue
		}
		if d := nav.Distance(at, from); d < bd || (d == bd && m.GetId() < best.GetId()) {
			best, bd = m, d
		}
	}
	return best
}

// levelPrey are the mobs a bot of a level hunts for EXP: the normal ones under its level, or at the first, but field bosses.
func (w *world) levelPrey(lvl int) []string {
	var out []string
	for _, m := range w.data.Mobs() {
		if !m.Elite && !w.bosses[m.ID] && m.Level <= max(lvl-1, 1) {
			out = append(out, m.ID)
		}
	}
	return out
}

// droppers are the mobs that drop an item, the likeliest first.
func (w *world) droppers(item string) []string {
	type d struct {
		mob    string
		chance float64
	}
	var out []d
	for _, m := range w.data.Mobs() {
		for _, dr := range m.Drops {
			if dr.Item == item {
				out = append(out, d{m.ID, dr.Chance})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b d) int { return cmp.Compare(b.chance, a.chance) })
	ids := make([]string, len(out))
	for i, x := range out {
		ids[i] = x.mob
	}
	return ids
}

type goalKind int

const (
	huntGoal goalKind = iota // for EXP, a kill or a drop
	talkGoal                 // to take, work or hand in a quest, or to promote
	tourGoal                 // a trip to the start town to talk to everyone there
)

type goal struct {
	kind    goalKind
	spot    spot         // a hunt's
	prey    []string     // the mobs a hunt goes after
	npc     gamedata.NPC // a talk's
	why     string       // what it is for, in the bot's log
	promote bool         // a talk to the trainer to promote
}

// plan is what a bot knows of itself for choosing a goal.
type plan struct {
	class    kernel.Class
	level    int
	own      gamedata.Stats
	invested map[string]int32
	quests   map[string]*clientv1.QuestState
	holding  func(item string) int
}

// next is what a bot works toward: its future class's quests in order, each taken, worked and handed in, then the promotion once the rest is met; it hunts for EXP while a level stands in the way, and as its class once promoted.
func (w *world) next(future gamedata.Class, p plan) goal {
	hunt := func(why string) goal {
		s, _ := w.levelSpot(p.level)
		return goal{kind: huntGoal, spot: s, prey: w.levelPrey(p.level), why: why}
	}
	if p.class == future.ID {
		return hunt("as a " + string(future.ID))
	}
	talk := func(npc, why string) goal {
		n, _ := w.data.NPC(npc)
		return goal{kind: talkGoal, npc: n, why: why}
	}
	for _, id := range future.Promotion.Quests {
		q, ok := w.data.Quest(id)
		if !ok {
			continue
		}
		st := p.quests[id]
		switch {
		case finished(st):
			continue
		case st.GetStatus() != clientv1.QuestStatus_QUEST_STATUS_ACTIVE:
			if p.level < q.Level {
				return hunt("for " + id + "'s level")
			}
			return talk(q.Giver, "to take "+id)
		}
		for i, o := range q.Objectives {
			var have int
			if i < len(st.GetProgress()) {
				have = int(st.GetProgress()[i])
			}
			switch o.Kind {
			case gamedata.Kill:
				if have < o.Count {
					return w.huntFor(o.Target, id, hunt)
				}
			case gamedata.Collect:
				if p.holding(o.Target) < o.Count {
					for _, mob := range w.droppers(o.Target) {
						if g := w.huntFor(mob, id, nil); g.why != "" {
							return g
						}
					}
					return hunt(id + "'s drop is out of reach")
				}
			case gamedata.TalkTo:
				if have < 1 {
					return talk(o.Target, "for "+id)
				}
			case gamedata.Reach:
				if p.level < o.Count {
					return hunt("for " + id + "'s level")
				}
			}
		}
		return talk(q.TurnIn, "to hand in "+id)
	}
	if meets(future.Promotion, p) {
		g := talk(future.Promotion.Trainer, "to promote")
		g.promote = true
		return g
	}
	return hunt("for the promotion")
}

// huntFor hunts a mob for a quest at its widest spawn on the bots' maps; with none it falls back, or with no fallback gives the empty goal.
func (w *world) huntFor(mob, quest string, fallback func(string) goal) goal {
	if s := w.spots(mob); len(s) > 0 {
		return goal{kind: huntGoal, spot: s[0], prey: []string{mob}, why: mob + " for " + quest}
	}
	if fallback == nil {
		return goal{}
	}
	return fallback(mob + " is out of reach")
}

func finished(st *clientv1.QuestState) bool {
	return st.GetStatus() == clientv1.QuestStatus_QUEST_STATUS_FINISHED || st.GetTimesFinished() > 0
}

// meets is every promotion condition as the bot counts them, its level, own stats, skill points and finished quests; the trainer's dialog has the last word.
func meets(pr gamedata.Promotion, p plan) bool {
	n, h := pr.Stats, p.own
	if p.level < pr.Level || h.STR < n.STR || h.DEX < n.DEX || h.INT < n.INT || h.AGI < n.AGI || h.VIT < n.VIT {
		return false
	}
	for _, q := range pr.Quests {
		if !finished(p.quests[q]) {
			return false
		}
	}
	for skill, need := range pr.Skills {
		if int(p.invested[skill]) < need {
			return false
		}
	}
	return true
}

// promotable reports whether a trainer's dialog offers the class with every condition met.
func promotable(o *clientv1.PromotionOffer, class kernel.Class) bool {
	if o == nil || o.GetClass() != string(class) || len(o.GetConditions()) == 0 {
		return false
	}
	for _, c := range o.GetConditions() {
		if c.GetHave() < c.GetNeed() {
			return false
		}
	}
	return true
}
