package instance

import (
	"slices"

	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/internal/shared/quant"
)

// A body's change goes out as a step when only where it is and how it moves changed; the client reads the step back against the last view of the body it took, which is the baseline kept here: the view last sent, and its place and way in whole units. A full view goes when anything else changed, when no baseline is kept yet, or when the body came to a cell: a player who sees that cell has never been sent it, and a step would have nothing to read against (large-regions.md section 5).
type sentPlayer struct {
	view                        model.PlayerView
	x, y, z, vx, vy, vz, hx, hz int32
}

type sentMob struct {
	view         model.MobView
	x, y, z, yaw int32
}

func sentPlayerOf(v model.PlayerView) *sentPlayer {
	s := v.State
	return &sentPlayer{view: v, x: quant.Position(s.X), y: quant.Position(s.Y), z: quant.Position(s.Z), vx: quant.Speed(s.VX), vy: quant.Speed(s.VY), vz: quant.Speed(s.VZ), hx: quant.Heading(s.HX), hz: quant.Heading(s.HZ)}
}

func sentMobOf(v model.MobView) sentMob {
	return sentMob{view: v, x: quant.Position(v.X), y: quant.Position(v.Y), z: quant.Position(v.Z), yaw: quant.Yaw(v.Yaw)}
}

// sameButMotion is whether a and b differ in nothing a step can say: where the player is and how it moves, its HP, aim, sprint and hold.
func sameButMotion(a, b model.PlayerView) bool {
	return a.ID == b.ID && a.Slot == b.Slot && a.Name == b.Name && a.Class == b.Class && a.Level == b.Level && a.MaxHP == b.MaxHP && a.Dead == b.Dead &&
		a.Invulnerable == b.Invulnerable && a.Dodging == b.Dodging && a.Shield == b.Shield && a.Team == b.Team && a.Bot == b.Bot && a.Look == b.Look &&
		a.State.Move == b.State.Move && a.State.Slow == b.State.Slow && a.State.Walk == b.State.Walk && slices.Equal(a.Statuses, b.Statuses)
}

func sameMobButMotion(a, b model.MobView) bool {
	return a.ID == b.ID && a.Type == b.Type && a.MaxHP == b.MaxHP && slices.Equal(a.Statuses, b.Statuses)
}

// A body that came to another cell goes out as its step to those who held it in the cell it left, and whole to the rest, who see its new cell and not the old: a shifted body keeps both.
type shiftedPlayer struct {
	view model.PlayerView
	step model.PlayerStep
}

type shiftedMob struct {
	view model.MobView
	step model.MobStep
}

type shift uint8

const (
	stays   shift = iota // in the cell it's filed in, or on a map sent whole
	unfiled              // in no cell yet: a player seeing its cell has never been sent it
	moves                // came to another cell
)

// shiftOf is how b, at (x, z), stands to the cell it's filed in.
func (n *interest) shiftOf(b body, x, z float64) shift {
	if n.whole {
		return stays
	}
	c, on := n.where[b]
	switch {
	case !on:
		return unfiled
	case c != n.of(x, z):
		return moves
	}
	return stays
}

// stepPlayers sorts the tick's changed views into the full views and the steps they go out as, and keeps each as the baseline of the next.
func (i *Instance) stepPlayers(views []model.PlayerView) (full []model.PlayerView, steps []model.PlayerStep, shifted []shiftedPlayer) {
	for _, v := range views {
		p := i.players[v.ID]
		now := sentPlayerOf(v)
		old := p.sent
		at := i.interest.shiftOf(playerOf(v.ID), v.State.X, v.State.Z)
		if old == nil || at == unfiled || !sameButMotion(v, old.view) {
			p.sent, p.waiting = now, false
			full = append(full, v)
			continue
		}
		if at == stays && motionOnly(v, old.view) && (i.tick+uint64(v.Slot))%2 != 0 && i.interest.crowded(i.interest.of(v.State.X, v.State.Z), i.tick) {
			p.waiting = true // its baseline stays, so the step it goes in is read from where all its watchers hold it
			continue
		}
		p.sent, p.waiting = now, false
		st := model.PlayerStep{ID: v.ID, Slot: v.Slot, X: v.State.X, Z: v.State.Z, DX: now.x - old.x, DY: now.y - old.y, DZ: now.z - old.z}
		if now.vx != old.vx {
			st.VX = new(now.vx)
		}
		if now.vy != old.vy {
			st.VY = new(now.vy)
		}
		if now.vz != old.vz {
			st.VZ = new(now.vz)
		}
		if now.hx != old.hx {
			st.HX = new(now.hx)
		}
		if now.hz != old.hz {
			st.HZ = new(now.hz)
		}
		if v.State.OnGround != old.view.State.OnGround {
			st.OnGround = new(v.State.OnGround)
		}
		if v.HP != old.view.HP {
			st.HP = new(int32(v.HP)) //nolint:gosec // HP is a few thousand
		}
		if v.State.Sprint != old.view.State.Sprint {
			st.Sprint = new(int32(v.State.Sprint)) //nolint:gosec // a sprint's count stops at its start, a few hundred sub-steps
		}
		if v.State.Held != old.view.State.Held {
			st.Held = new(int32(v.State.Held)) //nolint:gosec // a hold is a few hundred sub-steps
		}
		if v.Aim != old.view.Aim {
			st.Aim = new(v.Aim)
		}
		if at == moves {
			shifted = append(shifted, shiftedPlayer{v, st})
			continue
		}
		steps = append(steps, st)
	}
	return full, steps, shifted
}

// stepMobs is stepPlayers for the tick's changed mobs; a mob removed this tick keeps no baseline.
func (i *Instance) stepMobs(views []model.MobView, removed []uint64) (full []model.MobView, steps []model.MobStep, shifted []shiftedMob) {
	for _, v := range views {
		now := sentMobOf(v)
		old, had := i.mobSent[v.ID]
		i.mobSent[v.ID] = now
		at := i.interest.shiftOf(mobOf(v.ID), v.X, v.Z)
		if at == moves && slices.Contains(removed, v.ID) { // a mob killed as it came is sent whole in the cell it's lifted from
			at = unfiled
		}
		if !had || at == unfiled || !sameMobButMotion(v, old.view) {
			full = append(full, v)
			continue
		}
		st := model.MobStep{ID: v.ID, X: v.X, Z: v.Z, DX: now.x - old.x, DY: now.y - old.y, DZ: now.z - old.z}
		if now.yaw != old.yaw {
			st.Yaw = new(now.yaw)
		}
		if v.HP != old.view.HP {
			st.HP = new(int32(v.HP)) //nolint:gosec // HP is a few thousand
		}
		if v.Pose != old.view.Pose {
			st.Pose = new(v.Pose)
		}
		if at == moves {
			shifted = append(shifted, shiftedMob{v, st})
			continue
		}
		steps = append(steps, st)
	}
	for _, id := range removed {
		delete(i.mobSent, id)
	}
	return full, steps, shifted
}

// shown is a player's view as its watchers hold it: one whose step waits is shown as last sent, so a view handed out whole agrees with their baseline.
func (i *Instance) shown(p *player) model.PlayerView {
	if p.waiting && p.sent != nil {
		return p.sent.view
	}
	return i.view(p)
}

// motionOnly is whether a and b differ in at most where the player is and how it moves: a step that says nothing else may wait for its tick.
func motionOnly(a, b model.PlayerView) bool {
	return a.HP == b.HP && a.Aim == b.Aim && a.State.OnGround == b.State.OnGround && a.State.Sprint == b.State.Sprint && a.State.Held == b.State.Held
}

// toldPlayer is a view handed out whole to one player: a snapshot or a cell's full part shows a body as it stands, which a tick no one framed it in, or a view the clock ends something in, may have changed from what every other watcher holds. The baseline is theirs too, so a view that differs from it is dropped: the body's next frame goes whole to all, and no watcher misses what the join was shown.
func (i *Instance) toldPlayer(v model.PlayerView) {
	if p, ok := i.players[v.ID]; ok && p.sent != nil && !sameSent(p.sent, sentPlayerOf(v)) {
		p.sent = nil
	}
}

func (i *Instance) toldMob(v model.MobView) {
	if old, ok := i.mobSent[v.ID]; ok && !sameMobSent(old, sentMobOf(v)) {
		delete(i.mobSent, v.ID)
	}
}

// sameSent is whether two views of a player are the same in every field a step or a whole view carries, as the wire has them.
func sameSent(a, b *sentPlayer) bool {
	return sameButMotion(a.view, b.view) && a.x == b.x && a.y == b.y && a.z == b.z && a.vx == b.vx && a.vy == b.vy && a.vz == b.vz && a.hx == b.hx && a.hz == b.hz &&
		a.view.HP == b.view.HP && a.view.Aim == b.view.Aim && a.view.State.OnGround == b.view.State.OnGround && a.view.State.Sprint == b.view.State.Sprint && a.view.State.Held == b.view.State.Held
}

func sameMobSent(a, b sentMob) bool {
	return sameMobButMotion(a.view, b.view) && a.x == b.x && a.y == b.y && a.z == b.z && a.yaw == b.yaw && a.view.HP == b.view.HP && a.view.Pose == b.view.Pose
}

// ownViews are the changed players' views as they are, kept before the cut files them in parts: each player is sent their own whole.
func ownViews(views []model.PlayerView) map[kernel.CharacterID]model.PlayerView {
	own := make(map[kernel.CharacterID]model.PlayerView, len(views))
	for _, v := range views {
		own[v.ID] = v
	}
	return own
}
