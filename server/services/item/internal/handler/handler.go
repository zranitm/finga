// Package handler is the item service's transport layer: the ItemService
// gRPC server (api/proto/internal/item/v1). Each method decodes the
// request into the module's input (a malformed ID never reaches the
// module), calls exactly one service method, and encodes the result.
// Errors leave through Errors, with their reason and never their text.
package handler

import (
	"context"

	"github.com/google/uuid"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/model"
	"finga/services/item/internal/item"
)

// Service is what the handler needs from the item module.
type Service interface {
	Inventory(ctx context.Context, owner model.CharacterID) ([]item.Item, error)
	Add(ctx context.Context, actor item.Actor, in item.AddInput) (item.Result, error)
	Move(ctx context.Context, actor item.Actor, in item.MoveInput) (item.Result, error)
	Discard(ctx context.Context, actor item.Actor, in item.DiscardInput) (item.Result, error)
	GrantStarter(ctx context.Context, actor item.Actor, class model.Class) (item.Result, error)
}

// Server serves ItemService over the module.
type Server struct {
	itemv1.UnimplementedItemServiceServer
	svc Service
}

// New builds the server.
func New(svc Service) *Server { return &Server{svc: svc} }

// Inventory lists a character's items.
func (s *Server) Inventory(ctx context.Context, req *itemv1.InventoryRequest) (*itemv1.InventoryResponse, error) {
	owner, err := model.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	items, err := s.svc.Inventory(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]*itemv1.Item, 0, len(items))
	for _, it := range items {
		out = append(out, itemOf(it))
	}
	return &itemv1.InventoryResponse{Items: out}, nil
}

// Add puts a quantity of a template in the inventory.
func (s *Server) Add(ctx context.Context, req *itemv1.AddRequest) (*itemv1.AddResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	cause, err := causeOf(req.GetCause())
	if err != nil {
		return nil, err
	}
	res, err := s.svc.Add(ctx, actor, item.AddInput{Key: req.GetIdempotencyKey(), TemplateID: req.GetTemplateId(), Quantity: int(req.GetQuantity()), Cause: cause})
	if err != nil {
		return nil, err
	}
	return &itemv1.AddResponse{Changed: slots(res)}, nil
}

// Move moves an item within its tab.
func (s *Server) Move(ctx context.Context, req *itemv1.MoveRequest) (*itemv1.MoveResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	id, err := model.ParseItemID(req.GetItemId())
	if err != nil {
		return nil, err
	}
	res, err := s.svc.Move(ctx, actor, item.MoveInput{Key: req.GetIdempotencyKey(), ItemID: id, Tab: gamedata.Tab(req.GetTab()), Slot: int(req.GetSlot())})
	if err != nil {
		return nil, err
	}
	return &itemv1.MoveResponse{Changed: slots(res)}, nil
}

// Discard destroys an item or part of a stack.
func (s *Server) Discard(ctx context.Context, req *itemv1.DiscardRequest) (*itemv1.DiscardResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	id, err := model.ParseItemID(req.GetItemId())
	if err != nil {
		return nil, err
	}
	res, err := s.svc.Discard(ctx, actor, item.DiscardInput{Key: req.GetIdempotencyKey(), ItemID: id, Quantity: int(req.GetQuantity())})
	if err != nil {
		return nil, err
	}
	return &itemv1.DiscardResponse{Changed: slots(res)}, nil
}

// GrantStarter gives a new character its starter items, once.
func (s *Server) GrantStarter(ctx context.Context, req *itemv1.GrantStarterRequest) (*itemv1.GrantStarterResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	class, err := model.ParseClass(req.GetClass())
	if err != nil {
		return nil, err
	}
	res, err := s.svc.GrantStarter(ctx, actor, class)
	if err != nil {
		return nil, err
	}
	return &itemv1.GrantStarterResponse{Changed: slots(res)}, nil
}

func actorOf(a *itemv1.Actor) (item.Actor, error) {
	account, err := model.ParseAccountID(a.GetAccountId())
	if err != nil {
		return item.Actor{}, err
	}
	character, err := model.ParseCharacterID(a.GetCharacterId())
	if err != nil {
		return item.Actor{}, err
	}
	return item.Actor{Account: account, Character: character}, nil
}

var causeTypes = map[itemv1.CauseType]item.CauseType{
	itemv1.CauseType_CAUSE_TYPE_DROP:    item.CauseDrop,
	itemv1.CauseType_CAUSE_TYPE_PLAYER:  item.CausePlayer,
	itemv1.CauseType_CAUSE_TYPE_STARTER: item.CauseStarter,
}

func causeOf(c *itemv1.Cause) (item.Cause, error) {
	out := item.Cause{Type: causeTypes[c.GetType()]}
	if c.GetId() != "" {
		id, err := uuid.Parse(c.GetId())
		if err != nil {
			return item.Cause{}, model.ErrBadID
		}
		out.ID = id
	}
	return out, nil
}

func itemOf(it item.Item) *itemv1.Item {
	return &itemv1.Item{Id: it.ID.String(), TemplateId: it.TemplateID, Tab: string(it.Tab), Slot: int32(it.Slot), Quantity: int32(it.Quantity)}
}

func slots(res item.Result) []*itemv1.Slot {
	out := make([]*itemv1.Slot, 0, len(res.Changed))
	for _, s := range res.Changed {
		slot := &itemv1.Slot{Tab: string(s.Tab), Slot: int32(s.Slot)}
		if s.Item != nil {
			slot.Item = itemOf(*s.Item)
		}
		out = append(out, slot)
	}
	return out
}
