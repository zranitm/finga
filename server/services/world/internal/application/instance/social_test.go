package instance_test

import (
	"finga/services/world/worldtest"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/services/world/internal/model"
)

type fakeParties struct {
	mu      sync.Mutex
	parties [][]kernel.CharacterID
	updates []partyUpdate
}

type partyUpdate struct {
	id    kernel.CharacterID
	mapID string
	level int
	hp    int
	maxHP int
}

func (f *fakeParties) Members(id kernel.CharacterID) []kernel.CharacterID {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.parties {
		if slices.Contains(p, id) {
			return slices.Clone(p)
		}
	}
	return nil
}

func (f *fakeParties) SameParty(a, b kernel.CharacterID) bool {
	return slices.Contains(f.Members(a), b)
}

func (f *fakeParties) Update(id kernel.CharacterID, mapID string, level, hp, maxHP int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, partyUpdate{id, mapID, level, hp, maxHP})
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
	// The slime gives 5 EXP, plus 10% per extra member; the remainder goes to the killer.
	for _, tt := range []struct {
		name       string
		members    int  // party members joined on the arena besides the killer
		dead       bool // one of them is dead
		elsewhere  int  // party members not on this map
		wantKiller int64
		wantOthers int64
	}{
		{"alone", 0, false, 0, 5, 0},
		{"two: total 5, 2 each, 1 over to the killer", 1, false, 0, 3, 2},
		{"three: total 6, 2 each", 2, false, 0, 2, 2},
		{"four: total 6, 1 each, 2 over to the killer", 3, false, 0, 3, 1},
		{"four with one dead: shared by three", 3, true, 0, 2, 2},
		{"two here and two on another map: shared by two", 1, false, 2, 3, 2},
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
				sk := join(t, inst, killer)
				var others []*worldtest.Sink
				for k := range tt.members {
					m := warriorAt(150 + float64(k)*20)
					if tt.dead && k == 0 {
						m = warriorAt(300) // standing in the slime: dies on the first tick
						m.HP = 1
					}
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

func TestPartyMembersLootInsideTheWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakePickups()
		killer, member, stranger := warriorAt(250), warriorAt(300), warriorAt(300)
		parties := &fakeParties{parties: [][]kernel.CharacterID{{killer.ID, member.ID}}}
		inst, stop := runLootArena(t, data, items, 0.3, worldtest.Options{Parties: parties})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		join(t, inst, killer)
		killSlime(t, inst, killer.ID)
		ss := join(t, inst, stranger)
		sm := join(t, inst, member)
		_ = inst.PickUp(stranger.ID)
		time.Sleep(3 * worldtest.TickInterval)
		synctest.Wait()
		if items.callCount() != 0 || len(inventoryChanges(ss)) != 0 {
			t.Fatal("a stranger looted inside the window")
		}
		_ = inst.PickUp(member.ID)
		time.Sleep(3 * worldtest.TickInterval)
		synctest.Wait()
		if items.callCount() != 1 || len(inventoryChanges(sm)) != 1 {
			t.Fatalf("calls %d changes %d: a party member could not loot inside the window", items.callCount(), len(inventoryChanges(sm)))
		}
	})
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
		p.HP = 150
		join(t, inst, p)
		parties.mu.Lock()
		ups := slices.Clone(parties.updates)
		parties.mu.Unlock()
		// A join is a map change too; after a world restart it is what corrects the frames social kept.
		if len(ups) != 1 || ups[0] != (partyUpdate{p.ID, "arena", 1, 150, 204}) {
			t.Fatalf("updates after a join: %+v", ups)
		}
		killSlime(t, inst, p.ID)
		parties.mu.Lock()
		ups = slices.Clone(parties.updates)
		parties.mu.Unlock()
		if len(ups) != 2 || ups[1].id != p.ID || ups[1].level != 2 || ups[1].mapID != "arena" || ups[1].hp != ups[1].maxHP {
			t.Fatalf("updates after a level-up: %+v", ups)
		}
	})
}
