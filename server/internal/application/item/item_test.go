package item

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	model "finga/internal/model/item"
	"finga/internal/shared/facade"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/pkg/panics"
	"finga/pkg/uuidv7"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace/noop"
)

// fakeCases records what the facade passed in and answers with what each test sets.
type fakeCases struct {
	actor     model.Actor
	owner     kernel.CharacterID
	in        any
	drops     []AddInput
	asked     []QuestCompletion
	inventory model.Inventory
	result    Changes
	batch     DropBatch
	handedIn  []HandIn
	account   kernel.AccountID
	pieces    []model.Piece
	granted   []model.PieceGrant
	look      model.Look
	err       error
	calls     int
}

func (f *fakeCases) AddToWardrobe(_ context.Context, actor model.Actor, in WardrobeInput) (Changes, error) {
	f.calls++
	f.actor, f.in = actor, in
	return f.result, f.err
}

func (f *fakeCases) SetLook(_ context.Context, actor model.Actor, in LookInput) (Changes, error) {
	f.calls++
	f.actor, f.in = actor, in
	return f.result, f.err
}

func (f *fakeCases) GrantPieces(_ context.Context, account kernel.AccountID, grants []model.PieceGrant) ([]model.PieceGrant, error) {
	f.calls++
	f.account, f.in = account, grants
	return f.granted, f.err
}

func (f *fakeCases) Pieces(_ context.Context, account kernel.AccountID) ([]model.Piece, error) {
	f.calls++
	f.account = account
	return f.pieces, f.err
}

func (f *fakeCases) Look(_ context.Context, character kernel.CharacterID) (model.Look, error) {
	f.calls++
	f.owner = character
	return f.look, f.err
}

func (f *fakeCases) Inventory(_ context.Context, owner kernel.CharacterID) (model.Inventory, error) {
	f.calls++
	f.owner = owner
	return f.inventory, f.err
}

func (f *fakeCases) Add(_ context.Context, actor model.Actor, in AddInput) (Changes, error) {
	f.calls++
	f.actor, f.in = actor, in
	return f.result, f.err
}

func (f *fakeCases) AddMany(_ context.Context, actor model.Actor, drops []AddInput) (DropBatch, error) {
	f.calls++
	f.actor, f.drops = actor, drops
	return f.batch, f.err
}

func (f *fakeCases) Move(_ context.Context, actor model.Actor, in MoveInput) (Changes, error) {
	f.calls++
	f.actor, f.in = actor, in
	return f.result, f.err
}

func (f *fakeCases) Discard(_ context.Context, actor model.Actor, in DiscardInput) (Changes, error) {
	f.calls++
	f.actor, f.in = actor, in
	return f.result, f.err
}

func (f *fakeCases) DiscardMany(_ context.Context, actor model.Actor, in DiscardManyInput) (Changes, error) {
	f.calls++
	f.actor, f.in = actor, in
	return f.result, f.err
}

func (f *fakeCases) Use(_ context.Context, actor model.Actor, in UseInput) (Changes, bool, error) {
	f.calls++
	f.actor, f.in = actor, in
	return f.result, true, f.err
}

func (f *fakeCases) GrantStarter(_ context.Context, actor model.Actor) (Changes, error) {
	f.calls++
	f.actor = actor
	return f.result, f.err
}

func (f *fakeCases) Equip(_ context.Context, actor model.Actor, in EquipInput) (Changes, error) {
	f.calls++
	f.actor, f.in = actor, in
	return f.result, f.err
}

func (f *fakeCases) Unequip(_ context.Context, actor model.Actor, in UnequipInput) (Changes, error) {
	f.calls++
	f.actor, f.in = actor, in
	return f.result, f.err
}

func (f *fakeCases) HandIn(_ context.Context, actor model.Actor, in HandInInput) (Changes, error) {
	f.calls++
	f.actor, f.in = actor, in
	return f.result, f.err
}

func (f *fakeCases) HandedIn(_ context.Context, owner kernel.CharacterID, asked []QuestCompletion) ([]HandIn, error) {
	f.calls++
	f.owner, f.asked = owner, asked
	return f.handedIn, f.err
}

func newModule(t *testing.T, svc useCases) *Module {
	t.Helper()
	calls, err := facade.New(facade.Options{Module: "item", Traces: noop.NewTracerProvider(), Registry: prometheus.NewRegistry(), Panics: panics.NewRecorder(slog.New(slog.DiscardHandler), nil), Errors: errorTable})
	if err != nil {
		t.Fatal(err)
	}
	return &Module{calls: calls, svc: svc}
}

var (
	actor   = Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
	key     = "k-" + uuidv7.NewString()
	sword   = model.Item{ID: kernel.NewItemID(), TemplateID: "iron_sword", Owner: actor.Character, Tab: gamedata.Equip, Slot: 2, Quantity: 1, Stats: &progression.ItemStats{PhysicalAttack: 7}}
	worn    = model.Item{ID: kernel.NewItemID(), TemplateID: "leather_cap", Owner: actor.Character, Quantity: 1, EquipSlot: "hat"}
	changed = Changes{
		Changed:   []model.Slot{{Tab: gamedata.Equip, Slot: 1}, {Tab: gamedata.Equip, Slot: 2, Item: &sword}},
		Equipment: []model.WornSlot{{Slot: "hat", Item: &worn}, {Slot: "weapon"}},
		Worn:      []model.Item{worn},
		Revision:  12,
	}
)

// wantChanged checks res is changed, as callers see it: the owner left out, every slot and item as it was.
func wantChanged(t *testing.T, res Result) {
	t.Helper()
	swordAt, wornAt := itemOf(sword), itemOf(worn)
	want := Result{
		Changed:   []Slot{{Tab: gamedata.Equip, Slot: 1}, {Tab: gamedata.Equip, Slot: 2, Item: &swordAt}},
		Equipment: []EquipSlot{{Slot: "hat", Item: &wornAt}, {Slot: "weapon"}},
		Worn:      []Item{wornAt},
		Revision:  12,
	}
	same := res.Revision == want.Revision && slices.Equal(res.Worn, want.Worn) &&
		slices.EqualFunc(res.Changed, want.Changed, func(a, b Slot) bool { return a.Tab == b.Tab && a.Slot == b.Slot && equalAt(a.Item, b.Item) }) &&
		slices.EqualFunc(res.Equipment, want.Equipment, func(a, b EquipSlot) bool { return a.Slot == b.Slot && equalAt(a.Item, b.Item) })
	if !same {
		t.Fatalf("got %+v, want %+v", res, want)
	}
}

func equalAt(a, b *Item) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func TestEachCallConvertsToAndFromTheModel(t *testing.T) {
	ctx := t.Context()
	f := &fakeCases{result: changed}
	m := newModule(t, f)
	gift := uuidv7.New()
	t.Run("Inventory", func(t *testing.T) {
		f.inventory = model.Inventory{Revision: 7, Items: []model.Item{sword, worn}}
		inv, err := m.Inventory(ctx, actor.Character)
		if err != nil || f.owner != actor.Character || inv.Revision != 7 || !slices.Equal(inv.Items, []Item{itemOf(sword), itemOf(worn)}) {
			t.Fatalf("got %+v for %v, err %v", inv, f.owner, err)
		}
		if got := inv.Items[0]; got.ID != sword.ID || got.Stats != sword.Stats || got.Tab != gamedata.Equip || got.Slot != 2 {
			t.Fatalf("the sword reads %+v", got)
		}
	})
	t.Run("Add", func(t *testing.T) {
		res, err := m.Add(ctx, actor, AddRequest{Key: key, TemplateID: "wooden_sword", Quantity: 1, Cause: Cause{Type: CausePromotion, ID: gift}, Stats: sword.Stats})
		want := AddInput{Key: key, TemplateID: "wooden_sword", Quantity: 1, Cause: model.Cause{Type: model.CausePromotion, ID: gift}, Stats: sword.Stats}
		if err != nil || f.in != want || f.actor != model.Actor(actor) {
			t.Fatalf("sent %+v for %+v, err %v", f.in, f.actor, err)
		}
		wantChanged(t, res)
	})
	t.Run("Move, Discard, DiscardMany and Use pass their requests as they are", func(t *testing.T) {
		for _, tt := range []struct {
			call func() (Result, error)
			want any
		}{
			{func() (Result, error) {
				return m.Move(ctx, actor, MoveRequest{Key: key, ItemID: sword.ID, Tab: gamedata.Equip, Slot: 4})
			}, MoveInput{Key: key, ItemID: sword.ID, Tab: gamedata.Equip, Slot: 4}},
			{func() (Result, error) {
				return m.Discard(ctx, actor, DiscardRequest{Key: key, ItemID: sword.ID, Quantity: 1})
			}, DiscardInput{Key: key, ItemID: sword.ID, Quantity: 1}},
			{func() (Result, error) {
				res, replayed, err := m.Use(ctx, actor, UseRequest{Key: key, ItemID: sword.ID, Template: "red_potion"})
				if !replayed {
					t.Error("Use lost replayed")
				}
				return res, err
			}, UseInput{Key: key, ItemID: sword.ID, Template: "red_potion"}},
		} {
			res, err := tt.call()
			if err != nil || f.in != tt.want {
				t.Fatalf("sent %+v, want %+v, err %v", f.in, tt.want, err)
			}
			wantChanged(t, res)
		}
		ids := []kernel.ItemID{sword.ID, worn.ID}
		res, err := m.DiscardMany(ctx, actor, DiscardManyRequest{Key: key, ItemIDs: ids})
		if in, ok := f.in.(DiscardManyInput); err != nil || !ok || in.Key != key || !slices.Equal(in.ItemIDs, ids) {
			t.Fatalf("sent %+v, err %v", f.in, err)
		}
		wantChanged(t, res)
	})
	t.Run("Equip and Unequip", func(t *testing.T) {
		r := EquipRequest{Key: key, ItemID: sword.ID, EquipSlot: "weapon", Level: 9, Class: "warrior", Stats: gamedata.Stats{STR: 4}}
		res, err := m.Equip(ctx, actor, r)
		if err != nil || f.in != EquipInput(r) {
			t.Fatalf("sent %+v, err %v", f.in, err)
		}
		wantChanged(t, res)
		to := 6
		res, err = m.Unequip(ctx, actor, UnequipRequest{Key: key, EquipSlot: "hat", ToSlot: &to})
		if in, ok := f.in.(UnequipInput); err != nil || !ok || in.EquipSlot != "hat" || in.ToSlot != &to {
			t.Fatalf("sent %+v, err %v", f.in, err)
		}
		wantChanged(t, res)
	})
	t.Run("GrantStarter", func(t *testing.T) {
		res, err := m.GrantStarter(ctx, actor)
		if err != nil || f.actor != model.Actor(actor) {
			t.Fatalf("for %+v, err %v", f.actor, err)
		}
		wantChanged(t, res)
	})
	t.Run("HandIn and HandedIn", func(t *testing.T) {
		r := HandInRequest{Quest: "sticky_samples", Completion: 2, Take: []gamedata.Grant{{Item: "slime_gel", Quantity: 5}}, Give: []Give{{TemplateID: "leather_cap", Quantity: 1, Stats: sword.Stats}}}
		res, err := m.HandIn(ctx, actor, r)
		in, ok := f.in.(HandInInput)
		if err != nil || !ok || in.Quest != "sticky_samples" || in.Completion != 2 || !slices.Equal(in.Take, r.Take) || !slices.Equal(in.Give, []RewardLine{{TemplateID: "leather_cap", Quantity: 1, Stats: sword.Stats}}) {
			t.Fatalf("sent %+v, err %v", f.in, err)
		}
		wantChanged(t, res)
		f.handedIn = []HandIn{{Quest: "sticky_samples", Completion: 2, ID: gift, Result: changed}}
		found, err := m.HandedIn(ctx, actor.Character, []Completion{{Quest: "sticky_samples", Completion: 2}, {Quest: "slime_cleanup", Completion: 1}})
		if err != nil || len(found) != 1 || found[0].Quest != "sticky_samples" || found[0].Completion != 2 || !slices.Equal(f.asked, []QuestCompletion{{Quest: "sticky_samples", Completion: 2}, {Quest: "slime_cleanup", Completion: 1}}) {
			t.Fatalf("found %+v asking %+v, err %v", found, f.asked, err)
		}
		wantChanged(t, found[0].Result)
	})
	t.Run("GrantPieces", func(t *testing.T) {
		f.granted = []model.PieceGrant{{Piece: "helm_gold", Cause: model.PieceFromRank, Ref: "gold"}}
		added, err := m.GrantPieces(ctx, actor.Account, []PieceGrant{{Piece: "helm_silver", Cause: PieceFromRank, Ref: "silver"}, {Piece: "helm_gold", Cause: PieceFromRank, Ref: "gold"}})
		in, ok := f.in.([]model.PieceGrant)
		if err != nil || !ok || f.account != actor.Account || !slices.Equal(in, []model.PieceGrant{{Piece: "helm_silver", Cause: model.PieceFromRank, Ref: "silver"}, {Piece: "helm_gold", Cause: model.PieceFromRank, Ref: "gold"}}) {
			t.Fatalf("sent %+v for %v, err %v", f.in, f.account, err)
		}
		if !slices.Equal(added, []PieceGrant{{Piece: "helm_gold", Cause: "rank", Ref: "gold"}}) {
			t.Fatalf("added %+v", added)
		}
	})
}

// A key, as every call that takes one checks it: a UUID, optionally k- prefixed.
func TestCheckKey(t *testing.T) {
	for _, tt := range []struct {
		key string
		ok  bool
	}{
		{uuidv7.NewString(), true},
		{"k-" + uuidv7.NewString(), true},
		{"", false},
		{"discard-1", false},
		{"k-" + strings.Repeat("x", 36), false},
		{key + strings.Repeat("x", 4096), false},
	} {
		if err := errorTable.Kernel(checkKey(tt.key)); (err == nil) != tt.ok || err != nil && !errors.Is(err, kernel.ErrBadIdempotencyKey) {
			t.Errorf("key %.40q: got %v", tt.key, err)
		}
	}
}

// A bad key or cause is refused at the facade, before the module runs.
func TestAMalformedRequestNeverReachesTheModule(t *testing.T) {
	f := &fakeCases{}
	m := newModule(t, f)
	ctx := t.Context()
	for _, tt := range []struct {
		name string
		call func() error
		want error
	}{
		{"an Add without a key", func() error { _, err := m.Add(ctx, actor, AddRequest{Cause: Cause{Type: CauseDrop}}); return err }, kernel.ErrBadIdempotencyKey},
		{"an Add without a cause", func() error { _, err := m.Add(ctx, actor, AddRequest{Key: key}); return err }, kernel.ErrBadCause},
		{"an Add without a key or a cause, which names its cause as the handler does", func() error { _, err := m.Add(ctx, actor, AddRequest{}); return err }, kernel.ErrBadCause},
		{"a quest's cause, which only a hand-in names", func() error {
			_, err := m.Add(ctx, actor, AddRequest{Key: key, Cause: Cause{Type: CauseType(model.CauseQuest)}})
			return err
		}, kernel.ErrBadCause},
		{"a Move", func() error { _, err := m.Move(ctx, actor, MoveRequest{Key: "nope"}); return err }, kernel.ErrBadIdempotencyKey},
		{"a Discard", func() error { _, err := m.Discard(ctx, actor, DiscardRequest{}); return err }, kernel.ErrBadIdempotencyKey},
		{"a DiscardMany", func() error { _, err := m.DiscardMany(ctx, actor, DiscardManyRequest{}); return err }, kernel.ErrBadIdempotencyKey},
		{"a Use", func() error { _, _, err := m.Use(ctx, actor, UseRequest{}); return err }, kernel.ErrBadIdempotencyKey},
		{"an Equip", func() error { _, err := m.Equip(ctx, actor, EquipRequest{}); return err }, kernel.ErrBadIdempotencyKey},
		{"an Unequip", func() error { _, err := m.Unequip(ctx, actor, UnequipRequest{}); return err }, kernel.ErrBadIdempotencyKey},
		{"an AddMany of nothing", func() error { _, err := m.AddMany(ctx, actor, nil); return err }, kernel.ErrBadBatchSize},
		{"an AddMany of too many", func() error { _, err := m.AddMany(ctx, actor, make([]AddRequest, MaxBatch+1)); return err }, kernel.ErrBadBatchSize},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
	if f.calls != 0 {
		t.Fatalf("the module was called %d times", f.calls)
	}
}

// Each drop gets its outcome in order: a malformed one is refused alone before the module runs the rest, and the module's refusals reach callers as kernel errors.
func TestAddManyAnswersEachDrop(t *testing.T) {
	ok := func(key string) AddRequest {
		return AddRequest{Key: key, TemplateID: "slime_gel", Quantity: 1, Cause: Cause{Type: CauseDrop}}
	}
	key2, key3 := "k-"+uuidv7.NewString(), "k-"+uuidv7.NewString()
	f := &fakeCases{batch: DropBatch{
		Outcomes: []DropOutcome{{}, {Replayed: true, Result: changed}, {Err: fmt.Errorf("add: %w", model.ErrEtcTabFull)}},
		Changed:  changed.Changed,
		Revision: 12,
	}}
	m := newModule(t, f)
	b, err := m.AddMany(t.Context(), actor, []AddRequest{ok(key), ok("not-a-uuid"), ok(key2), {Key: key3, TemplateID: "slime_gel", Quantity: 1}, ok(key3)})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.drops) != 3 || f.drops[0].Key != key || f.drops[1].Key != key2 || f.drops[2].Key != key3 || f.drops[0].Cause.Type != model.CauseDrop {
		t.Fatalf("the module got %+v", f.drops)
	}
	refusals := []error{nil, kernel.ErrBadIdempotencyKey, nil, kernel.ErrBadCause, kernel.ErrEtcTabFull}
	for i, want := range refusals {
		if got := b.Outcomes[i].Err; want == nil && got != nil || want != nil && !errors.Is(got, want) {
			t.Fatalf("drop %d refused by %v, want %v", i, got, want)
		}
	}
	if !errors.Is(b.Outcomes[4].Err, model.ErrEtcTabFull) || !b.Outcomes[2].Replayed || b.Revision != 12 || len(b.Changed) != 2 {
		t.Fatalf("batch %+v", b)
	}
	wantChanged(t, b.Outcomes[2].Result)

	f.calls = 0
	if b, err := m.AddMany(t.Context(), actor, []AddRequest{ok("bad"), {}}); err != nil || f.calls != 0 || !errors.Is(b.Outcomes[0].Err, kernel.ErrBadIdempotencyKey) || !errors.Is(b.Outcomes[1].Err, kernel.ErrBadIdempotencyKey) {
		t.Fatalf("a batch with no drop left, the second without a key or a cause, as the handler names: %+v after %d calls, err %v", b, f.calls, err)
	}
}

// Callers find the kernel error with errors.Is, and the logs keep the model's cause.
func TestEveryModelErrorReachesCallersAsItsKernelError(t *testing.T) {
	for _, row := range errorTable {
		t.Run(row.Model.Error(), func(t *testing.T) {
			m := newModule(t, &fakeCases{err: fmt.Errorf("move: %w", row.Model)})
			_, err := m.Move(t.Context(), actor, MoveRequest{Key: key, ItemID: sword.ID})
			if !errors.Is(err, row.Kernel) || !errors.Is(err, row.Model) {
				t.Fatalf("got %v, want %v wrapping %v", err, row.Kernel, row.Model)
			}
		})
	}
	m := newModule(t, &fakeCases{err: model.ErrItemWorn})
	if _, err := m.Discard(t.Context(), actor, DiscardRequest{Key: key, ItemID: worn.ID, Quantity: 1}); !errors.Is(err, kernel.ErrItemWorn) {
		t.Fatalf("a worn item: got %v", err)
	}
}
