// Package interestbench measures a crowded region's frames end to end: the world cutting each tick by interest cell, and each client's Delta as the server encodes it (large-regions.md section 5).
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

	"finga/internal/application/world"
	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	"finga/internal/handler/transport"
	handlerworld "finga/internal/handler/world"
	gwmodel "finga/internal/model/account"
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

// A tick of 150 players walking a 1 km region among 400 mobs: what the world spends cutting and sending it, and what each client's Delta costs the server, in bytes and allocations.
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
				o, parts := newOutbox(conn), facadeFrame(frame)
				bytes = 0
				for _, r := range to {
					o.Deliver(gwmodel.Delivery{Tick: frame.Tick, Event: world.Framed{Frame: parts, Recipient: facadeRecipient(r)}})
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
	walk *sim.Input                        // when set, every player presses it
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
		if s.walk != nil {
			key = *s.walk
		}
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

// stream is one node's: it keeps the latest frame and who it went to.
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

// facadeFrame is f as the world's facade sends it, the views the scene sets copied as the facade copies them.
func facadeFrame(f *model.Frame) *world.Frame {
	out := &world.Frame{Parts: make([]world.Part, len(f.Parts))}
	for i, p := range f.Parts {
		g := &out.Parts[i]
		for _, v := range p.Players {
			g.Players = append(g.Players, playerView(v))
		}
		for _, v := range p.Steps {
			g.Steps = append(g.Steps, world.PlayerStep{Slot: v.Slot, DX: v.DX, DY: v.DY, DZ: v.DZ, VX: v.VX, VY: v.VY, VZ: v.VZ, HX: v.HX, HZ: v.HZ, OnGround: v.OnGround, HP: v.HP, Sprint: v.Sprint, Held: v.Held, Aim: v.Aim})
		}
		for _, v := range p.MobSteps {
			st := world.MobStep{ID: v.ID, DX: v.DX, DY: v.DY, DZ: v.DZ, Yaw: v.Yaw, HP: v.HP}
			if v.Pose != nil {
				pose := world.MobPose(*v.Pose)
				st.Pose = &pose
			}
			g.MobSteps = append(g.MobSteps, st)
		}
		for _, v := range p.Mobs {
			g.Mobs = append(g.Mobs, mobViewOf(v))
		}
		g.Removed, g.RemovedMobs = p.Removed, p.RemovedMobs
	}
	return out
}

// facadeRecipient is r as the world's facade sends it.
func facadeRecipient(r model.Recipient) world.Recipient {
	out := world.Recipient{Character: r.Character, Connection: r.Connection, Parts: r.Parts, AckSeq: r.AckSeq, Energy: r.Energy}
	if r.Own != nil {
		v := playerView(*r.Own)
		out.Own = &v
	}
	if u := r.Unseen; u != nil {
		out.Unseen = &world.Unseen{Players: u.Players, Mobs: u.Mobs}
	}
	return out
}

func playerView(v model.PlayerView) world.PlayerView {
	return world.PlayerView{
		ID: v.ID, Slot: v.Slot, Name: v.Name, Class: v.Class, Level: v.Level, HP: v.HP, MaxHP: v.MaxHP, Dead: v.Dead, Invulnerable: v.Invulnerable,
		Dodging: v.Dodging, Shield: v.Shield, State: v.State, Aim: v.Aim, Statuses: statuses(v.Statuses), Team: v.Team, Bot: v.Bot, Look: world.Look(v.Look),
	}
}

func mobViewOf(v model.MobView) world.MobView {
	return world.MobView{ID: v.ID, Type: v.Type, X: v.X, Y: v.Y, Z: v.Z, Yaw: v.Yaw, HP: v.HP, MaxHP: v.MaxHP, Pose: world.MobPose(v.Pose), Statuses: statuses(v.Statuses)}
}

func statuses(ss []model.StatusView) []world.StatusView {
	var out []world.StatusView
	for _, s := range ss {
		out = append(out, world.StatusView(s))
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
			size := wireSizer(b, frame.Tick)
			whole := facadeFrame(frame)
			players, mobs := &world.Frame{}, &world.Frame{}
			for _, p := range whole.Parts {
				players.Parts = append(players.Parts, world.Part{Players: p.Players, Steps: p.Steps, Removed: p.Removed})
				mobs.Parts = append(mobs.Parts, world.Part{Mobs: p.Mobs, MobSteps: p.MobSteps, RemovedMobs: p.RemovedMobs})
			}
			for b.Loop() {
			}
			var total, onlyPlayers, onlyMobs, empty, seenPlayers, seenMobs int
			for _, r := range to {
				total += size(whole, r, true)
				onlyPlayers += size(players, r, true)
				onlyMobs += size(mobs, r, false)
				empty += size(&world.Frame{Parts: make([]world.Part, len(whole.Parts))}, r, false)
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

// wireSizer is the bytes of the Delta a client is sent of a frame, as the server encodes it: with the recipient's own view or without.
func wireSizer(b testing.TB, tick uint64) func(f *world.Frame, r model.Recipient, withOwn bool) int {
	codec := protocol.ServerCodec(wsconn.Binary)
	conn := &discard{}
	return func(f *world.Frame, r model.Recipient, withOwn bool) int {
		o := newOutbox(conn)
		fr := facadeRecipient(r)
		if !withOwn {
			fr.Own = nil
		}
		o.Deliver(gwmodel.Delivery{Tick: tick, Event: world.Framed{Frame: f, Recipient: fr}})
		wire, err := codec.Encode(conn.last)
		if err != nil {
			b.Fatal(err)
		}
		return len(wire)
	}
}

// clumpedMap is a map with six camps of perCamp mobs within 70 m of its middle.
func clumpedMap(b testing.TB, perCamp int, spawn gamedata.Point) *gamedata.Map {
	const size = 256.0
	centre := gamedata.Point{X: size / 2, Z: size / 2}
	r := rand.New(rand.NewPCG(7, 3))
	kinds := []string{"green_slime", "orange_mushroom", "green_slime", "green_slime", "orange_mushroom", "green_slime"}
	spawns := make([]gamedata.Spawn, len(kinds))
	for k, kind := range kinds {
		at := gamedata.Point{X: centre.X + (r.Float64()*2-1)*70, Z: centre.Z + (r.Float64()*2-1)*70}
		spawns[k] = gamedata.Spawn{Mob: kind, Max: perCamp, Respawn: 24 * time.Second, Radius: 8, At: at}
	}
	return worldtest.Gridded(b, &gamedata.Map{
		ID: "camp", Kind: gamedata.Hunting, Town: "willowmere", Geometry: worldtest.Flat(size), Spawns: spawns, PlayerSpawn: spawn,
	})
}

// newClumpedScene is a scene of players kept within about 60 m of the middle of a map, among six camps of perCamp mobs within 70 m of it.
func newClumpedScene(b *testing.B, data *gamedata.Data, players, perCamp int) *scene {
	centre := gamedata.Point{X: 128, Z: 128}
	s := newSceneAt(b, clumpedMap(b, perCamp, centre), data, players, func(r *rand.Rand) (float64, float64) {
		return centre.X + (r.Float64()*2-1)*30, centre.Z + (r.Float64()*2-1)*30
	})
	s.home = &centre
	return s
}

// A clump of 150 players logs in a few a tick at the start of a map and all set off at once toward its camps, as the soak's bots did: the busiest whole second a client is sent once all are in, in kB/s, and what it holds a tick, in bytes, by kind. This is the case the 40 kB/s budget is judged on (large-regions.md section 5).
func BenchmarkAClumpWalking(b *testing.B) {
	data := interestData(b, 1, 3, 3)
	s := &scene{inst: worldtest.NewInstance(clumpedMap(b, 15, gamedata.Point{X: 56, Z: 40}), data, nil, worldtest.Options{}), out: &stream{}, r: rand.New(rand.NewPCG(2, 9)), at: map[kernel.CharacterID][2]float64{}, walk: &sim.Input{Right: true, Up: true}}
	type tick struct{ total, steps, whole, mobSteps, mobs, other, wholeViews float64 }
	var ticks []tick
	measure := func() {
		frame, to := s.out.frame, s.out.to
		if len(to) < crowd {
			return
		}
		size, parts := wireSizer(b, frame.Tick), facadeFrame(frame)
		only := func(keep func(p world.Part) world.Part) *world.Frame {
			f := &world.Frame{}
			for _, p := range parts.Parts {
				f.Parts = append(f.Parts, keep(p))
			}
			return f
		}
		steps := only(func(p world.Part) world.Part { return world.Part{Steps: p.Steps} })
		whole := only(func(p world.Part) world.Part { return world.Part{Players: p.Players} })
		mobSteps := only(func(p world.Part) world.Part { return world.Part{MobSteps: p.MobSteps} })
		mobs := only(func(p world.Part) world.Part { return world.Part{Mobs: p.Mobs} })
		empty := &world.Frame{Parts: make([]world.Part, len(parts.Parts))}
		var t tick
		for _, r := range to {
			e := float64(size(empty, r, false))
			t.total += float64(size(parts, r, true))
			t.steps += float64(size(steps, r, false)) - e
			t.whole += float64(size(whole, r, false)) - e
			t.mobSteps += float64(size(mobSteps, r, false)) - e
			t.mobs += float64(size(mobs, r, false)) - e
			t.other += e
		}
		n := float64(len(to))
		t.total, t.steps, t.whole, t.mobSteps, t.mobs, t.other = t.total/n, t.steps/n, t.whole/n, t.mobSteps/n, t.mobs/n, t.other/n
		for _, p := range parts.Parts {
			t.wholeViews += float64(len(p.Players))
		}
		ticks = append(ticks, t)
	}
	var joined sync.WaitGroup
	for k := range crowd {
		if k%4 == 0 {
			s.step()
			measure()
		}
		p := worldtest.NewPlayer(fmt.Sprintf("P%03d", k), 50+s.r.Float64()*12, 34+s.r.Float64()*12)
		p.Level, p.HP = 60, 50000
		s.ids, s.keys = append(s.ids, p.ID), append(s.keys, sim.Input{})
		joined.Go(func() {
			if err := s.inst.Join(context.Background(), p, sink{s.out}); err != nil {
				b.Error(err)
			}
		})
	}
	done := make(chan struct{})
	go func() { joined.Wait(); close(done) }()
	for waiting := true; waiting; {
		select {
		case <-done:
			waiting = false
		default:
			s.step()
		}
	}
	ticks = ticks[:0]
	for range 400 {
		s.step()
		measure()
	}
	for b.Loop() {
	}
	const second = 20
	best, at := 0.0, 0
	for k := 0; k+second <= len(ticks); k++ {
		sum := 0.0
		for _, t := range ticks[k : k+second] {
			sum += t.total
		}
		if sum > best {
			best, at = sum, k
		}
	}
	var busy tick
	for _, t := range ticks[at : at+second] {
		busy.total, busy.steps, busy.whole, busy.mobSteps, busy.mobs, busy.other, busy.wholeViews = busy.total+t.total, busy.steps+t.steps, busy.whole+t.whole, busy.mobSteps+t.mobSteps, busy.mobs+t.mobs, busy.other+t.other, busy.wholeViews+t.wholeViews
	}
	b.ReportMetric(best/1000, "kB/s@busiest")
	b.ReportMetric(busy.steps/second, "B/steps")
	b.ReportMetric(busy.whole/second, "B/whole")
	b.ReportMetric(busy.mobSteps/second, "B/mobsteps")
	b.ReportMetric(busy.mobs/second, "B/mobs")
	b.ReportMetric(busy.other/second, "B/other")
	b.ReportMetric(busy.wholeViews/second, "whole/tick")
}
