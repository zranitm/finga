package protocol

import (
	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/quant"
)

// Expander turns a connection's frames back into whole views, as a client reads them: each step is applied to the last view held of its body, so a reader of Delta.Players and Delta.Mobs sees every changed body whole, whether the server sent it whole or as a step.
type Expander struct {
	me      string
	mySlot  uint32
	players map[uint32]*clientv1.PlayerView
	slots   map[string]uint32 // the slot each player held is under
	mobs    map[uint32]*clientv1.MobView
}

// NewExpander is an expander for the player of that id, whose own steps it leaves out: their own view comes whole.
func NewExpander(me string) *Expander {
	return &Expander{me: me, players: map[uint32]*clientv1.PlayerView{}, slots: map[string]uint32{}, mobs: map[uint32]*clientv1.MobView{}}
}

// Snapshot takes a map's join: the views it holds, from which the steps that follow run.
func (e *Expander) Snapshot(s *clientv1.Snapshot) {
	clear(e.players)
	clear(e.slots)
	clear(e.mobs)
	e.mySlot = 0
	for _, v := range s.Players {
		e.hold(v)
	}
	for _, m := range s.Mobs {
		e.mobs[m.Id] = m
	}
}

// Delta puts the frame's steps into its Players and Mobs, as the views they say, and empties them.
func (e *Expander) Delta(d *clientv1.Delta) {
	for _, v := range d.Players {
		e.hold(v)
	}
	for _, m := range d.Mobs {
		e.mobs[m.Id] = m
	}
	for _, s := range d.Steps {
		if s.Slot == e.mySlot && e.mySlot != 0 {
			continue
		}
		if v, ok := e.players[s.Slot]; ok {
			v = steppedPlayer(v, s)
			e.players[s.Slot] = v
			d.Players = append(d.Players, v)
		}
	}
	for _, s := range d.MobSteps {
		if m, ok := e.mobs[s.Id]; ok {
			m = steppedMob(m, s)
			e.mobs[s.Id] = m
			d.Mobs = append(d.Mobs, m)
		}
	}
	d.Steps, d.MobSteps = nil, nil
	for _, id := range d.RemovedMobs {
		delete(e.mobs, id)
	}
	for _, id := range d.RemovedPlayers {
		e.forget(id)
	}
	for _, id := range d.GetUnseen().GetPlayers() {
		e.forget(id)
	}
	for _, id := range d.GetUnseen().GetMobs() {
		delete(e.mobs, id)
	}
}

func (e *Expander) forget(id string) {
	if id != e.me {
		delete(e.players, e.slots[id])
		delete(e.slots, id)
	}
}

func (e *Expander) hold(v *clientv1.PlayerView) {
	e.players[v.Slot] = v
	e.slots[v.Id] = v.Slot
	if v.Id == e.me {
		e.mySlot = v.Slot
	}
}

// steppedPlayer is v after s: its place moved by whole centimetres from where v's rounds to, the rest as s says.
func steppedPlayer(v *clientv1.PlayerView, s *clientv1.PlayerStep) *clientv1.PlayerView {
	out := proto.Clone(v).(*clientv1.PlayerView)
	out.X, out.Y, out.Z = quant.Metres(quant.Position(v.X)+s.Dx), quant.Metres(quant.Position(v.Y)+s.Dy), quant.Metres(quant.Position(v.Z)+s.Dz)
	if s.Vx != nil {
		out.Vx = quant.MetresPerSecond(s.GetVx())
	}
	if s.Vy != nil {
		out.Vy = quant.MetresPerSecond(s.GetVy())
	}
	if s.Vz != nil {
		out.Vz = quant.MetresPerSecond(s.GetVz())
	}
	if s.Hx != nil {
		out.Hx = quant.Unit(s.GetHx())
	}
	if s.Hz != nil {
		out.Hz = quant.Unit(s.GetHz())
	}
	if s.OnGround != nil {
		out.OnGround = s.GetOnGround()
	}
	if s.Hp != nil {
		out.Hp = s.GetHp()
	}
	if s.Sprint != nil {
		out.Sprint = s.GetSprint()
	}
	if s.Held != nil {
		out.Held = s.GetHeld()
	}
	if s.Aim != nil {
		out.Aim = s.GetAim()
	}
	return out
}

func steppedMob(m *clientv1.MobView, s *clientv1.MobStep) *clientv1.MobView {
	out := proto.Clone(m).(*clientv1.MobView)
	out.X, out.Y, out.Z = quant.Metres(quant.Position(m.X)+s.Dx), quant.Metres(quant.Position(m.Y)+s.Dy), quant.Metres(quant.Position(m.Z)+s.Dz)
	if s.Yaw != nil {
		out.Yaw = quant.Radians(s.GetYaw())
	}
	if s.Hp != nil {
		out.Hp = s.GetHp()
	}
	if s.Pose != nil {
		out.Pose = s.GetPose()
	}
	return out
}
