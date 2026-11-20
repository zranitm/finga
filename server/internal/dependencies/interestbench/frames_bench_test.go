// Package interestbench measures a crowded region's frames end to end: the world cutting each tick by interest cell, and each client's Delta as the gateway encodes it (large-regions.md section 5).
package interestbench

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	"finga/internal/handler/gateway/ws/push"
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

// shapes are the blocks of cells compared: one as big as the map, a square 7 by 7, and the shipped 7 by 5.
var shapes = []struct {
	name                string
	behind, ahead, side int
}{{"whole map", 32, 32, 32}, {"7x7", 3, 3, 3}, {"7x5", 1, 3, 3}}

// A tick of 150 players walking a 1 km region among 400 mobs: what the world spends cutting and sending it, and what each client's Delta costs the gateway, in bytes and allocations.
func BenchmarkACrowdedRegionsFrames(b *testing.B) {
	m := region(b)
	for _, sh := range shapes {
		data := worldtest.LoadDataWith(b, worldtest.WithoutRegions(b, map[string]func(string) string{"world.yaml": func(s string) string {
			return strings.NewReplacer(
				"  behind: 1\n", fmt.Sprintf("  behind: %d\n", sh.behind),
				"  ahead: 3\n", fmt.Sprintf("  ahead: %d\n", sh.ahead),
				"  side: 3\n", fmt.Sprintf("  side: %d\n", sh.side),
			).Replace(s)
		}}))
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
				o, parts := push.NewOutbox(conn, conn, push.NewFrames()), gatewayFrame(frame)
				bytes = 0
				for _, r := range to {
					o.Deliver(gwmodel.Delivery{Tick: frame.Tick, Push: gwmodel.Delta{Frame: parts, Parts: r.Parts, AckSeq: r.AckSeq, Unseen: unseen(r.Unseen)}})
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
		ID: "region", Kind: gamedata.Hunting, Town: "town", Geometry: worldtest.Flat(regionSize), Spawns: spawns,
		PlayerSpawn: gamedata.Point{X: regionSize / 2, Z: regionSize / 2},
	})
}

// scene is a map stepped by hand, its players each holding a key they change now and then, as walkers do.
type scene struct {
	inst *instance.Instance
	out  *stream
	ids  []kernel.CharacterID
	keys []sim.Input
	r    *rand.Rand
	tick uint64
	seq  uint32
}

func newScene(b *testing.B, m *gamedata.Map, data *gamedata.Data, players int) *scene {
	s := &scene{inst: worldtest.NewInstance(m, data, nil, worldtest.Options{}), out: &stream{}, r: rand.New(rand.NewPCG(2, 9))}
	var joined sync.WaitGroup
	for k := range players {
		p := worldtest.NewPlayer(fmt.Sprintf("P%03d", k), 20+s.r.Float64()*(regionSize-40), 20+s.r.Float64()*(regionSize-40))
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
		_ = s.inst.Input(id, s.seq, s.keys[k], 0)
	}
	s.inst.Step(s.tick, worldtest.TickInterval)
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
func (d *discard) MessageOut(string)           {}
func (d *discard) WelcomeSent(string, int)     {}

// gatewayFrame is f as the gateway's model has it, each view's fields the scene sets as the world's facade and adapter copy them.
func gatewayFrame(f *model.Frame) *gwmodel.Frame {
	out := &gwmodel.Frame{Parts: make([]gwmodel.Part, len(f.Parts))}
	for i, p := range f.Parts {
		g := &out.Parts[i]
		for _, v := range p.Players {
			st := v.State
			g.Players = append(g.Players, gwmodel.PlayerView{
				ID: v.ID.String(), Name: v.Name, Class: v.Class.String(), Level: v.Level, HP: v.HP, MaxHP: v.MaxHP, Dead: v.Dead, Invulnerable: v.Invulnerable,
				X: st.X, Y: st.Y, Z: st.Z, VX: st.VX, VY: st.VY, VZ: st.VZ, HX: st.HX, HZ: st.HZ, OnGround: st.OnGround, Aim: v.Aim, Held: st.Held, Sprint: st.Sprint, Walk: st.Walk,
				Statuses: statuses(v.Statuses),
			})
		}
		for _, v := range p.Mobs {
			g.Mobs = append(g.Mobs, gwmodel.MobView{ID: v.ID, Type: v.Type, X: v.X, Y: v.Y, Z: v.Z, Yaw: v.Yaw, HP: v.HP, MaxHP: v.MaxHP, Pose: poses[v.Pose], Statuses: statuses(v.Statuses)})
		}
		for _, id := range p.Removed {
			g.RemovedPlayers = append(g.RemovedPlayers, id.String())
		}
		g.RemovedMobs = p.RemovedMobs
	}
	return out
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
