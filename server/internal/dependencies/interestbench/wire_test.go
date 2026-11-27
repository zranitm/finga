package interestbench

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	"finga/internal/handler/gateway/ws/push"
	gwmodel "finga/internal/model/gateway"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/protocol"
	"finga/internal/shared/quant"
	"finga/internal/shared/sim"
	"finga/pkg/wsconn"
)

// wire is a map stepped by hand with its players as the gateway's clients: each frame goes through the push package's encoder and the client's codec, and each client reads it through protocol.Expander, the code the bots and the e2e kit read frames with.
type wire struct {
	inst    *instance.Instance
	frame   *model.Frame
	to      []model.Recipient
	snaps   map[kernel.CharacterID]model.Snapshot
	clients map[kernel.CharacterID]*client
	order   []*client            // by join, so a seed runs the same every time
	away    []kernel.CharacterID // handed off to another map: a respawn in a town elsewhere
	r       *rand.Rand
	tick    uint64
	seq     uint32
}

// client is what one player holds of the map, from their snapshot and every frame since.
type client struct {
	id      kernel.CharacterID
	key     sim.Input
	ex      *protocol.Expander
	players map[string]*clientv1.PlayerView
	mobs    map[uint32]*clientv1.MobView
}

func (w *wire) SendFrame(f *model.Frame, to []model.Recipient) bool {
	w.frame, w.to = f, slices.Clone(to)
	return true
}

type wireSink struct {
	w  *wire
	id kernel.CharacterID
}

func (k wireSink) Send(e model.Event) bool {
	switch e := e.(type) {
	case model.Snapshot:
		k.w.snaps[k.id] = e
	case model.Respawned:
		if e.MapID != "wire" { // the map hands the dead off to a town's, as a region does: they are no longer on this map
			k.w.away = append(k.w.away, k.id)
		}
	}
	return true
}

func (k wireSink) Stream() instance.Stream { return k.w }

// push is a message as the outbox sends it and the client decodes it.
func (w *wire) push(t testing.TB, frames *push.Frames, d gwmodel.Push, tick uint64) any {
	t.Helper()
	conn := &discard{}
	push.NewOutbox(conn, conn, frames).Deliver(gwmodel.Delivery{Tick: tick, Push: d})
	b, err := protocol.ServerCodec(wsconn.Binary).Encode(conn.last)
	if err != nil {
		t.Fatal(err)
	}
	m, err := protocol.ClientCodec(wsconn.Binary).Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	return m.Data
}

// step runs a tick, each player first holding their key, one in sixty a new one, and some swinging; then every client reads its frame.
func (w *wire) step(t testing.TB) {
	w.tick++
	w.seq++
	w.frame, w.to = nil, nil
	for _, c := range w.order {
		id := c.id
		if w.r.IntN(60) == 0 {
			c.key = sim.Input{Left: w.r.IntN(3) == 0, Right: w.r.IntN(3) == 0, Up: w.r.IntN(2) == 0, Down: w.r.IntN(3) == 0}
		}
		_ = w.inst.Input(id, w.seq, c.key, 0)
		if w.r.IntN(20) == 0 {
			_ = w.inst.Attack(id, float32(w.r.Float64()*2*math.Pi), model.EntityRef{})
		}
	}
	w.inst.Step(w.tick, worldtest.TickInterval)
	for _, id := range w.away {
		if c := w.clients[id]; c != nil {
			w.leave(t, c)
		}
	}
	w.away = w.away[:0]
	frames := push.NewFrames()
	for id, s := range w.snaps {
		snap := gwmodel.Snapshot{}
		for _, v := range s.Players {
			snap.Players = append(snap.Players, playerView(v))
		}
		for _, v := range s.Mobs {
			snap.Mobs = append(snap.Mobs, mobViewOf(v))
		}
		got := w.push(t, frames, snap, w.tick).(*clientv1.Snapshot)
		c := &client{id: id, ex: protocol.NewExpander(id.String()), players: map[string]*clientv1.PlayerView{}, mobs: map[uint32]*clientv1.MobView{}}
		c.ex.Snapshot(got)
		for _, v := range got.Players {
			c.players[v.Id] = v
		}
		for _, v := range got.Mobs {
			c.mobs[v.Id] = v
		}
		w.clients[id] = c
		w.order = append(w.order, c)
		delete(w.snaps, id)
	}
	if w.frame == nil { // a map with nobody on it builds no frame
		return
	}
	parts := gatewayFrame(w.frame)
	for _, r := range w.to {
		c := w.clients[r.Character]
		if c == nil {
			continue
		}
		d := w.push(t, frames, gwmodel.Delta{Frame: parts, Parts: r.Parts, AckSeq: r.AckSeq, Unseen: unseen(r.Unseen), Own: own(r)}, w.frame.Tick).(*clientv1.Delta)
		c.ex.Delta(d)
		for _, v := range d.Players {
			c.players[v.Id] = v
		}
		for _, v := range d.Mobs {
			c.mobs[v.Id] = v
		}
		for _, id := range d.RemovedPlayers {
			delete(c.players, id)
		}
		for _, id := range d.RemovedMobs {
			delete(c.mobs, id)
		}
		for _, id := range d.GetUnseen().GetPlayers() {
			delete(c.players, id)
		}
		for _, id := range d.GetUnseen().GetMobs() {
			delete(c.mobs, id)
		}
	}
}

// join adds a player and steps until they are in and have their snapshot.
func (w *wire) join(t testing.TB, p model.Player) *client {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- w.inst.Join(context.Background(), p, wireSink{w, p.ID}) }()
	for w.clients[p.ID] == nil { // the join's snapshot comes in a frame, whenever the goroutine is scheduled: stepping by it keeps a seed's run the same
		w.step(t)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return w.clients[p.ID]
}

func (w *wire) leave(t testing.TB, c *client) {
	t.Helper()
	if err := w.inst.Leave(c.id, "", nil); err != nil {
		t.Fatal(err)
	}
	delete(w.clients, c.id)
	w.order = slices.DeleteFunc(w.order, func(o *client) bool { return o == c })
}

// onTheWire is a view with its place, speed, heading and yaw at the whole units the wire gives them, so a view read from steps and one sent whole compare.
func onTheWire(v proto.Message) proto.Message {
	switch v := proto.Clone(v).(type) {
	case *clientv1.PlayerView:
		v.X, v.Y, v.Z = quant.Metres(quant.Position(v.X)), quant.Metres(quant.Position(v.Y)), quant.Metres(quant.Position(v.Z))
		v.Vx, v.Vy, v.Vz = quant.MetresPerSecond(quant.Speed(v.Vx)), quant.MetresPerSecond(quant.Speed(v.Vy)), quant.MetresPerSecond(quant.Speed(v.Vz))
		v.Hx, v.Hz = quant.Unit(quant.Heading(v.Hx)), quant.Unit(quant.Heading(v.Hz))
		return v
	case *clientv1.MobView:
		v.X, v.Y, v.Z = quant.Metres(quant.Position(v.X)), quant.Metres(quant.Position(v.Y)), quant.Metres(quant.Position(v.Z))
		v.Yaw = quant.Radians(quant.Yaw(v.Yaw))
		return v
	}
	return v
}

// Players walk and fight a map while others join and leave beside them: every 25 ticks a player joins on a client's spot, and what that client holds of every body, read from the wire's frames through the Expander, is in every field what the join was sent, to the whole units the wire rounds to. A body handed out whole to one player must not leave another's baseline wrong.
func TestEveryClientHoldsOnTheWireWhatAJoinIsSent(t *testing.T) {
	for seed := range uint64(6) {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			data := interestData(t, 1, 3, 3)
			r := rand.New(rand.NewPCG(seed, 17))
			const size = 256.0
			kinds := []string{"green_slime", "wild_boar", "cave_bat", "orange_mushroom"}
			var spawns []gamedata.Spawn
			for range 50 {
				spawns = append(spawns, gamedata.Spawn{Mob: kinds[r.IntN(len(kinds))], Max: 2, Respawn: 5 * time.Second, Radius: 8, At: gamedata.Point{X: 10 + r.Float64()*(size-20), Z: 10 + r.Float64()*(size-20)}})
			}
			m := worldtest.Gridded(t, &gamedata.Map{
				ID: "wire", Kind: gamedata.Hunting, Town: "willowmere", Geometry: worldtest.Flat(size), Spawns: spawns, PlayerSpawn: gamedata.Point{X: size / 2, Z: size / 2},
			})
			w := &wire{inst: worldtest.NewInstance(m, data, nil, worldtest.Options{}), snaps: map[kernel.CharacterID]model.Snapshot{}, clients: map[kernel.CharacterID]*client{}, r: r}
			named := 0
			fresh := func() model.Player {
				named++
				p := worldtest.NewPlayer(fmt.Sprintf("P%d", named), 20+r.Float64()*(size-40), 20+r.Float64()*(size-40))
				p.Level, p.HP = 20, 600 // mobs hurt it, and some walks end dead
				return p
			}
			for range 6 {
				w.join(t, fresh())
			}
			checks := 0
			for n := range 600 {
				w.step(t)
				for len(w.order) < 4 { // the dead are handed off, and others take their place
					w.join(t, fresh())
				}
				if r.IntN(150) == 0 { // one leaves and another comes in its place
					w.leave(t, w.order[r.IntN(len(w.order))])
					w.join(t, fresh())
				}
				if n%25 != 24 {
					continue
				}
				c := w.order[r.IntN(len(w.order))]
				at := c.players[c.id.String()]
				obs := worldtest.NewPlayer("Observer", at.X, at.Z)
				o := w.join(t, obs)
				if w.clients[c.id] != c || cellOf(c.players[c.id.String()]) != cellOf(at) { // the client was handed off, or crossed a cell line, while the join went in: its block is no longer the join's
					if r.IntN(2) == 0 {
						w.leave(t, o)
					}
					continue
				}
				checks++
				mine, theirs := maps(c.players, obs.ID.String()), maps(o.players, obs.ID.String())
				if len(mine) != len(theirs) || len(c.mobs) != len(o.mobs) {
					t.Fatalf("tick %d: a client holds %d players and %d mobs; a join on its spot is sent %d and %d", w.tick, len(mine), len(c.mobs), len(theirs), len(o.mobs))
				}
				for id, v := range theirs {
					if h, ok := mine[id]; !ok || !proto.Equal(onTheWire(h), onTheWire(v)) {
						t.Fatalf("tick %d: a client holds player %s as %v (%v); a join is sent %v", w.tick, v.Name, h, ok, v)
					}
				}
				for id, v := range o.mobs {
					if h, ok := c.mobs[id]; !ok || !proto.Equal(onTheWire(h), onTheWire(v)) {
						t.Fatalf("tick %d: a client holds mob %d as %v (%v); a join is sent %v", w.tick, id, h, ok, v)
					}
				}
				if r.IntN(2) == 0 { // some stay a while, so their cells' parts are theirs too
					w.leave(t, o)
				}
			}
			if checks == 0 {
				t.Fatal("no checks ran")
			}
		})
	}
}

// maps is the views less the player of that id.
func maps(m map[string]*clientv1.PlayerView, without string) map[string]*clientv1.PlayerView {
	out := make(map[string]*clientv1.PlayerView, len(m))
	for id, v := range m {
		if id != without {
			out[id] = v
		}
	}
	return out
}

// cellOf is the 32 m cell a player stands in.
func cellOf(v *clientv1.PlayerView) [2]int {
	return [2]int{int(math.Floor(v.X / 32)), int(math.Floor(v.Z / 32))}
}
