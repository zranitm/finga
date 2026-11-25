package instance

import (
	"testing"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
)

// heard keeps each update the party copy is handed.
type heard struct{ updates []model.MemberUpdate }

func (h *heard) Party(kernel.CharacterID) model.Party { return model.Party{} }
func (h *heard) Update(u model.MemberUpdate)          { h.updates = append(h.updates, u) }
func (h *heard) Epoch() uint64                        { return 0 }

// A member's HP alone reaches their party at most every 250 ms, its newest always within it: a hit every tick for 2 s sends at most 9 updates, the last value among them. Going down, getting up and a new max HP go out on their own tick.
func TestThePartyHearsHPAtMostEveryQuarterSecond(t *testing.T) {
	const second = 20 // ticks at 50 ms
	setup := func() (*Instance, *player, *heard) {
		h := &heard{}
		id := kernel.NewCharacterID()
		p := &player{ID: id, Class: "warrior", Level: 9, HP: 1000, derived: progression.Derived{MaxHP: 1000}}
		i := &Instance{m: &gamedata.Map{ID: "arena"}, tuning: Tuning{TickInterval: 50 * time.Millisecond}, deps: Services{Parties: h}, players: map[kernel.CharacterID]*player{id: p}, order: []kernel.CharacterID{id}, tick: 100}
		i.tellParty(p, i.m.ID, i.channel) // as its join does
		h.updates = nil
		return i, p, h
	}
	run := func(i *Instance, ticks int, each func(t uint64)) {
		for range ticks {
			i.tick++
			each(i.tick)
			i.tellHP()
		}
	}

	i, p, h := setup()
	run(i, 1, func(uint64) {}) // so the hits end inside a window, whose tail alone can tell the last
	run(i, 2*second, func(uint64) { p.HP-- })
	run(i, second/4, func(uint64) {})
	if n := len(h.updates); n == 0 || n > 9 {
		t.Fatalf("%d updates over 2 s of a hit a tick; want 1 to 9", n)
	}
	if last := h.updates[len(h.updates)-1]; last.HP != 1000-2*second {
		t.Fatalf("the last update says %d HP; want the newest, %d", last.HP, 1000-2*second)
	}
	for k := 1; k < len(h.updates); k++ {
		if h.updates[k].HP == h.updates[k-1].HP {
			t.Fatalf("update %d repeats %d HP", k, h.updates[k].HP)
		}
	}

	for _, tt := range []struct {
		name string
		then func(p *player)
		want func(u model.MemberUpdate) bool
	}{
		{"down", func(p *player) { p.HP = 0 }, func(u model.MemberUpdate) bool { return u.HP == 0 }},
		{"a new max HP", func(p *player) { p.derived.MaxHP = 1200 }, func(u model.MemberUpdate) bool { return u.MaxHP == 1200 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			i, p, h := setup()
			run(i, 1, func(uint64) { p.HP-- }) // a hit, held for the window
			if len(h.updates) != 0 {
				t.Fatalf("a hit 1 tick after the last update went out at once: %+v", h.updates)
			}
			run(i, 1, func(uint64) { tt.then(p) })
			if len(h.updates) != 1 || !tt.want(h.updates[0]) {
				t.Fatalf("updates %+v on the tick it changed; want one saying so", h.updates)
			}
		})
	}
	t.Run("up", func(t *testing.T) {
		i, p, h := setup()
		run(i, 1, func(uint64) { p.HP = 0 })
		run(i, 1, func(uint64) { p.HP = 300 }) // revived the tick after, inside the window
		if len(h.updates) != 2 || h.updates[0].HP != 0 || h.updates[1].HP != 300 {
			t.Fatalf("updates %+v; want down then up, each on its tick", h.updates)
		}
	})
}
