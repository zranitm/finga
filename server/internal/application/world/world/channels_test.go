package world_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// parties are fixed parties, each a list of members.
type parties struct {
	mu  sync.Mutex
	all [][]kernel.CharacterID
}

func (p *parties) Party(id kernel.CharacterID) model.Party {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, members := range p.all {
		if slices.Contains(members, id) {
			return model.Party{ID: "p", Members: slices.Clone(members)}
		}
	}
	return model.Party{}
}

func (*parties) Update(model.MemberUpdate) {}
func (*parties) Epoch() uint64             { return 0 }

// channelWorld runs a World with o in the bubble until the test ends.
func channelWorld(t *testing.T, o worldtest.Options) *testWorld {
	t.Helper()
	o.CheckpointInterval = time.Hour
	w := newWorld(t, &fakeSaver{}, o)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return w
}

// channelOf is the channel of the newest snapshot s heard.
func channelOf(t *testing.T, s *worldtest.Sink) int {
	t.Helper()
	snaps := worldtest.Of[model.Snapshot](s)
	if len(snaps) == 0 {
		t.Fatal("no snapshot")
	}
	return snaps[len(snaps)-1].Channel
}

// enterRegion joins name to the test region, and gives where they landed.
func (w *testWorld) enterRegion(t *testing.T, name string) (model.Player, *worldtest.Sink) {
	t.Helper()
	p, s := at(t, name, "test_region"), &worldtest.Sink{}
	w.enter(t, p, "test_region", "c-"+name, s)
	return p, s
}

func (w *testWorld) open(t *testing.T, p model.Player) []model.ChannelLoad {
	t.Helper()
	got, err := w.Channels(p.ID, "c-"+p.Name)
	if err != nil {
		t.Fatal(err)
	}
	return got.Open
}

// Joins fill a region's channels (large-regions.md section 4): the least full with room, a new one once all are full, and the list says how full each is.
func TestJoinsFillARegionsChannels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := channelWorld(t, worldtest.Options{ChannelCap: 2})
		var in []int
		var ayla model.Player
		for _, name := range []string{"Ayla", "Bo", "Cy", "Dee", "Eli"} {
			p, s := w.enterRegion(t, name)
			if name == "Ayla" {
				ayla = p
			}
			in = append(in, channelOf(t, s))
		}
		if !slices.Equal(in, []int{1, 1, 2, 2, 3}) {
			t.Fatalf("joined channels %v; want two to 1, two to 2, then 3", in)
		}
		want := []model.ChannelLoad{{Channel: 1, Players: 2}, {Channel: 2, Players: 2}, {Channel: 3, Players: 1}}
		if got, err := w.Channels(ayla.ID, "c-Ayla"); err != nil || !slices.Equal(got.Open, want) || got.Cap != 2 {
			t.Fatalf("channels %+v, %v; want %+v of 2", got, err, want)
		}
		if err := w.Leave(context.Background(), ayla.ID, "c-Ayla"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if _, s := w.enterRegion(t, "Fay"); channelOf(t, s) != 1 {
			t.Fatalf("Fay joined channel %d; want 1, as full as 3 and lower", channelOf(t, s))
		}
		bo, s := at(t, "Bo2", "town"), &worldtest.Sink{}
		w.enter(t, bo, "town", "c-Bo2", s)
		if _, err := w.Channels(bo.ID, "c-Bo2"); !errors.Is(err, model.ErrNotARegion) {
			t.Fatalf("channels in town: %v; want ErrNotARegion", err)
		}
		if snaps := worldtest.Of[model.Snapshot](s); snaps[0].Channel != 0 {
			t.Fatalf("a town's snapshot says channel %d", snaps[0].Channel)
		}
	})
}

// A party's channel comes first while it has room, though another is less full; once it's full, the least full.
func TestAPartyJoinsItsChannelWhileItHasRoom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ps := &parties{}
		w := channelWorld(t, worldtest.Options{ChannelCap: 3, Parties: ps})
		ayla, _ := w.enterRegion(t, "Ayla")
		bo, _ := w.enterRegion(t, "Bo")
		w.enterRegion(t, "Cy")
		if _, s := w.enterRegion(t, "Dee"); channelOf(t, s) != 2 {
			t.Fatalf("Dee joined %d; want 2", channelOf(t, s))
		}
		if err := w.Leave(context.Background(), bo.ID, "c-Bo"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		mira, nia := at(t, "Mira", "test_region"), at(t, "Nia", "test_region")
		tom, sTom := at(t, "Tom", "town"), &worldtest.Sink{}
		w.enter(t, tom, "town", "c-Tom", sTom) // in the party, but not in the region
		ps.mu.Lock()
		ps.all = [][]kernel.CharacterID{{ayla.ID, mira.ID, nia.ID, tom.ID}}
		ps.mu.Unlock()
		sm, sn := &worldtest.Sink{}, &worldtest.Sink{}
		w.enter(t, mira, "test_region", "c-Mira", sm)
		w.enter(t, nia, "test_region", "c-Nia", sn)
		if channelOf(t, sm) != 1 || channelOf(t, sn) != 2 {
			t.Fatalf("Mira joined %d, Nia %d; want Ayla's 1 while it had room, then the least full 2", channelOf(t, sm), channelOf(t, sn))
		}
	})
}

// A teleport into a region lands in the channel picked for it, as a join does.
func TestATeleportIntoARegionIsPicked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		w := channelWorld(t, worldtest.Options{ChannelCap: 1})
		w.enterRegion(t, "Ayla")
		bo, s := at(t, "Bo", "town"), &worldtest.Sink{}
		w.enter(t, bo, "town", "c-Bo", s)
		if err := w.Teleport(context.Background(), bo.ID, "c-Bo", "test_region", 1); err != nil {
			t.Fatal(err)
		}
		time.Sleep(data.TeleportChannel() + 3*worldtest.TickInterval)
		synctest.Wait()
		if snaps := worldtest.Of[model.Snapshot](s); len(snaps) != 2 || snaps[1].MapID != "test_region" || snaps[1].Channel != 2 {
			t.Fatalf("snapshots %+v; want the test region's channel 2 after the town", snaps)
		}
	})
}

// A channel past the first closes once empty for the idle time, and its number is opened again when needed; the first stays open.
func TestAnIdleChannelCloses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := channelWorld(t, worldtest.Options{ChannelCap: 1, ChannelIdle: 5 * time.Minute})
		ayla, _ := w.enterRegion(t, "Ayla")
		kai, sk := w.enterRegion(t, "Kai")
		if channelOf(t, sk) != 2 {
			t.Fatalf("Kai joined %d", channelOf(t, sk))
		}
		two := w.Opened("test_region/2")
		if err := w.Leave(context.Background(), kai.ID, "c-Kai"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5*time.Minute - time.Second)
		synctest.Wait()
		if got := w.open(t, ayla); !slices.Equal(got, []model.ChannelLoad{{Channel: 1, Players: 1}, {Channel: 2}}) {
			t.Fatalf("a second short of the idle time: %+v", got)
		}
		time.Sleep(25 * time.Second)
		synctest.Wait()
		if got := w.open(t, ayla); !slices.Equal(got, []model.ChannelLoad{{Channel: 1, Players: 1}}) {
			t.Fatalf("past the idle time: %+v", got)
		}
		if w.Opened("test_region/2") != nil {
			t.Fatal("channel 2's instance still runs")
		}
		if _, s := w.enterRegion(t, "Mira"); channelOf(t, s) != 2 || w.Opened("test_region/2") == two {
			t.Fatalf("Mira joined %d; want a new channel 2", channelOf(t, s))
		}
		if err := w.Leave(context.Background(), ayla.ID, "c-Ayla"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(6 * time.Minute)
		synctest.Wait()
		if _, s := w.enterRegion(t, "Nia"); channelOf(t, s) != 1 || w.MapActor("test_region").Players() != 1 {
			t.Fatalf("Nia joined %d; want the first channel, open after it stood empty past the idle time", channelOf(t, s))
		}
	})
}

// One who dies in a region's channel gets up in it: no hand-off, and the channel keeps them.
func TestADeathInAChannelGetsUpInIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		w := channelWorld(t, worldtest.Options{ChannelCap: 1})
		ayla, _ := w.enterRegion(t, "Ayla")
		region, _ := data.Map("test_region")
		var camp model.Player
		for _, s := range region.Spawns {
			if s.Mob == "green_slime" {
				camp = worldtest.NewPlayer("Kai", s.At.X, s.At.Z)
			}
		}
		camp.HP = 1
		sk := &worldtest.Sink{}
		w.enter(t, camp, "test_region", "c-Kai", sk)
		for range 120 {
			if len(worldtest.Of[model.Respawned](sk)) > 0 {
				break
			}
			_ = w.Attack(camp.ID, "c-Kai", 0, model.EntityRef{})
			time.Sleep(time.Second)
			synctest.Wait()
		}
		got := worldtest.Of[model.Respawned](sk)
		if len(got) != 1 || got[0].MapID != "test_region" {
			t.Fatalf("respawned %+v; want once in the test region", got)
		}
		if snaps := worldtest.Of[model.Snapshot](sk); len(snaps) != 1 || snaps[0].Channel != 2 || len(worldtest.Of[model.Teleport](sk)) != 0 {
			t.Fatalf("snapshots %+v; want the join's alone, in channel 2", snaps)
		}
		if got := w.open(t, ayla); !slices.Equal(got, []model.ChannelLoad{{Channel: 1, Players: 1}, {Channel: 2, Players: 1}}) {
			t.Fatalf("channels %+v; want Kai still in 2", got)
		}
	})
}

// A switch (large-regions.md section 4) is refused off a region, to the channel the player is in, to one not open and to a full one; else it channels as a teleport does and lands at the same spot in the other channel, or is cancelled as a teleport is.
func TestASwitchMovesToTheSameSpotInAnotherChannel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		w := channelWorld(t, worldtest.Options{ChannelCap: 2})
		ayla, sa := at(t, "Ayla", "test_region"), &worldtest.Sink{}
		ayla.X += 3
		w.enter(t, ayla, "test_region", "c-Ayla", sa)
		bo, sb := w.enterRegion(t, "Bo")
		w.enterRegion(t, "Cy")
		dee, _ := w.enterRegion(t, "Dee")
		tom := at(t, "Tom", "town")
		w.enter(t, tom, "town", "c-Tom", &worldtest.Sink{})
		ctx := context.Background()
		for _, c := range []struct {
			who  model.Player
			to   int
			want error
		}{
			{tom, 2, model.ErrNotARegion},
			{ayla, 1, model.ErrSameChannel},
			{ayla, 3, model.ErrNoSuchChannel},
			{ayla, 2, model.ErrChannelFull},
		} {
			if err := w.SwitchChannel(ctx, c.who.ID, "c-"+c.who.Name, c.to, 1); !errors.Is(err, c.want) {
				t.Fatalf("%s's switch to %d: %v; want %v", c.who.Name, c.to, err, c.want)
			}
		}
		if err := w.Leave(ctx, dee.ID, "c-Dee"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		here := joinedAt(t, sa, ayla.ID)
		if err := w.SwitchChannel(ctx, ayla.ID, "c-Ayla", 2, 1); err != nil {
			t.Fatal(err)
		}
		if err := w.SwitchChannel(ctx, bo.ID, "c-Bo", 2, 1); err != nil {
			t.Fatal(err)
		}
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		_ = w.Input(bo.ID, "c-Bo", 2, sim.Input{Up: true}, 0)
		time.Sleep(data.TeleportChannel() + 3*worldtest.TickInterval)
		synctest.Wait()
		tel, snaps := worldtest.Of[model.Teleport](sa), worldtest.Of[model.Snapshot](sa)
		if len(tel) != 1 || tel[0].MapID != "test_region" || tel[0].X != here.X || tel[0].Z != here.Z {
			t.Fatalf("teleports %+v; want one to where Ayla stood, %v", tel, here)
		}
		if len(snaps) != 2 || snaps[1].Channel != 2 {
			t.Fatalf("snapshots %+v; want channel 2's after channel 1's", snaps)
		}
		if i := slices.IndexFunc(snaps[1].Players, func(v model.PlayerView) bool { return v.ID == ayla.ID }); i < 0 || snaps[1].Players[i].State.X != here.X || snaps[1].Players[i].State.Z != here.Z {
			t.Fatalf("Ayla in channel 2's snapshot: %+v; want at %v", snaps[1].Players, here)
		}
		if cancelled := worldtest.Of[model.TeleportCancelled](sb); len(cancelled) != 1 || cancelled[0].Reason != model.TeleportMoved || channelOf(t, sb) != 1 {
			t.Fatalf("Bo's switch: cancelled %+v, in channel %d; want it cancelled by the move, in 1", cancelled, channelOf(t, sb))
		}
		if got := w.open(t, ayla); !slices.Equal(got, []model.ChannelLoad{{Channel: 1, Players: 1}, {Channel: 2, Players: 2}}) {
			t.Fatalf("channels %+v; want Bo alone in 1, Ayla and Cy in 2", got)
		}
	})
}

// A switch's start restarts its channel's idle clock, so the channel doesn't close under it: cancelled, it closes the idle time after.
func TestASwitchKeepsItsChannelOpen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := channelWorld(t, worldtest.Options{ChannelCap: 1, ChannelIdle: 5 * time.Minute})
		ayla, _ := w.enterRegion(t, "Ayla")
		kai, _ := w.enterRegion(t, "Kai")
		ctx := context.Background()
		if err := w.Leave(ctx, kai.ID, "c-Kai"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(4 * time.Minute)
		synctest.Wait()
		if err := w.SwitchChannel(ctx, ayla.ID, "c-Ayla", 2, 1); err != nil {
			t.Fatal(err)
		}
		_ = w.Input(ayla.ID, "c-Ayla", 2, sim.Input{Up: true}, 0)
		time.Sleep(90 * time.Second)
		synctest.Wait()
		if got := w.open(t, ayla); len(got) != 2 {
			t.Fatalf("5:30 after channel 2 emptied, 1:30 after the switch started: %+v; want it open", got)
		}
		time.Sleep(4 * time.Minute)
		synctest.Wait()
		if got := w.open(t, ayla); len(got) != 1 {
			t.Fatalf("5:30 after the switch started: %+v; want channel 2 closed", got)
		}
	})
}
