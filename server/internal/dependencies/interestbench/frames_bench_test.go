// Package interestbench measures a crowded region's frames end to end: the world cutting each tick by interest cell, and each client's Delta as the gateway encodes it (large-regions.md section 5).
package interestbench

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	"finga/internal/handler/transport"
	handlerworld "finga/internal/handler/world"
	gwmodel "finga/internal/model/gateway"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/protocol"
	"finga/internal/shared/sim"
	"finga/pkg/wsconn"
)

const (
	regionSize = 1024.0 // a region, about a kilometre square
	crowd      = 150    // players, spread over it
	quiet      = 20     // players on a quiet channel
	camps      = 100    // of four mobs each
)

// shapes are the blocks of cells compared: a square 7 by 7, and the shipped 7 by 5 (a bigger one holds a shipped region whole, which the loader refuses).
var shapes = []struct {
	name                string
	behind, ahead, side int
}{{"7x7", 3, 3, 3}, {"7x5", 1, 3, 3}}

// A tick of 150 players walking a 1 km region among 400 mobs: what the world spends cutting and sending it, and what each client's Delta costs the gateway, in bytes and allocations.
func BenchmarkACrowdedRegionsFrames(b *testing.B) {
	m := region(b)
	for _, sh := range shapes {
		data := interestData(b, sh.behind, sh.ahead, sh.side)
		s := newScene(b, m, data, crowd)
		s.run(200) // the crowd spreads and the camps wake
		b.Run(sh.name+"/tick", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				s.step()
			}
		})
		b.Run(sh.name+"/clients", func(b *testing.B) {
			s.step()
			frame, to := s.out.frame, s.out.to
			codec := protocol.ServerCodec(wsconn.Binary)
			conn := &discard{}
			bytes := 0
			b.ReportAllocs()
			for b.Loop() {
				o, parts := newOutbox(conn), gatewayFrame(frame)
				bytes = 0
				for _, r := range to {
					o.Deliver(gwmodel.Delivery{Tick: frame.Tick, Push: gwmodel.Delta{Frame: parts, Parts: r.Parts, AckSeq: r.AckSeq, Unseen: unseen(r.Unseen), Own: own(r)}})
					wire, err := codec.Encode(conn.last)
					if err != nil {
						b.Fatal(err)
					}
					bytes += len(wire)
				}
			}
			b.ReportMetric(float64(bytes)/float64(len(to)), "B/client")
		})
	}
}

// A tick of a quiet channel, 20 players on the region among its 400 mobs: about half its cells are watched, and the mobs in the rest sleep.
func BenchmarkAQuietRegionsTick(b *testing.B) {
	s := newScene(b, region(b), worldtest.LoadData(b), quiet)
	s.run(200)
	b.ReportAllocs()
	for b.Loop() {
		s.step()
	}
}

// region is flat ground a kilometre square, with camps of four mobs spread over it.
func region(b *testing.B) *gamedata.Map {
	r := rand.New(rand.NewPCG(5, 8))
	kinds := []string{"green_slime", "wild_boar", "cave_bat", "orange_mushroom"}
	spawns := make([]gamedata.Spawn, camps)
	for k := range spawns {
		at := gamedata.Point{X: 20 + r.Float64()*(regionSize-40), Z: 20 + r.Float64()*(regionSize-40)}
		spawns[k] = gamedata.Spawn{Mob: kinds[k%len(kinds)], Max: 4, Respawn: 10 * time.Second, Radius: 10, At: at}
	}
	return worldtest.Gridded(b, &gamedata.Map{
		ID: "region", Kind: gamedata.Hunting, Town: "willowmere", Geometry: worldtest.Flat(regionSize), Spawns: spawns,
		PlayerSpawn: gamedata.Point{X: regionSize / 2, Z: regionSize / 2},
	})
}

// scene is a map stepped by hand, its players each holding a key they change now and then, as walkers do.
type scene struct {
	home *gamedata.Point                   // when set, a player past 45 m of it presses toward it
	at   map[kernel.CharacterID][2]float64 // where each last was, as the latest frame had them
	inst *instance.Instance
	out  *stream
	ids  []kernel.CharacterID
	keys []sim.Input
	r    *rand.Rand
	tick uint64
	seq  uint32
}

func newScene(b *testing.B, m *gamedata.Map, data *gamedata.Data, players int) *scene {
	return newSceneAt(b, m, data, players, func(r *rand.Rand) (float64, float64) {
		return 20 + r.Float64()*(regionSize-40), 20 + r.Float64()*(regionSize-40)
	})
}

// newSceneAt is a scene whose players start where place puts each.
func newSceneAt(b *testing.B, m *gamedata.Map, data *gamedata.Data, players int, place func(*rand.Rand) (float64, float64)) *scene {
	s := &scene{inst: worldtest.NewInstance(m, data, nil, worldtest.Options{}), out: &stream{}, r: rand.New(rand.NewPCG(2, 9)), at: map[kernel.CharacterID][2]float64{}}
	var joined sync.WaitGroup
	for k := range players {
		x, z := place(s.r)
		p := worldtest.NewPlayer(fmt.Sprintf("P%03d", k), x, z)
		p.Level, p.HP = 60, 50000 // walks among mobs all through the run
		s.ids, s.keys = append(s.ids, p.ID), append(s.keys, sim.Input{})
		joined.Go(func() {
			if err := s.inst.Join(context.Background(), p, sink{s.out}); err != nil {
				b.Error(err)
			}
		})
	}
	done := make(chan struct{})
	go func() { joined.Wait(); close(done) }()
	for {
		select {
		case <-done:
			return s
		default:
			s.step()
		}
	}
}

func (s *scene) run(ticks int) {
	for range ticks {
		s.step()
	}
}

// step runs a tick, each player first pressing their key, one in sixty a new one.
func (s *scene) step() {
	s.tick++
	s.seq++
	for k, id := range s.ids {
		if s.r.IntN(60) == 0 {
			s.keys[k] = sim.Input{Left: s.r.IntN(3) == 0, Right: s.r.IntN(3) == 0, Up: s.r.IntN(2) == 0, Down: s.r.IntN(3) == 0}
		}
		key := s.keys[k]
		if a, ok := s.at[id]; ok && s.home != nil {
			if dx, dz := s.home.X-a[0], s.home.Z-a[1]; math.Hypot(dx, dz) > 45 {
				key = sim.Input{Right: dx > 0, Left: dx < 0, Up: dz > 0, Down: dz < 0}
			}
		}
		_ = s.inst.Input(id, s.seq, key, 0)
	}
	s.inst.Step(s.tick, worldtest.TickInterval)
	if s.home != nil && s.out.frame != nil {
		for _, p := range s.out.frame.Parts {
			for _, v := range p.Players {
				s.at[v.ID] = [2]float64{v.State.X, v.State.Z}
			}
		}
	}
}

// stream is one gateway's: it keeps the latest frame and who it went to.
type stream struct {
	frame *model.Frame
	to    []model.Recipient
}

func (s *stream) SendFrame(f *model.Frame, to []model.Recipient) bool {
	s.frame, s.to = f, slices.Clone(to)
	return true
}

type sink struct{ s *stream }

func (sink) Send(model.Event) bool     { return true }
func (k sink) Stream() instance.Stream { return k.s }

// discard is a connection keeping only the last message sent.
type discard struct{ last wsconn.Message }

func (d *discard) ID() string                  { return "c-1" }
func (d *discard) Send(m wsconn.Message) error { d.last = m; return nil }
func (d *discard) Close(int, string)           {}
func (d *discard) MessageIn(string)            {}
func (d *discard) MessageOut(string)           {}
func (d *discard) WelcomeSent(string, int)     {}
func (d *discard) SlowConsumer()               {}
func (d *discard) Unencoded(string)            {}

// newOutbox is a player's outbox with the world's encoders, and so a frame cache of its own, as each tick starts one.
func newOutbox(conn *discard) *transport.Outbox {
	return transport.NewOutbox(conn, conn, slog.New(slog.DiscardHandler), newEncoders())
}

// newEncoders are the world's push encoders, with a frame cache of their own.
func newEncoders() *transport.Encoders {
	enc := transport.NewEncoders()
	if err := handlerworld.Encoders(enc); err != nil {
		panic(err)
	}
	return enc
}

// playerView is v as the gateway's model has it, every field as the world's facade and the adapter copy them.
func playerView(v model.PlayerView) gwmodel.PlayerView {
	st := v.State
	return gwmodel.PlayerView{
		ID: v.ID.String(), Slot: v.Slot, Name: v.Name, Class: v.Class.String(), Level: v.Level, HP: v.HP, MaxHP: v.MaxHP, Dead: v.Dead, Invulnerable: v.Invulnerable,
		X: st.X, Y: st.Y, Z: st.Z, VX: st.VX, VY: st.VY, VZ: st.VZ, HX: st.HX, HZ: st.HZ, OnGround: st.OnGround, Aim: v.Aim, Held: st.Held, Sprint: st.Sprint, Walk: st.Walk,
		Move:    gwmodel.PlayerMove{VX: st.Move.VX, VZ: st.Move.VZ, Left: st.Move.Left, Stop: st.Move.Stop, Through: st.Move.Through, ClearX: st.Move.ClearX, ClearZ: st.Move.ClearZ},
		Dodging: v.Dodging, Shield: v.Shield, Slow: gwmodel.PlayerSlow{Left: st.Slow.Left, Factor: st.Slow.Factor},
		Statuses: statuses(v.Statuses), Team: v.Team, Bot: v.Bot, Look: gwmodel.Look(v.Look),
	}
}

// own is a recipient's own view as the gateway's model has it, nil when it didn't change.
func own(r model.Recipient) *gwmodel.PlayerView {
	if r.Own == nil {
		return nil
	}
	v := playerView(*r.Own)
	return &v
}

// gatewayFrame is f as the gateway's model has it, each view's fields the scene sets as the world's facade and adapter copy them.
func gatewayFrame(f *model.Frame) *gwmodel.Frame {
	out := &gwmodel.Frame{Parts: make([]gwmodel.Part, len(f.Parts))}
	for i, p := range f.Parts {
		g := &out.Parts[i]
		for _, v := range p.Players {
			g.Players = append(g.Players, playerView(v))
		}
		for _, v := range p.Steps {
			g.Steps = append(g.Steps, gwmodel.PlayerStep{Slot: v.Slot, DX: v.DX, DY: v.DY, DZ: v.DZ, VX: v.VX, VY: v.VY, VZ: v.VZ, HX: v.HX, HZ: v.HZ, OnGround: v.OnGround, HP: v.HP, Sprint: v.Sprint, Held: v.Held, Aim: v.Aim})
		}
		for _, v := range p.MobSteps {
			st := gwmodel.MobStep{ID: v.ID, DX: v.DX, DY: v.DY, DZ: v.DZ, Yaw: v.Yaw, HP: v.HP}
			if v.Pose != nil {
				pose := poses[*v.Pose]
				st.Pose = &pose
			}
			g.MobSteps = append(g.MobSteps, st)
		}
		for _, v := range p.Mobs {
			g.Mobs = append(g.Mobs, mobViewOf(v))
		}
		for _, id := range p.Removed {
			g.RemovedPlayers = append(g.RemovedPlayers, id.String())
		}
		g.RemovedMobs = p.RemovedMobs
	}
	return out
}

func mobViewOf(v model.MobView) gwmodel.MobView {
	return gwmodel.MobView{ID: v.ID, Type: v.Type, X: v.X, Y: v.Y, Z: v.Z, Yaw: v.Yaw, HP: v.HP, MaxHP: v.MaxHP, Pose: poses[v.Pose], Statuses: statuses(v.Statuses)}
}

var poses = map[model.MobPose]gwmodel.MobPose{model.Standing: gwmodel.Standing, model.Flinching: gwmodel.Flinching, model.Down: gwmodel.Down, model.Rising: gwmodel.Rising}

func statuses(ss []model.StatusView) []gwmodel.StatusView {
	var out []gwmodel.StatusView
	for _, s := range ss {
		out = append(out, gwmodel.StatusView{ID: s.ID, Ends: s.Ends})
	}
	return out
}

func unseen(u *model.Unseen) *gwmodel.Unseen {
	if u == nil {
		return nil
	}
	out := &gwmodel.Unseen{Mobs: u.Mobs}
	for _, id := range u.Players {
		out.Players = append(out.Players, id.String())
	}
	return out
}

// interestData is the data with the interest block cut as asked, over the stand-ins for the shipped regions, none of which a 7 by 7 block holds whole.
func interestData(b testing.TB, behind, ahead, side int) *gamedata.Data {
	return worldtest.LoadDataWith(b, map[string]func(string) string{"world.yaml": func(s string) string {
		return strings.NewReplacer(
			"  behind: 1\n", fmt.Sprintf("  behind: %d\n", behind),
			"  ahead: 3\n", fmt.Sprintf("  ahead: %d\n", ahead),
			"  side: 3\n", fmt.Sprintf("  side: %d\n", side),
		).Replace(s)
	}})
}

// A tick of 150 players clumped within about 60 m among Sunmeadow-like camps, as the soak's bots were in Sunmeadow: each client's Delta in bytes, split by what's in it, and how many bodies it holds, at 1 and 3 times the mobs a camp holds.
func BenchmarkACrowdInACamp(b *testing.B) {
	for _, perCamp := range []int{5, 15} {
		b.Run(fmt.Sprintf("%d mobs a camp", perCamp), func(b *testing.B) {
			data := interestData(b, 1, 3, 3)
			s := newClumpedScene(b, data, crowd, perCamp)
			s.run(200) // the crowd moves about and the camps wake
			s.step()
			frame, to := s.out.frame, s.out.to
			codec := protocol.ServerCodec(wsconn.Binary)
			conn := &discard{}
			size := func(f *gwmodel.Frame, r model.Recipient, withOwn bool) int {
				o := newOutbox(conn)
				d := gwmodel.Delta{Frame: f, Parts: r.Parts, AckSeq: r.AckSeq, Unseen: unseen(r.Unseen)}
				if withOwn {
					d.Own = own(r)
				}
				o.Deliver(gwmodel.Delivery{Tick: frame.Tick, Push: d})
				wire, err := codec.Encode(conn.last)
				if err != nil {
					b.Fatal(err)
				}
				return len(wire)
			}
			whole := gatewayFrame(frame)
			players, mobs := &gwmodel.Frame{}, &gwmodel.Frame{}
			for _, p := range whole.Parts {
				players.Parts = append(players.Parts, gwmodel.Part{Players: p.Players, Steps: p.Steps, RemovedPlayers: p.RemovedPlayers})
				mobs.Parts = append(mobs.Parts, gwmodel.Part{Mobs: p.Mobs, MobSteps: p.MobSteps, RemovedMobs: p.RemovedMobs})
			}
			for b.Loop() {
			}
			var total, onlyPlayers, onlyMobs, empty, seenPlayers, seenMobs int
			for _, r := range to {
				total += size(whole, r, true)
				onlyPlayers += size(players, r, true)
				onlyMobs += size(mobs, r, false)
				empty += size(&gwmodel.Frame{Parts: make([]gwmodel.Part, len(whole.Parts))}, r, false)
				for _, k := range r.Parts {
					seenPlayers += len(whole.Parts[k].Players) + len(whole.Parts[k].Steps)
					seenMobs += len(whole.Parts[k].Mobs) + len(whole.Parts[k].MobSteps)
				}
			}
			n := float64(len(to))
			b.ReportMetric(float64(total)/n, "B/client")
			b.ReportMetric(float64(onlyPlayers-empty)/n, "B/players")
			b.ReportMetric(float64(onlyMobs-empty)/n, "B/mobs")
			b.ReportMetric(float64(empty)/n, "B/other")
			b.ReportMetric(float64(seenPlayers)/n, "players/client")
			b.ReportMetric(float64(seenMobs)/n, "mobs/client")
			b.ReportMetric(float64(total)/n*20/1000, "kB/s@20Hz")
			far := 0.0
			for _, a := range s.at {
				far = max(far, math.Hypot(a[0]-s.home.X, a[1]-s.home.Z))
			}
			b.ReportMetric(far, "m/farthest")
		})
	}
}

// newClumpedScene is a scene of players kept within about 60 m of the middle of a map, among six camps of perCamp mobs within 70 m of it.
func newClumpedScene(b *testing.B, data *gamedata.Data, players, perCamp int) *scene {
	const size = 256.0
	centre := gamedata.Point{X: size / 2, Z: size / 2}
	r := rand.New(rand.NewPCG(7, 3))
	kinds := []string{"green_slime", "orange_mushroom", "green_slime", "green_slime", "orange_mushroom", "green_slime"}
	spawns := make([]gamedata.Spawn, len(kinds))
	for k, kind := range kinds {
		at := gamedata.Point{X: centre.X + (r.Float64()*2-1)*70, Z: centre.Z + (r.Float64()*2-1)*70}
		spawns[k] = gamedata.Spawn{Mob: kind, Max: perCamp, Respawn: 24 * time.Second, Radius: 8, At: at}
	}
	m := worldtest.Gridded(b, &gamedata.Map{
		ID: "camp", Kind: gamedata.Hunting, Town: "willowmere", Geometry: worldtest.Flat(size), Spawns: spawns, PlayerSpawn: centre,
	})
	s := newSceneAt(b, m, data, players, func(r *rand.Rand) (float64, float64) {
		return centre.X + (r.Float64()*2-1)*30, centre.Z + (r.Float64()*2-1)*30
	})
	s.home = &centre
	return s
}
