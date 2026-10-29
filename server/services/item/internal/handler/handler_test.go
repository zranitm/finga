package handler_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/sentinels"
	"finga/services/item/internal/application"
	"finga/services/item/internal/handler"
	"finga/services/item/internal/model"
)

// fakeItems records what the handler asked and answers with result, or err.
type fakeItems struct {
	mu        sync.Mutex
	owner     kernel.CharacterID
	actor     model.Actor
	add       application.AddInput
	move      application.MoveInput
	discard   application.DiscardInput
	class     kernel.Class
	inventory []model.Item
	result    application.Result
	err       error
	calls     int
}

func (f *fakeItems) record() { f.calls++ }

func (f *fakeItems) Inventory(_ context.Context, owner kernel.CharacterID) ([]model.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.owner = owner
	return f.inventory, f.err
}

func (f *fakeItems) Add(_ context.Context, actor model.Actor, in application.AddInput) (application.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.add = actor, in
	return f.result, f.err
}

func (f *fakeItems) Move(_ context.Context, actor model.Actor, in application.MoveInput) (application.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.move = actor, in
	return f.result, f.err
}

func (f *fakeItems) Discard(_ context.Context, actor model.Actor, in application.DiscardInput) (application.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.discard = actor, in
	return f.result, f.err
}

func (f *fakeItems) GrantStarter(_ context.Context, actor model.Actor, class kernel.Class) (application.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.class = actor, class
	return f.result, f.err
}

func client(t *testing.T, f *fakeItems) itemv1.ItemServiceClient {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{Errors: handler.Errors, Logger: slog.New(slog.DiscardHandler)})
	itemv1.RegisterItemServiceServer(srv, handler.New(f))
	return itemv1.NewItemServiceClient(grpctest.Conn(t, srv))
}

var (
	account   = kernel.NewAccountID()
	character = kernel.NewCharacterID()
	actor     = &itemv1.Actor{AccountId: account.String(), CharacterId: character.String()}
	potion    = model.Item{ID: kernel.NewItemID(), TemplateID: "red_potion", Owner: character, Tab: gamedata.Use, Slot: 3, Quantity: 5}
	// changed is a move's result: slot 0 emptied, slot 3 holds the potion.
	changed = application.Result{Changed: []model.Slot{{Tab: gamedata.Use, Slot: 0}, {Tab: gamedata.Use, Slot: 3, Item: &potion}}}
)

func wantChanged(t *testing.T, got []*itemv1.Slot) {
	t.Helper()
	if len(got) != 2 || got[0].GetItem() != nil || got[0].GetTab() != "use" || got[0].GetSlot() != 0 {
		t.Fatalf("emptied slot %+v", got)
	}
	it := got[1].GetItem()
	if got[1].GetSlot() != 3 || it.GetId() != potion.ID.String() || it.GetTemplateId() != "red_potion" || it.GetTab() != "use" || it.GetSlot() != 3 || it.GetQuantity() != 5 {
		t.Fatalf("filled slot %+v", got[1])
	}
}

func TestEachCallMapsItsFieldsAndResult(t *testing.T) {
	ctx := context.Background()
	f := &fakeItems{inventory: []model.Item{potion}, result: changed}
	c := client(t, f)

	t.Run("Inventory", func(t *testing.T) {
		res, err := c.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: character.String()})
		if err != nil {
			t.Fatal(err)
		}
		if f.owner != character || len(res.GetItems()) != 1 || res.GetItems()[0].GetId() != potion.ID.String() || res.GetItems()[0].GetQuantity() != 5 {
			t.Fatalf("owner %v items %+v", f.owner, res.GetItems())
		}
	})
	t.Run("Add, a pickup keyed by its drop", func(t *testing.T) {
		drop := uuid.New()
		res, err := c.Add(ctx, &itemv1.AddRequest{Actor: actor, IdempotencyKey: drop.String(), TemplateId: "slime_gel", Quantity: 2,
			Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: drop.String()}})
		if err != nil {
			t.Fatal(err)
		}
		want := application.AddInput{Key: drop.String(), TemplateID: "slime_gel", Quantity: 2, Cause: model.Cause{Type: model.CauseDrop, ID: drop}}
		if f.add != want || f.actor != (model.Actor{Account: account, Character: character}) {
			t.Fatalf("add %+v actor %+v", f.add, f.actor)
		}
		wantChanged(t, res.GetChanged())
	})
	t.Run("Add without a cause ID", func(t *testing.T) {
		if _, err := c.Add(ctx, &itemv1.AddRequest{Actor: actor, IdempotencyKey: "k", TemplateId: "slime_gel", Quantity: 1,
			Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_PLAYER}}); err != nil {
			t.Fatal(err)
		}
		if f.add.Cause != (model.Cause{Type: model.CausePlayer}) {
			t.Fatalf("cause %+v", f.add.Cause)
		}
	})
	t.Run("Move", func(t *testing.T) {
		res, err := c.Move(ctx, &itemv1.MoveRequest{Actor: actor, IdempotencyKey: "k-1", ItemId: potion.ID.String(), Tab: "use", Slot: 3})
		if err != nil {
			t.Fatal(err)
		}
		if f.move != (application.MoveInput{Key: "k-1", ItemID: potion.ID, Tab: gamedata.Use, Slot: 3}) {
			t.Fatalf("move %+v", f.move)
		}
		wantChanged(t, res.GetChanged())
	})
	t.Run("Discard", func(t *testing.T) {
		res, err := c.Discard(ctx, &itemv1.DiscardRequest{Actor: actor, IdempotencyKey: "k-2", ItemId: potion.ID.String(), Quantity: 2})
		if err != nil {
			t.Fatal(err)
		}
		if f.discard != (application.DiscardInput{Key: "k-2", ItemID: potion.ID, Quantity: 2}) {
			t.Fatalf("discard %+v", f.discard)
		}
		wantChanged(t, res.GetChanged())
	})
	t.Run("GrantStarter", func(t *testing.T) {
		res, err := c.GrantStarter(ctx, &itemv1.GrantStarterRequest{Actor: actor, Class: "bowman"})
		if err != nil {
			t.Fatal(err)
		}
		if f.class != kernel.Bowman || f.actor.Character != character {
			t.Fatalf("class %v actor %+v", f.class, f.actor)
		}
		wantChanged(t, res.GetChanged())
	})
}

func TestMalformedRequestsNeverReachTheModule(t *testing.T) {
	ctx := context.Background()
	f := &fakeItems{}
	c := client(t, f)
	bad := &itemv1.Actor{AccountId: "nope", CharacterId: character.String()}
	tests := []struct {
		name string
		call func() error
		want itemv1.Reason
	}{
		{"an inventory owner", func() error {
			_, err := c.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: "x"})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"an actor's account", func() error {
			_, err := c.Add(ctx, &itemv1.AddRequest{Actor: bad, IdempotencyKey: "k", TemplateId: "slime_gel", Quantity: 1})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"an actor's character", func() error {
			_, err := c.Move(ctx, &itemv1.MoveRequest{Actor: &itemv1.Actor{AccountId: account.String(), CharacterId: ""}, ItemId: potion.ID.String()})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"an item", func() error {
			_, err := c.Discard(ctx, &itemv1.DiscardRequest{Actor: actor, ItemId: "bad", Quantity: 1})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"a cause", func() error {
			_, err := c.Add(ctx, &itemv1.AddRequest{Actor: actor, IdempotencyKey: "k", TemplateId: "slime_gel", Quantity: 1, Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: "nope"}})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"a class", func() error {
			_, err := c.GrantStarter(ctx, &itemv1.GrantStarterRequest{Actor: actor, Class: "paladin"})
			return err
		}, itemv1.Reason_REASON_UNKNOWN_CLASS},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(tt.want) || status.Code(err) != codes.InvalidArgument {
				t.Fatalf("got %v", err)
			}
		})
	}
	if f.calls != 0 {
		t.Fatalf("the module was called %d times", f.calls)
	}
}

func TestEveryModuleErrorArrivesWithItsReason(t *testing.T) {
	for _, row := range handler.Errors {
		t.Run(grpcx.Key(row.Reason).Name, func(t *testing.T) {
			c := client(t, &fakeItems{err: row.Err})
			_, err := c.Move(context.Background(), &itemv1.MoveRequest{Actor: actor, IdempotencyKey: "k", ItemId: potion.ID.String(), Tab: "use", Slot: 1})
			if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(row.Reason) || status.Code(err) != row.Code {
				t.Fatalf("got %v", err)
			}
		})
	}
}

// The guard: every error the module declares has a row, and every reason
// in the contract has one, so nothing the module returns reaches a caller
// as Internal and no reason is promised that the service never sends.
func TestTheTableCoversTheModuleAndTheContract(t *testing.T) {
	declared, err := sentinels.Declared(filepath.Join("..", "model"))
	if err != nil {
		t.Fatal(err)
	}
	if len(declared) == 0 {
		t.Fatal("no errors found; is the walk broken?")
	}
	mapped, err := sentinels.Referenced("errors.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range declared {
		if !mapped[name] {
			t.Errorf("%s has no row in Errors", name)
		}
	}
	used := map[grpcx.Reason]bool{}
	for _, row := range handler.Errors {
		used[grpcx.Key(row.Reason)] = true
	}
	for _, r := range grpcx.Reasons(itemv1.Reason(0).Descriptor()) {
		if !used[r] {
			t.Errorf("%s has no row in Errors", r)
		}
	}
}
