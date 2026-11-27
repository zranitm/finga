package item

import (
	"context"
	"fmt"
	"slices"
	"time"

	model "finga/internal/model/item"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/pkg/idempotency"
)

// WardrobeInput adds an item's piece to the account's wardrobe.
type WardrobeInput struct {
	Key    string
	ItemID kernel.ItemID
}

// LookInput wears five picks, each a piece the account has collected or "" for the class's own.
type LookInput struct {
	Key   string
	Look  model.Look
	Class kernel.Class // the character's live class, whose kind a weapon look must be
}

func (in WardrobeInput) fingerprint(c kernel.CharacterID) []byte {
	return idempotency.Fingerprint("item.wardrobe/1", c.String(), in.ItemID.String())
}

// The class stays out, as an equip's does: a retry after a promotion is the same request.
func (in LookInput) fingerprint(c kernel.CharacterID) []byte {
	hide := "shown"
	if in.Look.HideHeadwear {
		hide = "hidden"
	}
	return idempotency.Fingerprint("item.look/1", c.String(), in.Look.Outfit, in.Look.Headwear, in.Look.Face, in.Look.Weapon, in.Look.Wings, hide)
}

// Pieces lists the account's wardrobe, oldest first.
func (s *Service) Pieces(ctx context.Context, account kernel.AccountID) ([]model.Piece, error) {
	return s.store.Pieces(ctx, account)
}

// Look is the character's look, the zero Look when it never set one.
func (s *Service) Look(ctx context.Context, character kernel.CharacterID) (model.Look, error) {
	return s.store.Look(ctx, character)
}

// AddToWardrobe takes one of an item that holds a piece and puts the piece in the account's wardrobe, in one transaction; a piece the wardrobe has changes nothing.
func (s *Service) AddToWardrobe(ctx context.Context, actor model.Actor, in WardrobeInput) (Changes, error) {
	var res Changes
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeWardrobe, in.Key, in.fingerprint(actor.Character), func() (Changes, error) {
			res, err := s.addToWardrobe(ctx, uow, actor, in.ItemID)
			return revised(ctx, uow, actor.Character, res, err)
		})
		return err
	})
	return res, err
}

func (s *Service) addToWardrobe(ctx context.Context, uow UnitOfWork, actor model.Actor, id kernel.ItemID) (Changes, error) {
	owner := actor.Character
	it, err := uow.Items().LockOwned(ctx, id, owner)
	if err != nil {
		return Changes{}, err // someone else's item is ErrNotFound too
	}
	piece, ok := s.content.PieceIn(it.TemplateID)
	if !ok {
		return Changes{}, model.ErrNotAPiece
	}
	added, err := uow.Wardrobe().Add(ctx, actor.Account, piece.ID, it.ID)
	if err != nil {
		return Changes{}, err
	}
	if !added {
		return Changes{}, model.ErrPieceCollected
	}
	now := time.Now()
	cause := model.Cause{Type: model.CausePlayer}
	if err := uow.Events().Append(ctx, model.Event{ItemID: it.ID, Kind: model.EventCollected, QuantityDelta: -1, FromOwner: &owner, Cause: cause, OccurredAt: now}); err != nil {
		return Changes{}, err
	}
	if err := uow.Notices().Send(ctx, model.Notice{Kind: model.NoticeCollected, Item: it, Quantity: -1, Cause: cause, OccurredAt: now}); err != nil {
		return Changes{}, err
	}
	it.Quantity--
	if it.Quantity == 0 {
		if err := uow.Items().Destroy(ctx, it.ID); err != nil {
			return Changes{}, err
		}
		return Changes{Changed: []model.Slot{{Tab: it.Tab, Slot: it.Slot}}, Piece: piece.ID}, nil
	}
	if err := uow.Items().SetQuantity(ctx, it.ID, it.Quantity); err != nil {
		return Changes{}, err
	}
	return Changes{Changed: []model.Slot{slotOf(it)}, Piece: piece.ID}, nil
}

// GrantPieces puts each grant's piece in the account's wardrobe, in one transaction, giving back the grants whose piece was new; a piece the wardrobe has stays as it was. Each grant must name a source its piece has.
func (s *Service) GrantPieces(ctx context.Context, account kernel.AccountID, grants []model.PieceGrant) ([]model.PieceGrant, error) {
	for _, g := range grants {
		if !s.grantable(g) {
			return nil, fmt.Errorf("item: piece %q has no %s source %q", g.Piece, g.Cause, g.Ref)
		}
	}
	var added []model.PieceGrant
	err := s.store.InTx(ctx, func(uow UnitOfWork) error {
		added = nil // a retried transaction starts again
		for _, g := range grants {
			ok, err := uow.Wardrobe().Grant(ctx, account, g)
			if err != nil {
				return err
			}
			if ok {
				added = append(added, g)
			}
		}
		return nil
	})
	return added, err
}

// grantable is whether g names a source its piece has: the rank, the quest or the feat that gives it.
func (s *Service) grantable(g model.PieceGrant) bool {
	p, ok := s.content.Piece(g.Piece)
	return ok && g.Ref != "" && slices.ContainsFunc(p.From, func(src gamedata.PieceSource) bool {
		switch g.Cause {
		case model.PieceFromRank:
			return src.Rank == g.Ref
		case model.PieceFromQuest:
			return src.Quest == g.Ref
		case model.PieceFromFeat:
			return src.Feat == g.Ref
		}
		return false
	})
}

// SetLook wears in's picks: each a piece of its slot the account has collected, a weapon look of the class's kind, or "" for the class's own.
func (s *Service) SetLook(ctx context.Context, actor model.Actor, in LookInput) (Changes, error) {
	picks, err := s.picks(in.Look, in.Class)
	if err != nil {
		return Changes{}, err
	}
	var res Changes
	err = s.store.InTx(ctx, func(uow UnitOfWork) error {
		var err error
		res, err = uow.Idempotency().Run(ctx, actor.Account, scopeLook, in.Key, in.fingerprint(actor.Character), func() (Changes, error) {
			have, err := uow.Wardrobe().Collected(ctx, actor.Account, picks)
			if err != nil {
				return Changes{}, err
			}
			if len(have) != len(picks) {
				return Changes{}, model.ErrNotCollected
			}
			look, err := uow.Wardrobe().SetLook(ctx, actor.Character, in.Look)
			return Changes{Look: &look}, err
		})
		return err
	})
	return res, err
}

// picks are look's pieces, each checked against the catalogue: known, of its slot, and a weapon look of class's kind.
func (s *Service) picks(look model.Look, class kernel.Class) ([]string, error) {
	c, ok := s.content.Class(class)
	if !ok {
		return nil, model.ErrWrongClass
	}
	var out []string
	for _, pick := range []struct {
		slot gamedata.LookSlot
		id   string
	}{{gamedata.OutfitSlot, look.Outfit}, {gamedata.HeadwearSlot, look.Headwear}, {gamedata.FaceSlot, look.Face}, {gamedata.WeaponSlot, look.Weapon}, {gamedata.WingsSlot, look.Wings}} {
		if pick.id == "" {
			continue
		}
		p, ok := s.content.Piece(pick.id)
		switch {
		case !ok:
			return nil, model.ErrUnknownPiece
		case p.Slot != pick.slot:
			return nil, model.ErrWrongLookSlot
		case p.Slot == gamedata.WeaponSlot && p.Kind != c.Weapon:
			return nil, model.ErrWrongKind
		}
		out = append(out, p.ID)
	}
	return out, nil
}

// fitLook clears a weapon look the class promoted to can't hold, in the gift's transaction, and returns the look as it stands.
func (s *Service) fitLook(ctx context.Context, uow UnitOfWork, owner kernel.CharacterID, promotes kernel.Class) (*model.Look, error) {
	c, ok := s.content.Class(promotes)
	if !ok {
		return nil, model.ErrWrongClass
	}
	look, err := uow.Wardrobe().LockLook(ctx, owner)
	if err != nil {
		return nil, err
	}
	if p, _ := s.content.Piece(look.Weapon); look.Weapon == "" || p.Kind == c.Weapon {
		return &look, nil
	}
	look.Weapon = ""
	look, err = uow.Wardrobe().SetLook(ctx, owner, look)
	return &look, err
}
