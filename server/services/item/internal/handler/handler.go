// Package handler is the item service's ItemService gRPC server.
package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/uuidv7"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
)

// Service is what the handler needs from the item module.
type Service interface {
	Inventory(ctx context.Context, owner kernel.CharacterID) (model.Inventory, error)
	Add(ctx context.Context, actor model.Actor, in application.AddInput) (application.Result, error)
	AddMany(ctx context.Context, actor model.Actor, drops []application.AddInput) (application.Batch, error)
	Move(ctx context.Context, actor model.Actor, in application.MoveInput) (application.Result, error)
	Discard(ctx context.Context, actor model.Actor, in application.DiscardInput) (application.Result, error)
	GrantStarter(ctx context.Context, actor model.Actor, class kernel.Class) (application.Result, error)
	Equip(ctx context.Context, actor model.Actor, in application.EquipInput) (application.Result, error)
	Unequip(ctx context.Context, actor model.Actor, in application.UnequipInput) (application.Result, error)
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
	return &itemv1.InventoryResponse{Items: items(inv.Items), Revision: inv.Revision}, nil
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
	res, err := s.svc.Add(ctx, actor, application.AddInput{Key: key, TemplateID: req.GetTemplateId(), Quantity: int(req.GetQuantity()), Cause: cause, Stats: statsOf(req.GetStats())})
	if err != nil {
		return nil, err
	}
	return &itemv1.AddResponse{Changed: slots(res), Revision: res.Revision}, nil
}

// AddMany grants a batch of drops for one owner; a drop with a malformed key or cause is refused alone, as a rule refuses one.
func (s *Server) AddMany(ctx context.Context, req *itemv1.AddManyRequest) (*itemv1.AddManyResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	drops := req.GetDrops()
	if len(drops) == 0 || len(drops) > application.MaxBatch {
		return nil, model.ErrBadBatch
	}
	resp := &itemv1.AddManyResponse{Outcomes: make([]*itemv1.DropOutcome, len(drops))}
	var ins []application.AddInput
	var at []int // each input's drop
	for i, d := range drops {
		in, err := addInputOf(d)
		if err != nil {
			resp.Outcomes[i] = refused(err)
			continue
		}
		ins, at = append(ins, in), append(at, i)
	}
	if len(ins) == 0 {
		return resp, nil
	}
	b, err := s.svc.AddMany(ctx, actor, ins)
	if err != nil {
		return nil, err
	}
	for k, out := range b.Outcomes {
		switch {
		case out.Err != nil:
			resp.Outcomes[at[k]] = refused(out.Err)
		case out.Replayed:
			resp.Outcomes[at[k]] = &itemv1.DropOutcome{Grant: itemv1.Grant_GRANT_ALREADY_GRANTED, Changed: slots(out.Result), Revision: out.Result.Revision}
		default:
			resp.Outcomes[at[k]] = &itemv1.DropOutcome{Grant: itemv1.Grant_GRANT_GRANTED}
		}
	}
	resp.Changed, resp.Revision = slots(application.Result{Changed: b.Changed}), b.Revision
	return resp, nil
}

func addInputOf(d *itemv1.AddManyDrop) (application.AddInput, error) {
	key, err := keyOf(d.GetIdempotencyKey())
	if err != nil {
		return application.AddInput{}, err
	}
	cause, err := causeOf(d.GetCause())
	if err != nil {
		return application.AddInput{}, err
	}
	return application.AddInput{Key: key, TemplateID: d.GetTemplateId(), Quantity: int(d.GetQuantity()), Cause: cause, Stats: statsOf(d.GetStats())}, nil
}

// refused is a drop's refusal, with the reason Errors gives its rule.
func refused(err error) *itemv1.DropOutcome {
	out := &itemv1.DropOutcome{Grant: itemv1.Grant_GRANT_REFUSED}
	for _, row := range Errors {
		if r, ok := row.Reason.(itemv1.Reason); ok && errors.Is(err, row.Err) {
			out.Reason = r
			break
		}
	}
	return out
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

// Equip wears an inventory item, checked against the level and class the world sends.
func (s *Server) Equip(ctx context.Context, req *itemv1.EquipRequest) (*itemv1.EquipResponse, error) {
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
	class, err := kernel.ParseClass(req.GetClass())
	if err != nil {
		return nil, err
	}
	res, err := s.svc.Equip(ctx, actor, application.EquipInput{Key: key, ItemID: id, EquipSlot: req.GetEquipSlot(), Level: int(req.GetLevel()), Class: class})
	if err != nil {
		return nil, err
	}
	return &itemv1.EquipResponse{Changed: slots(res), Equipment: equipment(res), Worn: items(res.Worn), Revision: res.Revision}, nil
}

// Unequip puts a worn item back in the Equip tab.
func (s *Server) Unequip(ctx context.Context, req *itemv1.UnequipRequest) (*itemv1.UnequipResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	key, err := keyOf(req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	in := application.UnequipInput{Key: key, EquipSlot: req.GetEquipSlot()}
	if req.ToSlot != nil {
		to := int(req.GetToSlot())
		in.ToSlot = &to
	}
	res, err := s.svc.Unequip(ctx, actor, in)
	if err != nil {
		return nil, err
	}
	return &itemv1.UnequipResponse{Changed: slots(res), Equipment: equipment(res), Worn: items(res.Worn), Revision: res.Revision}, nil
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
	out := &itemv1.Item{Id: it.ID.String(), TemplateId: it.TemplateID, Tab: string(it.Tab), Slot: int32(it.Slot), Quantity: int32(it.Quantity), EquipSlot: it.EquipSlot}
	if s := it.Stats; s != nil {
		//nolint:gosec // stats are small
		out.Stats = &itemv1.ItemStats{
			Attack: int32(s.PhysicalAttack), Defense: int32(s.PhysicalDefense), Str: int32(s.STR), Dex: int32(s.DEX), Int: int32(s.INT), Agi: int32(s.AGI), Vit: int32(s.VIT),
			Accuracy: int32(s.Accuracy), Evasion: int32(s.Evasion), CritRate: int32(s.CritRate), CritDamage: int32(s.CritDamage), MaxHp: int32(s.MaxHP), MaxMp: int32(s.MaxMP),
		}
	}
	return out
}

// statsOf is a roll as sent, nil when none was.
func statsOf(s *itemv1.ItemStats) *progression.ItemStats {
	if s == nil {
		return nil
	}
	return &progression.ItemStats{
		PhysicalAttack: int(s.GetAttack()), PhysicalDefense: int(s.GetDefense()), STR: int(s.GetStr()), DEX: int(s.GetDex()), INT: int(s.GetInt()), AGI: int(s.GetAgi()), VIT: int(s.GetVit()),
		Accuracy: int(s.GetAccuracy()), Evasion: int(s.GetEvasion()), CritRate: int(s.GetCritRate()), CritDamage: int(s.GetCritDamage()), MaxHP: int(s.GetMaxHp()), MaxMP: int(s.GetMaxMp()),
	}
}

func items(its []model.Item) []*itemv1.Item {
	out := make([]*itemv1.Item, 0, len(its))
	for _, it := range its {
		out = append(out, itemOf(it))
	}
	return out
}

func equipment(res application.Result) []*itemv1.EquipSlot {
	out := make([]*itemv1.EquipSlot, 0, len(res.Equipment))
	for _, s := range res.Equipment {
		slot := &itemv1.EquipSlot{EquipSlot: s.Slot}
		if s.Item != nil {
			slot.Item = itemOf(*s.Item)
		}
		out = append(out, slot)
	}
	return out
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
