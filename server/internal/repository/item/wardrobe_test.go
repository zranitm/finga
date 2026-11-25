package item

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	application "finga/internal/application/item/service"
	model "finga/internal/model/item"
	"finga/internal/repository/item/itemtest"
	"finga/internal/shared/kernel"
	"finga/pkg/uuidv7"
)

// collect has the harness's actor add an item's piece to its wardrobe under key.
func (h *harness) collect(key string, id kernel.ItemID) (application.Result, error) {
	return h.svc.AddToWardrobe(context.Background(), h.actor, application.WardrobeInput{Key: key, ItemID: id})
}

// Adding takes one of the item and makes the wardrobe entry in one transaction, idempotent by key; the piece is the account's, whichever character adds it next.
func TestAddingAPieceTakesOneItemAndMakesItsEntry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t)
	h.add(t, "a1", "piece.knight_plate", 2)
	plate := h.inventory(t)[0]
	t.Run("one from the stack, the entry naming the item, its event and notice", func(t *testing.T) {
		before := h.revision(t)
		res, err := h.collect("w1", plate.ID)
		if err != nil || res.Piece != "knight_plate" || len(res.Changed) != 1 || res.Changed[0].Item.Quantity != 1 || res.Revision != before+1 {
			t.Fatalf("result %+v, err %v", res, err)
		}
		pieces, err := h.store.Pieces(ctx, h.actor.Account)
		if err != nil || len(pieces) != 1 || pieces[0].ID != "knight_plate" || pieces[0].Cause != model.PieceFromItem || pieces[0].Item == nil || *pieces[0].Item != plate.ID {
			t.Fatalf("wardrobe %+v, err %v", pieces, err)
		}
		if h.count(t, `SELECT count(*) FROM item_events WHERE kind = 'collected' AND quantity_delta = -1 AND item_id = $1`, plate.ID) != 1 || h.count(t, `SELECT count(*) FROM outbox WHERE type = 'ItemCollected'`) != 1 {
			t.Fatal("no collected event or ItemCollected notice")
		}
	})
	t.Run("the same key again replays and takes nothing more", func(t *testing.T) {
		res, err := h.collect("w1", plate.ID)
		if err != nil || res.Piece != "knight_plate" || h.inventory(t)[0].Quantity != 1 || h.count(t, `SELECT count(*) FROM item_events WHERE kind = 'collected'`) != 1 {
			t.Fatalf("a replay: %+v, %v", res, err)
		}
	})
	t.Run("a piece the wardrobe has is refused, and nothing changes", func(t *testing.T) {
		before := h.revision(t)
		if _, err := h.collect("w2", plate.ID); !errors.Is(err, model.ErrPieceCollected) {
			t.Fatalf("a second add: %v, want ErrPieceCollected", err)
		}
		if h.inventory(t)[0].Quantity != 1 || h.revision(t) != before || h.count(t, `SELECT count(*) FROM item_events WHERE kind = 'collected'`) != 1 {
			t.Fatal("a refused add changed the inventory")
		}
	})
	t.Run("another character of the account finds it collected", func(t *testing.T) {
		alt := &harness{pool: h.pool, store: h.store, svc: h.svc, data: h.data, actor: model.Actor{Account: h.actor.Account, Character: kernel.NewCharacterID()}}
		alt.add(t, "a2", "piece.knight_plate", 1)
		if _, err := alt.collect("w3", alt.inventory(t)[0].ID); !errors.Is(err, model.ErrPieceCollected) {
			t.Fatalf("the account's other character: %v, want ErrPieceCollected", err)
		}
	})
	t.Run("the item's last goes with it", func(t *testing.T) {
		h.add(t, "a3", "piece.greatsword", 1)
		var sword model.Item
		for _, it := range h.inventory(t) {
			if it.TemplateID == "piece.greatsword" {
				sword = it
			}
		}
		res, err := h.collect("w4", sword.ID)
		if err != nil || len(res.Changed) != 1 || res.Changed[0].Item != nil || h.count(t, `SELECT count(*) FROM items WHERE id = $1 AND location = 'destroyed'`, sword.ID) != 1 {
			t.Fatalf("adding the last: %+v, %v", res, err)
		}
	})
	t.Run("an item that holds no piece, or another's", func(t *testing.T) {
		h.add(t, "a4", "slime_gel", 1)
		var gel model.Item
		for _, it := range h.inventory(t) {
			if it.TemplateID == "slime_gel" {
				gel = it
			}
		}
		if _, err := h.collect("w5", gel.ID); !errors.Is(err, model.ErrNotAPiece) {
			t.Fatalf("slime gel: %v, want ErrNotAPiece", err)
		}
		other := &harness{pool: h.pool, store: h.store, svc: h.svc, data: h.data, actor: model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}}
		if _, err := other.collect("w6", plate.ID); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("another's item: %v, want ErrNotFound", err)
		}
	})
	checkInvariants(t, h.pool, h.data)
}

// A grant makes its piece's entry for the rank, with no item, event or notice, and the piece is the account's; a second grant of it changes nothing, and a call with a grant its piece has no source for grants none.
func TestAGrantMakesItsEntryOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t)
	silver := model.PieceGrant{Piece: "helm_silver", Cause: model.PieceFromRank, Ref: "silver"}
	gold := model.PieceGrant{Piece: "helm_gold", Cause: model.PieceFromRank, Ref: "gold"}
	platinum := model.PieceGrant{Piece: "platinum_plate", Cause: model.PieceFromRank, Ref: "platinum"}
	t.Run("the entry and its cause, and nothing else", func(t *testing.T) {
		added, err := h.svc.GrantPieces(ctx, h.actor.Account, []model.PieceGrant{silver})
		if err != nil || !slices.Equal(added, []model.PieceGrant{silver}) {
			t.Fatalf("added %+v, err %v", added, err)
		}
		pieces, err := h.store.Pieces(ctx, h.actor.Account)
		if err != nil || len(pieces) != 1 || pieces[0].ID != "helm_silver" || pieces[0].Cause != model.PieceFromRank || pieces[0].Item != nil {
			t.Fatalf("wardrobe %+v, err %v", pieces, err)
		}
		if h.count(t, `SELECT count(*) FROM account_pieces WHERE cause_ref = 'silver' AND item_id IS NULL`) != 1 || h.count(t, `SELECT count(*) FROM item_events`) != 0 || h.count(t, `SELECT count(*) FROM outbox`) != 0 {
			t.Fatal("a grant's entry names no rank, or it wrote an event or a notice")
		}
	})
	t.Run("again, beside another: only the other is new", func(t *testing.T) {
		added, err := h.svc.GrantPieces(ctx, h.actor.Account, []model.PieceGrant{silver, gold})
		if err != nil || !slices.Equal(added, []model.PieceGrant{gold}) || h.count(t, `SELECT count(*) FROM account_pieces`) != 2 {
			t.Fatalf("added %+v, err %v", added, err)
		}
	})
	t.Run("a grant its piece has no source for refuses the whole call", func(t *testing.T) {
		for _, bad := range []model.PieceGrant{
			{Piece: "helm_gold", Cause: model.PieceFromRank, Ref: "silver"},
			{Piece: "knight_plate", Cause: model.PieceFromRank, Ref: "gold"},
			{Piece: "platinum_plate", Cause: model.PieceFromItem, Ref: "platinum"},
			{Piece: "platinum_plate", Cause: model.PieceFromRank},
			{Piece: "no_such_piece", Cause: model.PieceFromRank, Ref: "gold"},
		} {
			if added, err := h.svc.GrantPieces(ctx, h.actor.Account, []model.PieceGrant{platinum, bad}); err == nil {
				t.Errorf("granting %+v: %+v", bad, added)
			}
		}
		if h.count(t, `SELECT count(*) FROM account_pieces`) != 2 {
			t.Fatal("a refused call granted its good grant")
		}
	})
	t.Run("another character of the account wears it", func(t *testing.T) {
		alt := model.Actor{Account: h.actor.Account, Character: kernel.NewCharacterID()}
		res, err := h.svc.SetLook(ctx, alt, application.LookInput{Key: "l1", Look: model.Look{Headwear: "helm_gold"}, Class: "warrior"})
		if err != nil || res.Look == nil || res.Look.Headwear != "helm_gold" {
			t.Fatalf("wearing the gold visor: %+v, %v", res, err)
		}
	})
	t.Run("a feat's piece, for the feat that gives it alone", func(t *testing.T) {
		boss := model.PieceGrant{Piece: "crimson_helm", Cause: model.PieceFromFeat, Ref: "first_boss"}
		if added, err := h.svc.GrantPieces(ctx, h.actor.Account, []model.PieceGrant{boss}); err != nil || !slices.Equal(added, []model.PieceGrant{boss}) {
			t.Fatalf("added %+v, err %v", added, err)
		}
		if h.count(t, `SELECT count(*) FROM account_pieces WHERE piece_id = 'crimson_helm' AND cause_type = 'feat' AND cause_ref = 'first_boss' AND item_id IS NULL`) != 1 {
			t.Fatal("no entry for the feat")
		}
		for _, bad := range []model.PieceGrant{
			{Piece: "veteran_plate", Cause: model.PieceFromFeat, Ref: "first_boss"},
			{Piece: "veteran_plate", Cause: model.PieceFromRank, Ref: "promoted"},
		} {
			if added, err := h.svc.GrantPieces(ctx, h.actor.Account, []model.PieceGrant{bad}); err == nil {
				t.Errorf("granting %+v: %+v", bad, added)
			}
		}
	})
	checkInvariants(t, h.pool, h.data)
}

// failing is a store whose transactions fail at the inventory's revision, an add's last write, so a test sees the writes before it undone.
type failing struct{ application.Store }

func (f failing) InTx(ctx context.Context, fn func(uow application.UnitOfWork) error) error {
	return f.Store.InTx(ctx, func(uow application.UnitOfWork) error { return fn(failingWork{uow}) })
}

type failingWork struct{ application.UnitOfWork }

func (w failingWork) Items() application.Repository { return failingItems{w.UnitOfWork.Items()} }

type failingItems struct{ application.Repository }

var errCommit = errors.New("the commit failed")

func (failingItems) NextRevision(context.Context, kernel.CharacterID) (uint64, error) {
	return 0, errCommit
}

// A failed transaction leaves the item and no entry.
func TestAFailedAddLeavesTheItemAndNoEntry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t)
	h.add(t, "a1", "piece.knight_plate", 1)
	plate := h.inventory(t)[0]
	svc := application.NewService(application.Deps{Store: failing{h.store}, Content: h.data, Logger: slog.New(slog.DiscardHandler)})
	if _, err := svc.AddToWardrobe(ctx, h.actor, application.WardrobeInput{Key: "w1", ItemID: plate.ID}); !errors.Is(err, errCommit) {
		t.Fatalf("the add: %v, want the failure", err)
	}
	if inv := h.inventory(t); len(inv) != 1 || inv[0].Quantity != 1 || h.count(t, `SELECT count(*) FROM account_pieces`) != 0 {
		t.Fatalf("after a failed add: %+v", inv)
	}
	checkInvariants(t, h.pool, h.data)
}

// A look wears pieces the account has collected, each of its slot, a weapon look of the class's kind; each change is one revision on.
func TestALookWearsCollectedPiecesOfTheirSlotAndKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t)
	for _, tmpl := range []string{"piece.knight_plate", "piece.greatsword", "piece.oak_wand", "piece.knight_helm"} {
		it := h.added(t, tmpl)
		if _, err := h.collect(uuidv7.NewString(), it.ID); err != nil {
			t.Fatal(err)
		}
	}
	set := func(key string, look model.Look, class kernel.Class) (application.Result, error) {
		return h.svc.SetLook(ctx, h.actor, application.LookInput{Key: key, Look: look, Class: class})
	}
	res, err := set("l1", model.Look{Outfit: "knight_plate", Weapon: "greatsword", Headwear: "knight_helm", HideHeadwear: true}, "warrior")
	want := model.Look{Outfit: "knight_plate", Weapon: "greatsword", Headwear: "knight_helm", HideHeadwear: true, Revision: 1}
	if err != nil || res.Look == nil || *res.Look != want {
		t.Fatalf("the look: %+v, %v", res.Look, err)
	}
	if again, err := set("l1", model.Look{Outfit: "knight_plate", Weapon: "greatsword", Headwear: "knight_helm", HideHeadwear: true}, "warrior"); err != nil || *again.Look != want {
		t.Fatalf("its key again: %+v, %v, want the same revision", again.Look, err)
	}
	for _, c := range []struct {
		name  string
		look  model.Look
		class kernel.Class
		want  error
	}{
		{"a pick that's no piece", model.Look{Outfit: "golden_robe"}, "warrior", model.ErrUnknownPiece},
		{"a piece in another slot's place", model.Look{Headwear: "knight_plate"}, "warrior", model.ErrWrongLookSlot},
		{"an outfit for wings", model.Look{Wings: "knight_plate"}, "warrior", model.ErrWrongLookSlot},
		{"a piece not collected", model.Look{Outfit: "mage_robe"}, "warrior", model.ErrNotCollected},
		{"a weapon look of another kind", model.Look{Weapon: "oak_wand"}, "warrior", model.ErrWrongKind},
	} {
		if _, err := set(uuidv7.NewString(), c.look, c.class); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	if got, err := h.store.Look(ctx, h.actor.Character); err != nil || got != want {
		t.Fatalf("after refusals the look is %+v, %v; want %+v", got, err, want)
	}
	if res, err := set("l2", model.Look{Weapon: "oak_wand"}, "magician"); err != nil || *res.Look != (model.Look{Weapon: "oak_wand", Revision: 2}) {
		t.Fatalf("back to the class's own but a wand: %+v, %v", res.Look, err)
	}
}

// The store keeps all five picks, wings among them, and the hidden headwear, each change one revision on.
func TestTheStoreKeepsEveryPick(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t)
	look := model.Look{Outfit: "a", Headwear: "b", Face: "c", Weapon: "d", Wings: "e", HideHeadwear: true}
	for want := uint64(1); want <= 2; want++ {
		var got model.Look
		if err := h.store.InTx(ctx, func(uow application.UnitOfWork) error {
			var err error
			got, err = uow.Wardrobe().SetLook(ctx, h.actor.Character, look)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		look.Revision = want
		if read, err := h.store.Look(ctx, h.actor.Character); err != nil || got != look || read != look {
			t.Fatalf("set %+v, read %+v, %v; want %+v", got, read, err, look)
		}
	}
}

// A trainer's gift clears a weapon look its class can't hold in the gift's own transaction, and keeps one it can.
func TestATrainersGiftClearsAWeaponLookTheNewClassCantHold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gift := func(h *harness, svc *application.Service, key string, to kernel.Class) (application.Result, error) {
		return svc.Add(ctx, h.actor, application.AddInput{Key: key, TemplateID: "wooden_sword", Quantity: 1, Cause: model.Cause{Type: model.CausePromotion, ID: uuidv7.New()}, Stats: h.roll("wooden_sword"), Promotes: to})
	}
	wearing := func(t *testing.T) *harness {
		h := newHarness(t)
		sword := h.added(t, "piece.greatsword")
		if _, err := h.collect("w1", sword.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.SetLook(ctx, h.actor, application.LookInput{Key: "l1", Look: model.Look{Outfit: "", Weapon: "greatsword"}, Class: "beginner"}); err != nil {
			t.Fatal(err)
		}
		return h
	}
	t.Run("a kind that refuses it", func(t *testing.T) {
		h := wearing(t)
		res, err := gift(h, h.svc, "g1", "magician")
		if err != nil || res.Look == nil || *res.Look != (model.Look{Revision: 2}) {
			t.Fatalf("the gift's look %+v, %v; want the weapon cleared", res.Look, err)
		}
		if got, _ := h.store.Look(ctx, h.actor.Character); got != (model.Look{Revision: 2}) {
			t.Fatalf("stored %+v", got)
		}
	})
	t.Run("a kind that holds it", func(t *testing.T) {
		h := wearing(t)
		res, err := gift(h, h.svc, "g1", "warrior")
		if err != nil || res.Look == nil || *res.Look != (model.Look{Weapon: "greatsword", Revision: 1}) {
			t.Fatalf("the gift's look %+v, %v; want it kept", res.Look, err)
		}
	})
	t.Run("a gift that fails keeps the look", func(t *testing.T) {
		h := wearing(t)
		svc := application.NewService(application.Deps{Store: failing{h.store}, Content: h.data, Logger: slog.New(slog.DiscardHandler)})
		if _, err := gift(h, svc, "g1", "magician"); !errors.Is(err, errCommit) {
			t.Fatalf("the gift: %v, want the failure", err)
		}
		if got, _ := h.store.Look(ctx, h.actor.Character); got != (model.Look{Weapon: "greatsword", Revision: 1}) {
			t.Fatalf("after a failed gift the look is %+v, want it untouched", got)
		}
	})
}

// Check 13 sees a wardrobe out of step with the collected events, either way.
func TestCheckThirteenCatchesAWardrobeOutOfStep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, c := range []struct{ name, corrupt string }{
		{"an entry lost", `DELETE FROM account_pieces`},
		{"an entry naming another piece than its item holds", `UPDATE account_pieces SET piece_id = 'mage_robe'`},
		{"an entry naming an item with no collected event", `INSERT INTO account_pieces (account_id, piece_id, cause_type, item_id, created_at, updated_at) SELECT account_id, 'mage_robe', 'item', gen_random_uuid(), now(), now() FROM account_pieces`},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			plate := h.added(t, "piece.knight_plate")
			if _, err := h.collect("w1", plate.ID); err != nil {
				t.Fatal(err)
			}
			checkInvariants(t, h.pool, h.data)
			if _, err := h.pool.Exec(ctx, c.corrupt); err != nil {
				t.Fatal(err)
			}
			got, err := itemtest.CheckInvariants(ctx, h.pool, h.data)
			if err != nil || len(got) == 0 || slices.ContainsFunc(got, func(m string) bool { return !strings.HasPrefix(m, "13 ") }) {
				t.Fatalf("found %v, %v; want check 13's mismatches alone", got, err)
			}
		})
	}
}
