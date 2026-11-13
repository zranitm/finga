// Package handler is the item service's ItemService gRPC server.
package handler

import (
	"context"
	"errors"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/uuidv7"
	"finga/services/item"
)

// Service is the item module's facade, as the handler calls it.
type Service interface {
	Inventory(ctx context.Context, owner kernel.CharacterID) (item.Inventory, error)
	Add(ctx context.Context, actor item.Actor, r item.AddRequest) (item.Result, error)
	AddMany(ctx context.Context, actor item.Actor, drops []item.AddRequest) (item.Batch, error)
	Move(ctx context.Context, actor item.Actor, r item.MoveRequest) (item.Result, error)
	Discard(ctx context.Context, actor item.Actor, r item.DiscardRequest) (item.Result, error)
	DiscardMany(ctx context.Context, actor item.Actor, r item.DiscardManyRequest) (item.Result, error)
	Use(ctx context.Context, actor item.Actor, r item.UseRequest) (item.Result, bool, error)
	GrantStarter(ctx context.Context, actor item.Actor) (item.Result, error)
	Equip(ctx context.Context, actor item.Actor, r item.EquipRequest) (item.Result, error)
	Unequip(ctx context.Context, actor item.Actor, r item.UnequipRequest) (item.Result, error)
	HandIn(ctx context.Context, actor item.Actor, r item.HandInRequest) (item.Result, error)
	HandedIn(ctx context.Context, owner kernel.CharacterID, asked []item.Completion) ([]item.HandedIn, error)
}

// Server serves ItemService over the module's facade.
type Server struct {
	itemv1.UnimplementedItemServiceServer
	svc Service
}

// New builds the server over the facade.
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
	if err := item.CheckKey(req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	res, err := s.svc.Add(ctx, actor, item.AddRequest{Key: req.GetIdempotencyKey(), TemplateID: req.GetTemplateId(), Quantity: int(req.GetQuantity()), Cause: cause, Stats: statsOf(req.GetStats())})
	if err != nil {
		return nil, err
	}
	return &itemv1.AddResponse{Changed: slots(res.Changed), Revision: res.Revision}, nil
}

// AddMany grants a batch of drops for one owner; a drop with a malformed key or cause is refused alone, as a rule refuses one.
func (s *Server) AddMany(ctx context.Context, req *itemv1.AddManyRequest) (*itemv1.AddManyResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	drops := req.GetDrops()
	if len(drops) == 0 || len(drops) > item.MaxBatch {
		return nil, kernel.ErrBadBatchSize
	}
	resp := &itemv1.AddManyResponse{Outcomes: make([]*itemv1.DropOutcome, len(drops))}
	var rs []item.AddRequest
	var at []int // each request's drop
	for i, d := range drops {
		r, err := addRequestOf(d)
		if err != nil {
			resp.Outcomes[i] = refused(err)
			continue
		}
		rs, at = append(rs, r), append(at, i)
	}
	if len(rs) == 0 {
		return resp, nil
	}
	b, err := s.svc.AddMany(ctx, actor, rs)
	if err != nil {
		return nil, err
	}
	for k, out := range b.Outcomes {
		switch {
		case out.Err != nil:
			resp.Outcomes[at[k]] = refused(out.Err)
		case out.Replayed:
			resp.Outcomes[at[k]] = &itemv1.DropOutcome{Grant: itemv1.Grant_GRANT_ALREADY_GRANTED, Changed: slots(out.Result.Changed), Revision: out.Result.Revision}
		default:
			resp.Outcomes[at[k]] = &itemv1.DropOutcome{Grant: itemv1.Grant_GRANT_GRANTED}
		}
	}
	resp.Changed, resp.Revision = slots(b.Changed), b.Revision
	return resp, nil
}

func addRequestOf(d *itemv1.AddManyDrop) (item.AddRequest, error) {
	if err := item.CheckKey(d.GetIdempotencyKey()); err != nil {
		return item.AddRequest{}, err
	}
	cause, err := causeOf(d.GetCause())
	if err != nil {
		return item.AddRequest{}, err
	}
	return item.AddRequest{Key: d.GetIdempotencyKey(), TemplateID: d.GetTemplateId(), Quantity: int(d.GetQuantity()), Cause: cause, Stats: statsOf(d.GetStats())}, nil
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
	if err := item.CheckKey(req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	res, err := s.svc.Move(ctx, actor, item.MoveRequest{Key: req.GetIdempotencyKey(), ItemID: id, Tab: gamedata.Tab(req.GetTab()), Slot: int(req.GetSlot())})
	if err != nil {
		return nil, err
	}
	return &itemv1.MoveResponse{Changed: slots(res.Changed), Revision: res.Revision}, nil
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
	if err := item.CheckKey(req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	res, err := s.svc.Discard(ctx, actor, item.DiscardRequest{Key: req.GetIdempotencyKey(), ItemID: id, Quantity: int(req.GetQuantity())})
	if err != nil {
		return nil, err
	}
	return &itemv1.DiscardResponse{Changed: slots(res.Changed), Revision: res.Revision}, nil
}

// DiscardMany destroys whole items or stacks, all of them or none.
func (s *Server) DiscardMany(ctx context.Context, req *itemv1.DiscardManyRequest) (*itemv1.DiscardManyResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	ids := make([]kernel.ItemID, 0, len(req.GetItemIds()))
	for _, raw := range req.GetItemIds() {
		id, err := kernel.ParseItemID(raw)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := item.CheckKey(req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	res, err := s.svc.DiscardMany(ctx, actor, item.DiscardManyRequest{Key: req.GetIdempotencyKey(), ItemIDs: ids})
	if err != nil {
		return nil, err
	}
	return &itemv1.DiscardManyResponse{Changed: slots(res.Changed), Revision: res.Revision}, nil
}

// Use takes one of a usable item from its stack.
func (s *Server) Use(ctx context.Context, req *itemv1.UseRequest) (*itemv1.UseResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	id, err := kernel.ParseItemID(req.GetItemId())
	if err != nil {
		return nil, err
	}
	if err := item.CheckKey(req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	res, replayed, err := s.svc.Use(ctx, actor, item.UseRequest{Key: req.GetIdempotencyKey(), ItemID: id, Template: req.GetTemplateId()})
	if err != nil {
		return nil, err
	}
	return &itemv1.UseResponse{Changed: slots(res.Changed), Revision: res.Revision, Replayed: replayed}, nil
}

// GrantStarter gives a new character its starter items, once.
func (s *Server) GrantStarter(ctx context.Context, req *itemv1.GrantStarterRequest) (*itemv1.GrantStarterResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	res, err := s.svc.GrantStarter(ctx, actor)
	if err != nil {
		return nil, err
	}
	return &itemv1.GrantStarterResponse{Changed: slots(res.Changed), Revision: res.Revision}, nil
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
	if err := item.CheckKey(req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	r := item.EquipRequest{Key: req.GetIdempotencyKey(), ItemID: id, EquipSlot: req.GetEquipSlot(), Level: int(req.GetLevel()), Class: kernel.Class(req.GetClass()), Stats: primaryOf(req.GetStats())}
	res, err := s.svc.Equip(ctx, actor, r)
	if err != nil {
		return nil, err
	}
	return &itemv1.EquipResponse{Changed: slots(res.Changed), Equipment: equipment(res.Equipment), Worn: items(res.Worn), Revision: res.Revision}, nil
}

// Unequip puts a worn item back in the Equip tab.
func (s *Server) Unequip(ctx context.Context, req *itemv1.UnequipRequest) (*itemv1.UnequipResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	if err := item.CheckKey(req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	r := item.UnequipRequest{Key: req.GetIdempotencyKey(), EquipSlot: req.GetEquipSlot()}
	if req.ToSlot != nil {
		to := int(req.GetToSlot())
		r.ToSlot = &to
	}
	res, err := s.svc.Unequip(ctx, actor, r)
	if err != nil {
		return nil, err
	}
	return &itemv1.UnequipResponse{Changed: slots(res.Changed), Equipment: equipment(res.Equipment), Worn: items(res.Worn), Revision: res.Revision}, nil
}

// HandIn takes the items a quest asks for and gives its rewards.
func (s *Server) HandIn(ctx context.Context, req *itemv1.HandInRequest) (*itemv1.HandInResponse, error) {
	actor, err := actorOf(req.GetActor())
	if err != nil {
		return nil, err
	}
	r := item.HandInRequest{Quest: req.GetQuestId(), Completion: int(req.GetCompletion())}
	for _, t := range req.GetTake() {
		r.Take = append(r.Take, gamedata.Grant{Item: t.GetTemplateId(), Quantity: int(t.GetQuantity())})
	}
	for _, g := range req.GetGive() {
		r.Give = append(r.Give, item.Give{TemplateID: g.GetTemplateId(), Quantity: int(g.GetQuantity()), Stats: statsOf(g.GetStats())})
	}
	res, err := s.svc.HandIn(ctx, actor, r)
	if err != nil {
		return nil, err
	}
	return &itemv1.HandInResponse{Changed: slots(res.Changed), Revision: res.Revision}, nil
}

// HandedIn is which of the completions asked about the character handed in.
func (s *Server) HandedIn(ctx context.Context, req *itemv1.HandedInRequest) (*itemv1.HandedInResponse, error) {
	owner, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	asked := make([]item.Completion, 0, len(req.GetCompletions()))
	for _, c := range req.GetCompletions() {
		asked = append(asked, item.Completion{Quest: c.GetQuestId(), Completion: int(c.GetCompletion())})
	}
	found, err := s.svc.HandedIn(ctx, owner, asked)
	if err != nil {
		return nil, err
	}
	out := &itemv1.HandedInResponse{HandedIn: make([]*itemv1.HandedInQuest, 0, len(found))}
	for _, h := range found {
		out.HandedIn = append(out.HandedIn, &itemv1.HandedInQuest{QuestId: h.Quest, Completion: int32(h.Completion), Changed: slots(h.Result.Changed), Revision: h.Result.Revision}) //nolint:gosec // completions are small
	}
	return out, nil
}

func actorOf(a *itemv1.Actor) (item.Actor, error) {
	account, err := kernel.ParseAccountID(a.GetAccountId())
	if err != nil {
		return item.Actor{}, err
	}
	character, err := kernel.ParseCharacterID(a.GetCharacterId())
	if err != nil {
		return item.Actor{}, err
	}
	return item.Actor{Account: account, Character: character}, nil
}

var causeTypes = map[itemv1.CauseType]item.CauseType{
	itemv1.CauseType_CAUSE_TYPE_DROP:      item.CauseDrop,
	itemv1.CauseType_CAUSE_TYPE_PLAYER:    item.CausePlayer,
	itemv1.CauseType_CAUSE_TYPE_STARTER:   item.CauseStarter,
	itemv1.CauseType_CAUSE_TYPE_PROMOTION: item.CausePromotion,
}

func causeOf(c *itemv1.Cause) (item.Cause, error) {
	t, ok := causeTypes[c.GetType()]
	if !ok {
		return item.Cause{}, kernel.ErrBadCause
	}
	out := item.Cause{Type: t}
	if c.GetId() != "" {
		id, err := uuidv7.Parse(c.GetId())
		if err != nil {
			return item.Cause{}, kernel.ErrBadID
		}
		out.ID = id
	}
	return out, nil
}

func itemOf(it item.Item) *itemv1.Item {
	out := &itemv1.Item{Id: it.ID.String(), TemplateId: it.TemplateID, Tab: string(it.Tab), Slot: int32(it.Slot), Quantity: int32(it.Quantity), EquipSlot: it.EquipSlot}
	if s := it.Stats; s != nil {
		//nolint:gosec // stats are small
		out.Stats = &itemv1.ItemStats{
			Str: int32(s.STR), Dex: int32(s.DEX), Int: int32(s.INT), Agi: int32(s.AGI), Vit: int32(s.VIT),
			PhysicalAttack: int32(s.PhysicalAttack), MagicAttack: int32(s.MagicAttack), PhysicalDefense: int32(s.PhysicalDefense), MagicDefense: int32(s.MagicDefense),
			MaxHp: int32(s.MaxHP), MaxMp: int32(s.MaxMP), Accuracy: int32(s.Accuracy), Evasion: int32(s.Evasion),
			CritRate: int32(s.CritRate), CritDamage: int32(s.CritDamage), Penetration: int32(s.Penetration), HpRegen: int32(s.HPRegen), MpRegen: int32(s.MPRegen),
			AttackSpeed: int32(s.AttackSpeed), CooldownSpeed: int32(s.CooldownSpeed), MoveSpeed: int32(s.MoveSpeed),
		}
	}
	return out
}

// primaryOf is a character's own primary stats as sent; none sent is all zero.
func primaryOf(s *itemv1.PrimaryStats) gamedata.Stats {
	return gamedata.Stats{STR: int(s.GetStr()), DEX: int(s.GetDex()), INT: int(s.GetInt()), AGI: int(s.GetAgi()), VIT: int(s.GetVit())}
}

// statsOf is a roll as sent, nil when none was.
func statsOf(s *itemv1.ItemStats) *progression.ItemStats {
	if s == nil {
		return nil
	}
	return &progression.ItemStats{
		STR: int(s.GetStr()), DEX: int(s.GetDex()), INT: int(s.GetInt()), AGI: int(s.GetAgi()), VIT: int(s.GetVit()),
		PhysicalAttack: int(s.GetPhysicalAttack()), MagicAttack: int(s.GetMagicAttack()), PhysicalDefense: int(s.GetPhysicalDefense()), MagicDefense: int(s.GetMagicDefense()),
		MaxHP: int(s.GetMaxHp()), MaxMP: int(s.GetMaxMp()), Accuracy: int(s.GetAccuracy()), Evasion: int(s.GetEvasion()),
		CritRate: int(s.GetCritRate()), CritDamage: int(s.GetCritDamage()), Penetration: int(s.GetPenetration()), HPRegen: int(s.GetHpRegen()), MPRegen: int(s.GetMpRegen()),
		AttackSpeed: int(s.GetAttackSpeed()), CooldownSpeed: int(s.GetCooldownSpeed()), MoveSpeed: int(s.GetMoveSpeed()),
	}
}

func items(its []item.Item) []*itemv1.Item {
	out := make([]*itemv1.Item, 0, len(its))
	for _, it := range its {
		out = append(out, itemOf(it))
	}
	return out
}

func equipment(worn []item.EquipSlot) []*itemv1.EquipSlot {
	out := make([]*itemv1.EquipSlot, 0, len(worn))
	for _, s := range worn {
		slot := &itemv1.EquipSlot{EquipSlot: s.Slot}
		if s.Item != nil {
			slot.Item = itemOf(*s.Item)
		}
		out = append(out, slot)
	}
	return out
}

func slots(changed []item.Slot) []*itemv1.Slot {
	out := make([]*itemv1.Slot, 0, len(changed))
	for _, s := range changed {
		slot := &itemv1.Slot{Tab: string(s.Tab), Slot: int32(s.Slot)}
		if s.Item != nil {
			slot.Item = itemOf(*s.Item)
		}
		out = append(out, slot)
	}
	return out
}
