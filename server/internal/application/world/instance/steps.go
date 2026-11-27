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

// crosses is whether b, at (x, z), is new to the cell it's in: a player seeing that cell has never been sent it.
func (n *interest) crosses(b body, x, z float64) bool {
	if n.whole {
		return false
	}
	c, on := n.where[b]
	return !on || c != n.of(x, z)
}

// stepPlayers sorts the tick's changed views into the full views and the steps they go out as, and keeps each as the baseline of the next.
func (i *Instance) stepPlayers(views []model.PlayerView) (full []model.PlayerView, steps []model.PlayerStep) {
	for _, v := range views {
		p := i.players[v.ID]
		now := sentPlayerOf(v)
		old := p.sent
		p.sent = now
		if old == nil || i.interest.crosses(playerOf(v.ID), v.State.X, v.State.Z) || !sameButMotion(v, old.view) {
			full = append(full, v)
			continue
		}
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
		steps = append(steps, st)
	}
	return full, steps
}

// stepMobs is stepPlayers for the tick's changed mobs; a mob removed this tick keeps no baseline.
func (i *Instance) stepMobs(views []model.MobView, removed []uint64) (full []model.MobView, steps []model.MobStep) {
	for _, v := range views {
		now := sentMobOf(v)
		old, had := i.mobSent[v.ID]
		i.mobSent[v.ID] = now
		if !had || i.interest.crosses(mobOf(v.ID), v.X, v.Z) || !sameMobButMotion(v, old.view) {
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
		steps = append(steps, st)
	}
	for _, id := range removed {
		delete(i.mobSent, id)
	}
	return full, steps
}

// told keeps a view sent whole as the baseline of the steps after it: a snapshot or a cell's full part shows a body as it stands, which a tick no one watched may have changed from the last frame's.
func (i *Instance) toldPlayer(v model.PlayerView) {
	if p, ok := i.players[v.ID]; ok {
		p.sent = sentPlayerOf(v)
	}
}

func (i *Instance) toldMob(v model.MobView) { i.mobSent[v.ID] = sentMobOf(v) }

// ownViews are the changed players' views as they are, kept before the cut files them in parts: each player is sent their own whole.
func ownViews(views []model.PlayerView) map[kernel.CharacterID]model.PlayerView {
	own := make(map[kernel.CharacterID]model.PlayerView, len(views))
	for _, v := range views {
		own[v.ID] = v
	}
	return own
}
