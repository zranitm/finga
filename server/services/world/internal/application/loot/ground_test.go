package loot_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/world/internal/application/economy"
	"finga/services/world/internal/application/loot"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

const tick = 50 * time.Millisecond

// A slime that always drops two gels; at a roll of 0 they land 24 px left of it.
var slime = gamedata.Mob{ID: "green_slime", Drops: []gamedata.Drop{{Item: "slime_gel", Chance: 1, Min: 2, Max: 2}}}

const dropX, dropY = 176.0, 400.0

var (
	errFull    = errors.New("inventory full")
	errDown    = errors.New("item service down")
	errGranted = errors.New("granted by an earlier try")
	gel        = []model.Slot{{Tab: gamedata.Etc, Slot: 0, Item: &model.SlotItem{Template: "slime_gel", Quantity: 2}}}
	potion     = model.Slot{Tab: gamedata.Use, Slot: 3, Item: &model.SlotItem{Template: "red_potion", Quantity: 1}} // not the drop's: a newer move may have put it elsewhere
)

type answer struct {
	slots []model.Slot
	err   error
}

// pickups is the item service: each Add gets the next answer, the last one again once they run out.
type pickups struct {
	mu        sync.Mutex
	answers   []answer
	calls     []model.Pickup
	inventory answer
}

func (p *pickups) Add(_ context.Context, in model.Pickup) ([]model.Slot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, in)
	a := p.answers[min(len(p.calls), len(p.answers))-1]
	return a.slots, a.err
}

func (p *pickups) Retryable(err error) bool { return errors.Is(err, errDown) }
func (p *pickups) Granted(err error) bool   { return errors.Is(err, errGranted) }

func (p *pickups) Inventory(context.Context, kernel.CharacterID) ([]model.Slot, error) {
	return p.inventory.slots, p.inventory.err
}

func (p *pickups) added() []model.Pickup {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]model.Pickup(nil), p.calls...)
}

// parties puts the killer and the mate in one party.
type parties struct{ killer, mate kernel.CharacterID }

func (pt parties) SameParty(a, b kernel.CharacterID) bool {
	in := func(id kernel.CharacterID) bool { return id == pt.killer || id == pt.mate }
	return in(a) && in(b)
}

type jobs struct{}

func (jobs) JobStarted()  {}
func (jobs) JobFinished() {}

type scene struct {
	g                      *loot.Ground
	items                  *pickups
	workers                *economy.Workers
	killer, mate, stranger loot.Picker
}

// newScene is a ground whose slime the killer just killed at tick 1; run starts the workers that grant pickups.
func newScene(t *testing.T, items *pickups, run bool) scene {
	t.Helper()
	workers := economy.NewWorkers(economy.Deps{Observer: jobs{}, Panics: worldtest.DiscardPanics()}, economy.Options{Workers: 1})
	if run {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = workers.Run(ctx) }()
		t.Cleanup(func() { cancel(); <-done })
	}
	at := func() loot.Picker {
		return loot.Picker{ID: kernel.NewCharacterID(), Account: kernel.NewAccountID(), X: dropX, Y: dropY}
	}
	s := scene{items: items, workers: workers, killer: at(), mate: at(), stranger: at()}
	s.g = loot.NewGround(loot.Deps{Items: items, Workers: workers, Parties: parties{s.killer.ID, s.mate.ID}, Traces: noop.NewTracerProvider(), Roll: worldtest.FixedRoll{V: 0}}, loot.Tuning{TickInterval: tick})
	s.g.Begin(1)
	s.g.Roll(slime, 200, dropY, s.killer.ID)
	return s
}

// pick has p reach for a drop and waits for the grant; ok is false when nothing was reserved or the grant has not run.
func (s scene) pick(t *testing.T, p loot.Picker) (loot.Result, bool) {
	t.Helper()
	results := make(chan loot.Result, 1)
	if err := s.g.PickUp(p, func(_ context.Context, r loot.Result) { results <- r }); err != nil {
		t.Fatal(err)
	}
	synctest.Wait()
	select {
	case r := <-results:
		return r, true
	default:
		return loot.Result{}, false
	}
}

func TestADropIsTheKillersPartysUntilItsWindowEnds(t *testing.T) {
	after := 1 + uint64(10*time.Second/tick)
	tests := []struct {
		name  string
		who   func(s scene) loot.Picker
		at    uint64
		taken bool
	}{
		{"the killer", func(s scene) loot.Picker { return s.killer }, 2, true},
		{"the killer's party", func(s scene) loot.Picker { return s.mate }, 2, true},
		{"anyone else", func(s scene) loot.Picker { return s.stranger }, 2, false},
		{"anyone else, once the window ends", func(s scene) loot.Picker { return s.stranger }, after, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newScene(t, &pickups{answers: []answer{{slots: gel}}}, true)
				s.g.Begin(tt.at)
				_, taken := s.pick(t, tt.who(s))
				if taken != tt.taken {
					t.Fatalf("taken %v, want %v", taken, tt.taken)
				}
			})
		})
	}
}

func TestAPickerReachesOnlyNearbyDrops(t *testing.T) {
	for dx, taken := range map[float64]bool{39: true, 41: false} {
		synctest.Test(t, func(t *testing.T) {
			s := newScene(t, &pickups{answers: []answer{{slots: gel}}}, true)
			p := s.killer
			p.X += dx
			if _, got := s.pick(t, p); got != taken {
				t.Fatalf("%v px away: taken %v, want %v", dx, got, taken)
			}
		})
	}
}

// A drop whose pickup is on its way is offered to no one else, even the killer's party.
func TestADropOnItsWayIsNotOfferedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newScene(t, &pickups{answers: []answer{{slots: gel}}}, false) // no workers: the grant never runs
		if err := s.g.PickUp(s.killer, func(context.Context, loot.Result) {}); err != nil {
			t.Fatal(err)
		}
		if err := s.g.PickUp(s.mate, func(context.Context, loot.Result) { t.Error("the mate got the killer's drop") }); err != nil {
			t.Fatal(err)
		}
		if len(s.g.Views()) != 1 {
			t.Fatalf("drops %+v; want the gel still on the ground", s.g.Views())
		}
	})
}

func TestSettlingAPickup(t *testing.T) {
	tests := []struct {
		name      string
		items     *pickups
		event     model.Event // nil when the picker is told nothing
		adds      int         // calls to the item service
		gone      bool        // the drop left the ground
		mateTakes bool        // left on the ground, it is free for the killer's party again
	}{
		{"granted", &pickups{answers: []answer{{slots: gel}}}, model.InventoryChanged{Tick: 1, Slots: gel}, 1, true, false},
		{"granted with nothing to show", &pickups{answers: []answer{{}}}, nil, 1, true, false},
		{"refused", &pickups{answers: []answer{{err: errFull}}}, model.PickupFailed{Tick: 1, Err: errFull}, 1, false, true},
		{"granted by a try whose reply was lost", &pickups{answers: []answer{{err: errGranted}}, inventory: answer{slots: append(slices.Clone(gel), potion)}}, model.InventoryChanged{Tick: 1, Slots: gel}, 1, true, false},
		{"granted, with the inventory unreadable", &pickups{answers: []answer{{err: errGranted}}, inventory: answer{err: errDown}}, nil, 1, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newScene(t, tt.items, true)
				r, ok := s.pick(t, s.killer)
				if !ok {
					t.Fatal("the killer's pickup never came back")
				}
				event, told := s.g.Settle(r)
				if told != (tt.event != nil) || !reflect.DeepEqual(event, tt.event) {
					t.Fatalf("told %v %#v, want %#v", told, event, tt.event)
				}
				if adds := len(tt.items.added()); adds != tt.adds {
					t.Fatalf("%d calls to the item service, want %d", adds, tt.adds)
				}
				if gone := len(s.g.Views()) == 0; gone != tt.gone {
					t.Fatalf("drops left %+v; gone want %v", s.g.Views(), tt.gone)
				}
				if _, removed := s.g.Changes(); (len(removed) == 1) != tt.gone {
					t.Fatalf("removed %v this tick", removed)
				}
				if tt.gone {
					return
				}
				if _, taken := s.pick(t, s.mate); taken != tt.mateTakes {
					t.Fatalf("the mate took it: %v, want %v", taken, tt.mateTakes)
				}
			})
		})
	}
}

// A grant whose outcome is unknown, the item service unreachable twice, holds its drop for the same picker and key.
func TestAnUnknownOutcomeKeepsTheDropForItsPicker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &pickups{answers: []answer{{err: errDown}, {err: errDown}, {slots: gel}}}
		s := newScene(t, items, true)
		r, _ := s.pick(t, s.killer)
		event, _ := s.g.Settle(r)
		if failed, ok := event.(model.PickupFailed); !ok || !errors.Is(failed.Err, model.ErrPickupsUnavailable) {
			t.Fatalf("told %#v; want the pickups unavailable", event)
		}
		if _, taken := s.pick(t, s.mate); taken {
			t.Fatal("the mate took a drop whose first grant may have landed")
		}
		if _, taken := s.pick(t, s.killer); !taken {
			t.Fatal("the killer could not try again")
		}
		calls := items.added()
		if len(calls) != 3 || calls[2].DropID != calls[0].DropID {
			t.Fatalf("calls %+v; want the retry under the first key", calls)
		}
	})
}

func TestADropPastItsLifeLeavesUnlessAPickupHoldsIt(t *testing.T) {
	late := 1 + uint64(60*time.Second/tick)
	for _, held := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			s := newScene(t, &pickups{answers: []answer{{slots: gel}}}, false)
			if held {
				_ = s.g.PickUp(s.killer, func(context.Context, loot.Result) {})
			}
			s.g.Begin(late - 1)
			s.g.Expire()
			if len(s.g.Views()) != 1 {
				t.Fatal("the drop left before its life ended")
			}
			s.g.Begin(late)
			s.g.Expire()
			if stays := len(s.g.Views()) == 1; stays != held {
				t.Fatalf("held %v: stays %v", held, stays)
			}
		})
	}
}

// late is the tick the slime's drop, rolled at tick 1, reaches the end of its life.
const late = 1 + uint64(60*time.Second/tick)

// unsettle has the killer reach for the drop with the item service down, so its outcome is unknown; done hears each later grant.
func (s scene) unsettle(t *testing.T) <-chan loot.Result {
	t.Helper()
	results := make(chan loot.Result, 1)
	if err := s.g.PickUp(s.killer, func(_ context.Context, r loot.Result) { results <- r }); err != nil {
		t.Fatal(err)
	}
	synctest.Wait()
	event, _ := s.g.Settle(<-results)
	if failed, ok := event.(model.PickupFailed); !ok || !errors.Is(failed.Err, model.ErrPickupsUnavailable) {
		t.Fatalf("told %#v; want the pickups unavailable", event)
	}
	return results
}

// A drop whose grant may have landed is not just dropped at the end of its life: a last grant under its key tells the picker.
func TestAnUnsettledDropTriesOnceMoreAsItExpires(t *testing.T) {
	tests := []struct {
		name  string
		after []answer // the item service's answers to the last try
		event model.Event
	}{
		{"the replay grants it", []answer{{slots: gel}}, model.InventoryChanged{Tick: late, Slots: gel}},
		{"the item service is still down", []answer{{err: errDown}, {err: errDown}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				items := &pickups{answers: append([]answer{{err: errDown}, {err: errDown}}, tt.after...)}
				s := newScene(t, items, true)
				results := s.unsettle(t)
				s.g.Begin(late)
				s.g.Expire()
				if _, removed := s.g.Changes(); len(s.g.Views()) != 0 || len(removed) != 1 {
					t.Fatalf("drops %+v removed %v; want the drop gone at the end of its life", s.g.Views(), removed)
				}
				synctest.Wait()
				var r loot.Result
				select {
				case r = <-results:
				default:
					t.Fatal("no last try")
				}
				event, told := s.g.Settle(r)
				if told != (tt.event != nil) || !reflect.DeepEqual(event, tt.event) {
					t.Fatalf("told %v %#v, want %#v", told, event, tt.event)
				}
				calls := items.added()
				if len(calls) != 2+len(tt.after) || calls[2].DropID != calls[0].DropID {
					t.Fatalf("calls %+v; want the last try under the first key", calls)
				}
			})
		})
	}
}

// With every worker busy, an unsettled drop stays past its life until one is free for its last try.
func TestAnUnsettledDropWaitsForAWorkerToTryOnceMore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newScene(t, &pickups{answers: []answer{{err: errDown}, {err: errDown}, {slots: gel}}}, true)
		results := s.unsettle(t)
		release := make(chan struct{})
		s.workers.Submit(func(context.Context) { <-release })
		synctest.Wait()
		for s.workers.Submit(func(context.Context) {}) {
		}
		s.g.Begin(late)
		s.g.Expire()
		if len(s.g.Views()) != 1 {
			t.Fatal("the drop left with no worker free for its last try")
		}
		close(release)
		synctest.Wait()
		s.g.Begin(late + 1)
		s.g.Expire()
		if len(s.g.Views()) != 0 {
			t.Fatal("the drop stayed once a worker was free")
		}
		synctest.Wait()
		if event, _ := s.g.Settle(<-results); !reflect.DeepEqual(event, model.InventoryChanged{Tick: late + 1, Slots: gel}) {
			t.Fatalf("told %#v; want the gel granted", event)
		}
	})
}
