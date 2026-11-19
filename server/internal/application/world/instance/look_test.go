package instance_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// fakeLooks records each look asked for and answers with answer, or err; with a gate, once it closes.
type fakeLooks struct {
	mu     sync.Mutex
	calls  []model.LookCall
	answer model.Look
	err    error
	gate   chan struct{}
}

func (f *fakeLooks) SetLook(ctx context.Context, c model.LookCall) (model.Look, error) {
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return model.Look{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	return f.answer, f.err
}

func (f *fakeLooks) called() []model.LookCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.LookCall(nil), f.calls...)
}

func setLookAsync(inst *instance.Instance, id kernel.CharacterID, key string, look model.Look) <-chan error {
	done := make(chan error, 1)
	go func() { done <- inst.SetLook(context.Background(), id, "c1", key, look) }()
	return done
}

// setLook asks for look on the next tick and returns its answer once the tick after applies it.
func setLook(inst *instance.Instance, id kernel.CharacterID, key string, look model.Look) error {
	done := setLookAsync(inst, id, key, look)
	tick(2)
	return <-done
}

// joinedIn is the look s's snapshot showed id in.
func joinedIn(s *worldtest.Sink, id kernel.CharacterID) model.Look {
	for _, snap := range eventsOf[model.Snapshot](s) {
		for _, v := range snap.Players {
			if v.ID == id {
				return v.Look
			}
		}
	}
	return model.Look{}
}

// A player joins in their look, which everyone on the map sees; a look worn through the item module, with the live class, shows in the next view, and one no newer than theirs changes nothing.
func TestALookIsWornAndEveryoneSeesIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		looks := &fakeLooks{answer: model.Look{Outfit: "knight_plate", Weapon: "greatsword", HideHeadwear: true, Revision: 2}}
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), arena(t, time.Hour), nil, worldtest.Options{Looks: looks, ItemCalls: runningPool(t)})
		defer stop()
		tick(1)
		ayla := caster(farFromSlime)
		ayla.Account, ayla.Look = kernel.NewAccountID(), model.Look{Outfit: "rogue_leathers", Revision: 1}
		join(t, inst, ayla)
		bram := caster(farFromSlime + 1)
		bram.Name = "Bram"
		sb := join(t, inst, bram)
		if seen := joinedIn(sb, ayla.ID); seen != ayla.Look {
			t.Fatalf("Bram's snapshot shows Ayla in %+v, want the look she joined in", seen)
		}
		if err := setLook(inst, ayla.ID, "k1", model.Look{Outfit: "knight_plate", Weapon: "greatsword", HideHeadwear: true}); err != nil {
			t.Fatal(err)
		}
		if got := looks.called(); len(got) != 1 || got[0].Key != "k1" || got[0].Account != ayla.Account || got[0].Character != ayla.ID || got[0].Class != "warrior" || got[0].Look.Weapon != "greatsword" {
			t.Fatalf("looks asked %+v", got)
		}
		if v, _ := sb.Own(ayla.ID); v.Look != looks.answer {
			t.Fatalf("Bram sees Ayla in %+v, want %+v", v.Look, looks.answer)
		}
		looks.answer = model.Look{Outfit: "mage_robe", Revision: 2} // a reply no newer than the look worn
		if err := setLook(inst, ayla.ID, "k2", model.Look{Outfit: "mage_robe"}); err != nil {
			t.Fatal(err)
		}
		if v, _ := sb.Own(ayla.ID); v.Look.Outfit != "knight_plate" {
			t.Fatalf("a stale reply put Ayla in %+v", v.Look)
		}
	})
}

// A look is refused to a player in combat or dead, before it reaches the item module; the item module's refusal reaches the caller.
func TestALookIsRefusedInCombatOrDead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		looks := &fakeLooks{err: errors.New("the item module refused")}
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), arena(t, time.Hour), nil, worldtest.Options{Looks: looks, Transfer: &transferRecorder{}, ItemCalls: runningPool(t)})
		defer stop()
		tick(1)
		calm := caster(farFromSlime)
		join(t, inst, calm)
		if err := setLook(inst, calm.ID, "k1", model.Look{}); !errors.Is(err, looks.err) {
			t.Fatalf("out of combat: %v, want the item module's refusal", err)
		}
		hit := caster(nextToSlime)
		hit.Name, hit.HP = "Bram", 1
		s := join(t, inst, hit)
		mobHitsOn(t, s, hit.ID, 1)
		untilDead(t, s, hit.ID)
		if err := setLook(inst, hit.ID, "k2", model.Look{}); !errors.Is(err, model.ErrCharacterDead) {
			t.Fatalf("dead: %v, want ErrCharacterDead", err)
		}
		fighter := caster(nextToSlime)
		fighter.Name = "Cleo"
		sf := join(t, inst, fighter)
		mobHitsOn(t, sf, fighter.ID, 1)
		if err := setLook(inst, fighter.ID, "k3", model.Look{}); !errors.Is(err, model.ErrInCombat) {
			t.Fatalf("in combat: %v, want ErrInCombat", err)
		}
		if got := looks.called(); len(got) != 1 {
			t.Fatalf("the item module was asked %d times, want once: the dead and the fighting never reach it", len(got))
		}
	})
}

// A promotion wears the look the trainer's gift left, in the same tick as the new class: a weapon look the class can't hold goes with it.
func TestAPromotionWearsTheLookItsGiftLeft(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := trainee(t)
		p.Look = model.Look{Outfit: "mage_robe", Weapon: "oak_wand", Revision: 1}
		gifts := &fakeGifts{answer: model.Gift{Look: model.Look{Outfit: "mage_robe", Revision: 2}}}
		inst, s, _, _ := promotionTown(t, p, gifts)
		done := promoteAsync(inst, p.ID, "warrior_trainer", "warrior")
		tick(2)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if got := gifts.called(); len(got) != 1 || got[0].Promotes != "warrior" {
			t.Fatalf("gifts %+v, want one naming the Warrior", got)
		}
		for _, d := range eventsOf[worldtest.Delta](s) {
			for _, v := range d.Players {
				if v.ID == p.ID && v.Class == "warrior" && v.Look != gifts.answer.Look {
					t.Fatalf("the first view as a Warrior shows %+v, want the gift's %+v", v.Look, gifts.answer.Look)
				}
			}
		}
		if v, _ := s.Own(p.ID); v.Class != "warrior" || v.Look != gifts.answer.Look {
			t.Fatalf("after the promotion %+v in %+v", v.Class, v.Look)
		}
	})
}

// A look and a promotion never overlap, as each is checked against the class the other may change: whichever is in flight refuses the other before it reaches the item module, and the other goes once it settles.
func TestALookAndAPromotionWaitForEachOther(t *testing.T) {
	for _, first := range []string{"a look", "a promotion"} {
		t.Run(first+" first", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := trainee(t)
				gifts, looks := &fakeGifts{gate: make(chan struct{})}, &fakeLooks{gate: make(chan struct{})}
				inst, stop := worldtest.Run(t, worldtest.LoadData(t), &worldtest.Checkpoints{}, worldtest.Options{Gifts: gifts, Looks: looks, CheckpointInterval: time.Hour, ItemCalls: runningPool(t)})
				defer stop()
				join(t, inst, p)
				look := func() <-chan error { return setLookAsync(inst, p.ID, "k1", model.Look{Weapon: "oak_wand"}) }
				promote := func() <-chan error { return promoteAsync(inst, p.ID, "warrior_trainer", "warrior") }
				held, other := look, promote
				if first == "a promotion" {
					held, other = promote, look
				}
				answer := held()
				tick(1)
				refused := other()
				tick(1)
				if err := <-refused; !errors.Is(err, transient.ErrUnavailable) {
					t.Fatalf("the other while %s is in flight: %v, want unavailable", first, err)
				}
				close(gifts.gate)
				close(looks.gate)
				tick(2)
				if err := <-answer; err != nil {
					t.Fatal(err)
				}
				if g, l := len(gifts.called()), len(looks.called()); g+l != 1 {
					t.Fatalf("%d gifts and %d looks reached the item module, want %s's alone", g, l, first)
				}
				again := other()
				tick(2)
				if err := <-again; err != nil {
					t.Fatalf("the other once %s settled: %v", first, err)
				}
			})
		})
	}
}

// botLookOf is the look bot shows in a duel with Ayla.
func botLookOf(t *testing.T, data *gamedata.Data, bot *model.Bot) model.Look {
	var look model.Look
	synctest.Test(t, func(t *testing.T) {
		ayla := fighterFrom(t, data, "Ayla")
		inst, stop := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Bot: bot}), nil, worldtest.Options{})
		defer stop()
		sink := &worldtest.Sink{}
		if err := inst.Join(context.Background(), ayla, sink); err != nil {
			t.Fatal(err)
		}
		sleep(worldtest.TickInterval)
		v, ok := sink.Own(bot.ID)
		if !ok {
			t.Fatal("the bot never showed")
		}
		look = v.Look
	})
	return look
}

// A bot wears a look seeded by its id: the same each match, each pick its class's own or a piece a boss drops, of its slot, a weapon look of its class's kind.
func TestABotWearsALookSeededByItsID(t *testing.T) {
	data := worldtest.LoadData(t)
	first := botOf(data, "Ash")
	if a, b := botLookOf(t, data, first), botLookOf(t, data, first); a != b {
		t.Fatalf("one id wore %+v, then %+v", a, b)
	}
	class, _ := data.Class(first.Class)
	picked := 0
	for range 12 {
		look := botLookOf(t, data, botOf(data, "Ash"))
		for _, pick := range []struct {
			slot gamedata.LookSlot
			id   string
		}{{gamedata.OutfitSlot, look.Outfit}, {gamedata.HeadwearSlot, look.Headwear}, {gamedata.FaceSlot, look.Face}, {gamedata.WeaponSlot, look.Weapon}, {gamedata.WingsSlot, look.Wings}} {
			if pick.id == "" {
				continue
			}
			picked++
			p, ok := data.Piece(pick.id)
			if !ok || p.Slot != pick.slot || len(p.From) == 0 || p.From[0].Boss == "" || p.Slot == gamedata.WeaponSlot && p.Kind != class.Weapon {
				t.Errorf("a bot wears %q as its %s: %+v", pick.id, pick.slot, p)
			}
		}
	}
	if picked == 0 {
		t.Fatal("twelve bots wore nothing but their class's own")
	}
}
