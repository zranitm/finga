package instance

import (
	"bytes"
	"iter"
	"math"
	"slices"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// interest is which of a map's cells each player is sent (large-regions.md section 5); a map one block holds from every cell is sent whole, with none of it kept.
type interest struct {
	in      gamedata.Interest
	nx, nz  int
	whole   bool
	where   map[body]int
	cells   [][]body
	watches map[kernel.CharacterID]*watch
	leavers []kernel.CharacterID // the players who left since the last cut

	// The frame being cut: the bodies that came to a cell, those that left the map from theirs, and each cell's parts, -1 for none.
	crossed []body
	gone    []left
	touched []int
	changes []int
	full    []int
	boss    int

	part, to, ends []int // file's, kept to fill again
}

// body is a player or a mob on the map.
type body struct {
	kind   bodyKind
	id     uint64
	player kernel.CharacterID
}

type bodyKind uint8

const (
	playerBody bodyKind = iota
	mobBody
)

func playerOf(id kernel.CharacterID) body { return body{kind: playerBody, player: id} }
func mobOf(id uint64) body                { return body{kind: mobBody, id: id} }

// left is a body taken off the map this tick, and the cell it was in.
type left struct {
	b    body
	cell int
}

// watch is what a player has been sent: the cell their block was round at the last frame, and the bodies in the block they've seen.
type watch struct {
	cell int
	seen map[body]struct{}
}

func newInterest(in gamedata.Interest, sizeX, sizeZ float64) *interest {
	nx := max(1, int(math.Ceil(sizeX/in.Cell)))
	nz := max(1, int(math.Ceil(sizeZ/in.Cell)))
	n := &interest{
		in: in, nx: nx, nz: nz, whole: nx-1 <= in.Side && nz-1 <= min(in.Behind, in.Ahead),
		where: map[body]int{}, cells: make([][]body, nx*nz), watches: map[kernel.CharacterID]*watch{},
		changes: make([]int, nx*nz), full: make([]int, nx*nz), boss: -1,
	}
	for c := range n.changes {
		n.changes[c], n.full[c] = -1, -1
	}
	return n
}

// of is the cell (x, z) lies in; the cells at the map's edges hold what's past them.
func (n *interest) of(x, z float64) int {
	col := min(max(int(math.Floor(x/n.in.Cell)), 0), n.nx-1)
	row := min(max(int(math.Floor(z/n.in.Cell)), 0), n.nz-1)
	return row*n.nx + col
}

// sees is whether a player whose block is round cell c is sent cell d: within side columns, and from behind rows south to ahead rows north, as the camera faces north. No cell is -1.
func (n *interest) sees(c, d int) bool {
	if d < 0 {
		return false
	}
	dc, dr := d%n.nx-c%n.nx, d/n.nx-c/n.nx
	return dc >= -n.in.Side && dc <= n.in.Side && dr >= -n.in.Behind && dr <= n.in.Ahead
}

// block is the cells a player whose block is round c is sent, ascending.
func (n *interest) block(c int) iter.Seq[int] {
	return func(yield func(int) bool) {
		col, row := c%n.nx, c/n.nx
		for r := max(row-n.in.Behind, 0); r <= min(row+n.in.Ahead, n.nz-1); r++ {
			for k := max(col-n.in.Side, 0); k <= min(col+n.in.Side, n.nx-1); k++ {
				if !yield(r*n.nx + k) {
					return
				}
			}
		}
	}
}

// place puts b in the cell at (x, z), noting it crossed when it's new there; it returns the cell.
func (n *interest) place(b body, x, z float64) int {
	c := n.of(x, z)
	old, on := n.where[b]
	if on && old == c {
		return c
	}
	if on {
		n.cells[old] = slices.DeleteFunc(n.cells[old], func(o body) bool { return o == b })
	}
	n.where[b] = c
	n.cells[c] = append(n.cells[c], b)
	n.crossed = append(n.crossed, b)
	return c
}

// lift takes b off the map; its last cell, false if it wasn't on it.
func (n *interest) lift(b body) (int, bool) {
	c, on := n.where[b]
	if !on {
		return 0, false
	}
	n.cells[c] = slices.DeleteFunc(n.cells[c], func(o body) bool { return o == b })
	delete(n.where, b)
	n.gone = append(n.gone, left{b, c})
	return c, true
}

// cellOf is where the entity r stands, or stood as it left the map this tick; -1 for neither.
func (n *interest) cellOf(r model.EntityRef) int {
	b := mobOf(r.Mob)
	if r.Player != (kernel.CharacterID{}) {
		b = playerOf(r.Player)
	}
	if c, on := n.where[b]; on {
		return c
	}
	for _, g := range n.gone {
		if g.b == b {
			return g.cell
		}
	}
	return -1
}

// begin clears the last frame's cut.
func (n *interest) begin() {
	for _, c := range n.touched {
		n.changes[c], n.full[c] = -1, -1
	}
	n.touched, n.crossed, n.gone, n.boss = n.touched[:0], n.crossed[:0], n.gone[:0], -1
}

// changesIn is cell c's changes part, made empty the first time.
func (n *interest) changesIn(f *model.Frame, c int) *model.Part {
	if n.changes[c] < 0 {
		if n.full[c] < 0 {
			n.touched = append(n.touched, c)
		}
		n.changes[c] = len(f.Parts)
		f.Parts = append(f.Parts, model.Part{})
	}
	return &f.Parts[n.changes[c]]
}

// cut files the tick's changes in the cells they happened in, a part each, a body leaving the map in the cell the last frame left it in, and the field boss in the map's part, which every player is sent.
func (i *Instance) cut(players []model.PlayerView, mobs []model.MobView, removedMobs []uint64) *model.Frame {
	n := i.interest
	removed := n.departed(i)
	if n.whole {
		return &model.Frame{Tick: i.tick, Parts: []model.Part{{Players: players, Mobs: mobs, Removed: removed, RemovedMobs: removedMobs}}}
	}
	n.begin()
	f := &model.Frame{Tick: i.tick}
	for _, id := range removed {
		if c, on := n.lift(playerOf(id)); on {
			part := n.changesIn(f, c)
			part.Removed = append(part.Removed, id)
		}
	}
	leave := func(id uint64) bool {
		c, on := n.lift(mobOf(id))
		if on {
			part := n.changesIn(f, c)
			part.RemovedMobs = append(part.RemovedMobs, id)
		}
		return on
	}
	var came []uint64
	for _, id := range removedMobs {
		if !leave(id) {
			came = append(came, id)
		}
	}
	file(n, f, players, func(v model.PlayerView) (body, float64, float64) { return playerOf(v.ID), v.State.X, v.State.Z }, func(p *model.Part) *[]model.PlayerView { return &p.Players })
	file(n, f, mobs, func(v model.MobView) (body, float64, float64) { return mobOf(v.ID), v.X, v.Z }, func(p *model.Part) *[]model.MobView { return &p.Mobs })
	for _, v := range mobs {
		if i.boss != nil && v.ID == i.boss.id {
			n.boss = len(f.Parts)
			f.Parts = append(f.Parts, model.Part{Boss: &v})
		}
	}
	for _, id := range came {
		leave(id) // placed by its view just now
	}
	return f
}

// file places each body of views in its cell and gives each cell's part its run of them: views, which the cut owns, grouped by part in place, each part's in their order.
func file[V any](n *interest, f *model.Frame, views []V, at func(V) (body, float64, float64), list func(*model.Part) *[]V) {
	part, to, ends := n.part[:0], n.to[:0], n.ends[:0]
	for _, v := range views {
		c := n.place(at(v))
		n.changesIn(f, c)
		part = append(part, n.changes[c])
	}
	ends = append(ends, make([]int, len(f.Parts))...)
	for _, k := range part {
		ends[k]++
	}
	for k, first := 0, 0; k < len(ends); k++ { // where each part's run begins, then, as views take their slots, ends
		ends[k], first = first, first+ends[k]
	}
	for _, k := range part {
		to = append(to, ends[k])
		ends[k]++
	}
	for v := range views { // each swap puts a view in its slot
		for to[v] != v {
			w := to[v]
			views[v], views[w], to[v], to[w] = views[w], views[v], to[w], to[v]
		}
	}
	for k, first := 0, 0; k < len(ends); first, k = ends[k], k+1 {
		if ends[k] > first {
			*list(&f.Parts[k]) = views[first:ends[k]:ends[k]]
		}
	}
	n.part, n.to, n.ends = part, to, ends
}

// fullIn is cell c's full part, made the first time a player comes to see the cell: every body in it.
func (i *Instance) fullIn(f *model.Frame, c int) int {
	n := i.interest
	if n.full[c] >= 0 {
		return n.full[c]
	}
	if n.changes[c] < 0 {
		n.touched = append(n.touched, c)
	}
	var part model.Part
	for _, b := range n.cells[c] {
		switch b.kind {
		case playerBody:
			part.Players = append(part.Players, i.view(i.players[b.player]))
		case mobBody:
			v, _ := i.arena.View(b.id)
			part.Mobs = append(part.Mobs, v)
		}
	}
	n.full[c] = len(f.Parts)
	f.Parts = append(f.Parts, part)
	return n.full[c]
}

// follow moves p's watch to their cell as the frame leaves it: the parts they're sent, a cell new to them whole, added to the tick's list of them, and the bodies they stopped seeing.
func (i *Instance) follow(f *model.Frame, id kernel.CharacterID, all *[]int) ([]int, *model.Unseen) {
	n := i.interest
	start := len(*all)
	if n.whole {
		*all = append(*all, 0)
		return (*all)[start:len(*all):len(*all)], nil
	}
	w := n.watches[id]
	center := n.where[playerOf(id)]
	var unseen []body
	lose := func(b body) {
		delete(w.seen, b)
		unseen = append(unseen, b)
	}
	for _, g := range n.gone {
		if _, s := w.seen[g.b]; s {
			delete(w.seen, g.b)
			if !n.sees(center, g.cell) { // its cell, in the last frame's block, sends the removal with its changes while the block still holds it
				unseen = append(unseen, g.b)
			}
		}
	}
	if center != w.cell {
		for b := range w.seen {
			if !n.sees(center, n.where[b]) {
				lose(b)
			}
		}
	}
	for _, b := range n.crossed {
		c, on := n.where[b]
		if !on {
			continue
		}
		switch _, s := w.seen[b]; {
		case s && !n.sees(center, c):
			lose(b)
		case !s && n.sees(center, c):
			w.seen[b] = struct{}{}
		}
	}
	for c := range n.block(center) {
		if n.sees(w.cell, c) {
			if k := n.changes[c]; k >= 0 {
				*all = append(*all, k)
			}
			continue
		}
		*all = append(*all, i.fullIn(f, c))
		for _, b := range n.cells[c] {
			w.seen[b] = struct{}{}
		}
	}
	if n.boss >= 0 {
		*all = append(*all, n.boss)
	}
	w.cell = center
	return (*all)[start:len(*all):len(*all)], unseenOf(unseen)
}

func unseenOf(bodies []body) *model.Unseen {
	if len(bodies) == 0 {
		return nil
	}
	u := &model.Unseen{}
	for _, b := range bodies {
		switch b.kind {
		case playerBody:
			u.Players = append(u.Players, b.player)
		case mobBody:
			u.Mobs = append(u.Mobs, b.id)
		}
	}
	slices.SortFunc(u.Players, func(a, b kernel.CharacterID) int { return bytes.Compare(a[:], b[:]) })
	slices.Sort(u.Mobs)
	return u
}

// watchFrom starts p's watch, filling their snapshot with what their block holds; on a map nobody watched, it files every body again first, as frames went uncut.
func (i *Instance) watchFrom(p *player, snap *model.Snapshot) {
	n := i.interest
	if n.whole {
		n.watches[p.ID] = &watch{}
		for _, id := range i.order {
			snap.Players = append(snap.Players, i.view(i.players[id]))
		}
		snap.Mobs = i.arena.Views()
		return
	}
	if len(n.watches) == 0 {
		n.refile(i)
	}
	center := n.of(p.state.X, p.state.Z)
	w := &watch{cell: center, seen: map[body]struct{}{playerOf(p.ID): {}}}
	n.watches[p.ID] = w
	for _, id := range i.order {
		if c, on := n.where[playerOf(id)]; id == p.ID || on && n.sees(center, c) { // one who joined this tick is filed, and comes, with the next frame
			snap.Players = append(snap.Players, i.view(i.players[id]))
			w.seen[playerOf(id)] = struct{}{}
		}
	}
	for c := range n.block(center) {
		for _, b := range n.cells[c] {
			if b.kind == mobBody {
				v, _ := i.arena.View(b.id)
				snap.Mobs = append(snap.Mobs, v)
				w.seen[b] = struct{}{}
			}
		}
	}
	if i.boss != nil {
		if v, up := i.arena.View(i.boss.id); up {
			snap.Boss = &v
		}
	}
}

// refile files the map's mobs in their cells again, each cell's by ID; players are filed by their frames, the first after they join.
func (n *interest) refile(i *Instance) {
	clear(n.where)
	for c := range n.cells {
		n.cells[c] = n.cells[c][:0]
	}
	put := func(b body, c int) {
		n.where[b] = c
		n.cells[c] = append(n.cells[c], b)
	}
	for _, v := range i.arena.Views() {
		put(mobOf(v.ID), n.of(v.X, v.Z))
	}
}

// leave ends p's watch as they leave the map, and has the next cut take them off it if a frame ever brought them.
func (n *interest) leave(id kernel.CharacterID, framed bool) {
	delete(n.watches, id)
	if framed {
		n.leavers = append(n.leavers, id)
	}
}

// departed are the players who left since the last cut and aren't back, each once: a rejoin in one tick is an update, and the client applies removals last.
func (n *interest) departed(i *Instance) []kernel.CharacterID {
	var out []kernel.CharacterID
	for _, id := range n.leavers {
		if _, back := i.players[id]; !back && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	n.leavers = n.leavers[:0]
	return out
}

// rest forgets the leavers of a map nobody is on, as nobody is left to tell.
func (n *interest) rest() { n.leavers = n.leavers[:0] }

// place is where an event happens, for those who hear it: the cells of its source and target, either -1 when it has none; anywhere for one the whole map hears.
type place struct {
	source, target int
	anywhere       bool
}

// by is whether a player whose block is round c hears it.
func (h place) by(n *interest, c int) bool {
	return h.anywhere || n.sees(c, h.source) || n.sees(c, h.target)
}

// heard is where each of the tick's events happens: a hit, a swing, a skill or a mob's attack at its source's cell and its target's, so a shot into a block shows its hit and one out of it its shooter's swing; the rest anywhere.
func (i *Instance) heard() []place {
	n := i.interest
	out := make([]place, len(i.events))
	for k, e := range i.events {
		if n.whole {
			out[k] = place{anywhere: true}
			continue
		}
		switch e := e.(type) {
		case model.Damage:
			out[k] = place{source: n.cellOf(e.Source), target: n.cellOf(e.Target)}
		case model.SkillUsed:
			out[k] = place{source: n.cellOf(model.EntityRef{Player: e.Caster}), target: n.cellOf(e.Target)}
		case model.AttackUsed:
			out[k] = place{source: n.cellOf(model.EntityRef{Player: e.Attacker}), target: -1}
		case model.MobAttacked:
			out[k] = place{source: n.cellOf(model.EntityRef{Mob: e.Mob}), target: n.of(e.ToX, e.ToZ)}
		default:
			out[k] = place{anywhere: true}
		}
	}
	return out
}
