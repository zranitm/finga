package instance_test

import (
	"finga/modules/world/worldtest"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/modules/world/internal/model"
)

type fakeParties struct {
	mu      sync.Mutex
	parties [][]kernel.CharacterID
	updates []model.MemberUpdate
	epoch   uint64
}

func (f *fakeParties) Epoch() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.epoch
}

func (f *fakeParties) updated() []model.MemberUpdate {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.updates)
}

// Party is the first party holding id, named by its place in f.parties.
func (f *fakeParties) Party(id kernel.CharacterID) model.Party {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, p := range f.parties {
		if slices.Contains(p, id) {
			return model.Party{ID: fmt.Sprintf("party-%d", k), Members: slices.Clone(p)}
		}
	}
	return model.Party{}
}

func (f *fakeParties) Update(u model.MemberUpdate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, u)
}

// When the party stream subscribes again, social may have restarted and kept only what logins told it, so every player's state is sent again.
func TestANewPartySubscriptionRetellsEveryPlayer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		parties := &fakeParties{epoch: 1}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Parties: parties})
		defer stop()
		x, y := worldtest.TownSpawn(t, data)
		ayla, robin := worldtest.NewPlayer("Ayla", x, y), worldtest.NewPlayer("Robin", x, y)
		join(t, inst, ayla)
		join(t, inst, robin)
		before := len(parties.updated())
		parties.mu.Lock()
		parties.epoch = 2
		parties.mu.Unlock()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		got := parties.updated()[before:]
		if len(got) != 2 || got[0].ID != ayla.ID || got[1].ID != robin.ID || got[0].Level != 1 || got[0].MapID != "town" || got[0].Class != "warrior" {
			t.Fatalf("updates %+v; want each player's again", got)
		}
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		if n := len(parties.updated()) - before; n != 2 {
			t.Fatalf("%d updates two ticks on; want them sent once", n)
		}
	})
}

func expGained(s *worldtest.Sink) []int64 {
	var out []int64
	for _, e := range s.All() {
		if g, ok := e.(model.ExpGained); ok {
			out = append(out, g.Amount)
		}
	}
	return out
}

func TestExpSharing(t *testing.T) {
	// The slime gives 8 EXP, plus 10% per extra member; the remainder goes to the killer.
	for _, tt := range []struct {
		name       string
		members    int  // party members joined on the arena besides the killer
		dead       bool // one of them is dead
		elsewhere  int  // party members not on this map
		wantKiller int64
		wantOthers int64
	}{
		{"alone", 0, false, 0, 8, 0},
		{"two: total 8, 4 each", 1, false, 0, 4, 4},
		{"three: total 9, 3 each", 2, false, 0, 3, 3},
		{"four: total 10, 2 each, 2 over to the killer", 3, false, 0, 4, 2},
		{"four with one dead: shared by three", 3, true, 0, 3, 3},
		{"two here and two on another map: shared by two", 1, false, 2, 4, 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				parties := &fakeParties{}
				inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{Parties: parties})
				defer stop()
				time.Sleep(worldtest.TickInterval)
				synctest.Wait()
				killer := warriorAt(250)
				party := []kernel.CharacterID{killer.ID}
				var others []*worldtest.Sink
				if tt.dead { // the first member comes alone into the slime's reach and dies to its hop, so it goes after no one else first
					m := warriorAt(nextToSlime)
					m.HP = 1
					party = append(party, m.ID)
					others = append(others, join(t, inst, m))
					untilDead(t, others[0], m.ID)
				}
				sk := join(t, inst, killer)
				for k := range tt.members {
					if tt.dead && k == 0 {
						continue
					}
					m := warriorAt(150 + float64(k)*20)
					party = append(party, m.ID)
					others = append(others, join(t, inst, m))
				}
				for range tt.elsewhere {
					party = append(party, kernel.NewCharacterID())
				}
				parties.mu.Lock()
				parties.parties = [][]kernel.CharacterID{party}
				parties.mu.Unlock()
				killSlime(t, inst, killer.ID)
				if got := expGained(sk); len(got) != 1 || got[0] != tt.wantKiller {
					t.Fatalf("killer gained %v, want %d", got, tt.wantKiller)
				}
				for k, so := range others {
					got := expGained(so)
					if tt.dead && k == 0 {
						if len(got) != 0 {
							t.Fatalf("the dead member gained %v", got)
						}
						continue
					}
					if len(got) != 1 || got[0] != tt.wantOthers {
						t.Fatalf("member %d gained %v, want %d", k, got, tt.wantOthers)
					}
				}
			})
		})
	}
}

func chats(s *worldtest.Sink) []model.Chat {
	var out []model.Chat
	for _, e := range s.All() {
		if c, ok := e.(model.Chat); ok {
			out = append(out, c)
		}
	}
	return out
}

func TestMapChatReachesOnlyTheMap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		here, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		other := arena(time.Hour)
		other.ID = "arena2"
		there, stop2 := worldtest.RunMap(t, data, other, nil, worldtest.Options{})
		defer stop2()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		ayla, robin, cass := warriorAt(100), warriorAt(150), warriorAt(100)
		sa, sr, sc := join(t, here, ayla), join(t, here, robin), join(t, there, cass)
		_ = here.Chat(ayla.ID, "Ayla", "hello map")
		_ = here.Chat(cass.ID, "Cass", "I am not here") // not on this map: dropped
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		for _, s := range []*worldtest.Sink{sa, sr} {
			got := chats(s)
			if len(got) != 1 || got[0].Sender != ayla.ID || got[0].SenderName != "Ayla" || got[0].Text != "hello map" {
				t.Fatalf("chat %+v", got)
			}
		}
		if len(chats(sc)) != 0 {
			t.Fatal("a line crossed maps")
		}
	})
}

// A party sees a member's HP fall to a mob, once for the tick it fell in.
func TestPartyHearsHPLostToAMob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		parties := &fakeParties{}
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{Parties: parties})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		s := join(t, inst, warriorAt(nextToSlime))
		id := idOf(t, s)
		mobHitsOn(t, s, id, 1)
		tick(5)
		if got := damages(s); len(got) != 1 || got[0].Amount != 5 {
			t.Fatalf("damages %+v; want the slime's one hit", got)
		}
		ups := parties.updated()
		if len(ups) != 2 || ups[1] != (model.MemberUpdate{ID: id, MapID: "arena", Class: "warrior", Level: 1, HP: 139, MaxHP: 144}) {
			t.Fatalf("updates %+v; want the join's, then 139 of 144 once", ups)
		}
	})
}

func TestPartyHearsLevelsRespawnsAndMapChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		parties := &fakeParties{}
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{Parties: parties, Transfer: tr})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(250)
		p.Exp = 18 // two short of level 2
		p.HP = 120
		join(t, inst, p)
		parties.mu.Lock()
		ups := slices.Clone(parties.updates)
		parties.mu.Unlock()
		// A join is a map change too; after a world restart it is what corrects the frames social kept.
		if len(ups) != 1 || ups[0] != (model.MemberUpdate{ID: p.ID, MapID: "arena", Class: "warrior", Level: 1, HP: 120, MaxHP: 144}) {
			t.Fatalf("updates after a join: %+v", ups)
		}
		killSlime(t, inst, p.ID)
		parties.mu.Lock()
		ups = slices.Clone(parties.updates)
		parties.mu.Unlock()
		if len(ups) != 2 || ups[1].ID != p.ID || ups[1].Level != 2 || ups[1].MapID != "arena" || ups[1].Class != "warrior" || ups[1].HP != ups[1].MaxHP {
			t.Fatalf("updates after a level-up: %+v", ups)
		}
	})
}
