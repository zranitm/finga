// Package handler is the item service's ItemService gRPC server.
package handler

import (
	"context"
	"strings"

	"github.com/google/uuid"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/uuidv7"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
)

// Service is what the handler needs from the item module.
type Service interface {
	Inventory(ctx context.Context, owner kernel.CharacterID) (model.Inventory, error)
	Add(ctx context.Context, actor model.Actor, in application.AddInput) (application.Result, error)
	Move(ctx context.Context, actor model.Actor, in application.MoveInput) (application.Result, error)
	Discard(ctx context.Context, actor model.Actor, in application.DiscardInput) (application.Result, error)
	GrantStarter(ctx context.Context, actor model.Actor, class kernel.Class) (application.Result, error)
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
	owner, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	inv, err := s.svc.Inventory(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]*itemv1.Item, 0, len(inv.Items))
	for _, it := range inv.Items {
		out = append(out, itemOf(it))
	}
	return &itemv1.InventoryResponse{Items: out, Revision: inv.Revision}, nil
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
	key, err := keyOf(req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	res, err := s.svc.Add(ctx, actor, application.AddInput{Key: key, TemplateID: req.GetTemplateId(), Quantity: int(req.GetQuantity()), Cause: cause})
	if err != nil {
		return nil, err
	}
	return &itemv1.AddResponse{Changed: slots(res), Revision: res.Revision}, nil
}

// Move moves an item within its tab.
func (s *Server) Move(ctx context.Context, req *itemv1.MoveRequest) (*itemv1.MoveResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	id, err := kernel.ParseItemID(req.GetItemId())
	if err != nil {
		return nil, err
	}
	key, err := keyOf(req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	res, err := s.svc.Move(ctx, actor, application.MoveInput{Key: key, ItemID: id, Tab: gamedata.Tab(req.GetTab()), Slot: int(req.GetSlot())})
	if err != nil {
		return nil, err
	}
	return &itemv1.MoveResponse{Changed: slots(res), Revision: res.Revision}, nil
}

// Discard destroys an item or part of a stack.
func (s *Server) Discard(ctx context.Context, req *itemv1.DiscardRequest) (*itemv1.DiscardResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	id, err := kernel.ParseItemID(req.GetItemId())
	if err != nil {
		return nil, err
	}
	key, err := keyOf(req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	res, err := s.svc.Discard(ctx, actor, application.DiscardInput{Key: key, ItemID: id, Quantity: int(req.GetQuantity())})
	if err != nil {
		return nil, err
	}
	return &itemv1.DiscardResponse{Changed: slots(res), Revision: res.Revision}, nil
}

// GrantStarter gives a new character its starter items, once.
func (s *Server) GrantStarter(ctx context.Context, req *itemv1.GrantStarterRequest) (*itemv1.GrantStarterResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	class, err := kernel.ParseClass(req.GetClass())
	if err != nil {
		return nil, err
	}
	res, err := s.svc.GrantStarter(ctx, actor, class)
	if err != nil {
		return nil, err
	}
	return &itemv1.GrantStarterResponse{Changed: slots(res), Revision: res.Revision}, nil
}

func actorOf(a *itemv1.Actor) (model.Actor, error) {
	account, err := kernel.ParseAccountID(a.GetAccountId())
	if err != nil {
		return model.Actor{}, err
	}
	character, err := kernel.ParseCharacterID(a.GetCharacterId())
	if err != nil {
		return model.Actor{}, err
	}
	return model.Actor{Account: account, Character: character}, nil
}

var causeTypes = map[itemv1.CauseType]model.CauseType{
	itemv1.CauseType_CAUSE_TYPE_DROP:    model.CauseDrop,
	itemv1.CauseType_CAUSE_TYPE_PLAYER:  model.CausePlayer,
	itemv1.CauseType_CAUSE_TYPE_STARTER: model.CauseStarter,
}

// keyOf accepts a canonical UUID, optionally k- prefixed, kept as sent so a retry across a deploy still finds it.
func keyOf(key string) (string, error) {
	id := strings.TrimPrefix(key, "k-")
	if len(id) != 36 {
		return "", model.ErrBadKey
	}
	if _, err := uuid.Parse(id); err != nil {
		return "", model.ErrBadKey
	}
	return key, nil
}

func causeOf(c *itemv1.Cause) (model.Cause, error) {
	t, ok := causeTypes[c.GetType()]
	if !ok {
		return model.Cause{}, model.ErrBadCause
	}
	out := model.Cause{Type: t}
	if c.GetId() != "" {
		id, err := uuidv7.Parse(c.GetId())
		if err != nil {
			return model.Cause{}, kernel.ErrBadID
		}
		out.ID = id
	}
	return out, nil
}

func itemOf(it model.Item) *itemv1.Item {
	return &itemv1.Item{Id: it.ID.String(), TemplateId: it.TemplateID, Tab: string(it.Tab), Slot: int32(it.Slot), Quantity: int32(it.Quantity)}
}

func slots(res application.Result) []*itemv1.Slot {
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
