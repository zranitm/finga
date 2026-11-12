package main

import (
	"cmp"
	"maps"
	"math"
	"slices"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
)

// spotMargin keeps a hunting bot this far inside a spawn's ends, clear of a platform's edge.
const spotMargin = 24.0

// spot is where a bot hunts: between two x on level y of a map, up a climb when that isn't the ground.
type spot struct {
	mapID    string
	from, to float64
	y        float64
	climb    *climb // nil on the ground
}

// world is the part of the game the bots walk: the start town and every map a portal leads to that is a town or hunts at a promotion's level or under, with their climbs.
type world struct {
	data   *gamedata.Data
	maps   map[string]*gamedata.Map
	climbs map[string]*climb // by map
}

func newWorld(data *gamedata.Data, climbs map[string]*climb) *world {
	top := 0
	for _, c := range data.Classes() {
		top = max(top, c.Promotion.Level)
	}
	start := data.StartTown()
	w := &world{data: data, maps: map[string]*gamedata.Map{start.ID: start}, climbs: climbs}
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

// spots are where a mob is hunted on the bots' maps: the widest spawn of it on a map's ground first, then any its map's climb reaches.
func (w *world) spots(mob string) []spot {
	var ground, high []spot
	for _, id := range slices.Sorted(maps.Keys(w.maps)) {
		m := w.maps[id]
		for _, s := range m.Spawns {
			if s.Mob != mob {
				continue
			}
			from, to, bottom := s.Area.X, s.Area.X+s.Area.W, s.Area.Y+s.Area.H
			if level(bottom, m.PlayerSpawn.Y) {
				ground = append(ground, spot{mapID: id, from: from + spotMargin, to: to - spotMargin, y: bottom})
			} else if c := w.climbs[id]; c != nil && level(bottom, c.to.Y) {
				from, to = max(from, c.to.X), min(to, c.to.X+c.to.W)
				if to-from > 2*spotMargin {
					high = append(high, spot{mapID: id, from: from + spotMargin, to: to - spotMargin, y: bottom, climb: c})
				}
			}
		}
	}
	slices.SortStableFunc(ground, func(a, b spot) int { return cmp.Compare(b.to-b.from, a.to-a.from) })
	return append(ground, high...)
}

// levelSpot is where a bot of a level hunts for EXP: on the map of the strongest normal mob under its level, or at the first, so it seldom dies, the richer on a tie; across the ground of every mob there it may hunt, so the whole map's mobs feed a party that shares their EXP.
func (w *world) levelSpot(lvl int) (spot, bool) {
	top := max(lvl-1, 1)
	var best *gamedata.Mob
	var at spot
	for _, m := range w.data.Mobs() {
		if m.Elite || m.Level > top || (best != nil && (m.Level < best.Level || (m.Level == best.Level && m.Exp <= best.Exp))) {
			continue
		}
		if s := w.spots(m.ID); len(s) > 0 && s[0].climb == nil {
			best, at = &m, s[0]
		}
	}
	if best == nil {
		return spot{}, false
	}
	for _, m := range w.data.Mobs() {
		if m.Elite || m.Level > top {
			continue
		}
		for _, s := range w.spots(m.ID) {
			if s.climb == nil && s.mapID == at.mapID && level(s.y, at.y) {
				at.from, at.to = min(at.from, s.from), max(at.to, s.to)
			}
		}
	}
	return at, true
}

// preyLevel is how far above or below a spot's level a mob may stand and still be on it.
const preyLevel = 40.0

// nearest is the closest mob of a prey kind, or of any with no prey named, standing on a spot, between its ends; the lower ID on a tie, so the choice holds.
func nearest(mobs map[uint32]*clientv1.MobView, s spot, prey []string, x float64) *clientv1.MobView {
	var best *clientv1.MobView
	for _, m := range mobs {
		if (prey != nil && !slices.Contains(prey, m.GetType())) || math.Abs(m.GetY()-s.y) > preyLevel || m.GetX() < s.from-spotMargin || m.GetX() > s.to+spotMargin {
			continue
		}
		d, bd := math.Abs(m.GetX()-x), math.Inf(1)
		if best != nil {
			bd = math.Abs(best.GetX() - x)
		}
		if d < bd || (d == bd && m.GetId() < best.GetId()) {
			best = m
		}
	}
	return best
}

// levelPrey are the mobs a bot of a level hunts for EXP: the normal ones under its level, or at the first.
func (w *world) levelPrey(lvl int) []string {
	var out []string
	for _, m := range w.data.Mobs() {
		if !m.Elite && m.Level <= max(lvl-1, 1) {
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
func (w *world) next(future gamedata.Class, p plan, climbFailed bool) goal {
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
					return w.huntFor(o.Target, id, climbFailed, hunt)
				}
			case gamedata.Collect:
				if p.holding(o.Target) < o.Count {
					for _, mob := range w.droppers(o.Target) {
						if g := w.huntFor(mob, id, climbFailed, nil); g.why != "" {
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

// huntFor hunts a mob for a quest at its first reachable spot; with none, or its only climb failed, it falls back, or with no fallback gives the empty goal.
func (w *world) huntFor(mob, quest string, climbFailed bool, fallback func(string) goal) goal {
	for _, s := range w.spots(mob) {
		if s.climb == nil || !climbFailed {
			return goal{kind: huntGoal, spot: s, prey: []string{mob}, why: mob + " for " + quest}
		}
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

// statPoints spends unspent points on what the promotion asks of own stats first, then by the class's split in whole splits; the rest waits for the next level.
func statPoints(unspent int, own, need, split gamedata.Stats) gamedata.Stats {
	var out gamedata.Stats
	fill := func(spent *int, have, want int) {
		k := min(max(want-have, 0), unspent)
		*spent += k
		unspent -= k
	}
	fill(&out.STR, own.STR, need.STR)
	fill(&out.DEX, own.DEX, need.DEX)
	fill(&out.INT, own.INT, need.INT)
	fill(&out.AGI, own.AGI, need.AGI)
	fill(&out.VIT, own.VIT, need.VIT)
	if per := split.Total(); per > 0 {
		k := unspent / per
		out.STR += k * split.STR
		out.DEX += k * split.DEX
		out.INT += k * split.INT
		out.AGI += k * split.AGI
		out.VIT += k * split.VIT
	}
	return out
}

// skillPoints puts unspent skill points into what the promotion asks of each skill first, then all the rest into rest.
func skillPoints(unspent int32, invested map[string]int32, need map[string]int, rest string) map[string]int32 {
	out := map[string]int32{}
	for _, skill := range slices.Sorted(maps.Keys(need)) {
		k := min(max(int32(need[skill])-invested[skill], 0), unspent)
		if k > 0 {
			out[skill] = k
			unspent -= k
		}
	}
	if unspent > 0 && rest != "" {
		out[rest] += unspent
	}
	return out
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
