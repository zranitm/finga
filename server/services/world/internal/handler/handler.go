// Package handler is the world service's gRPC server: one bounded Deliver stream per gateway.
package handler

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/pkg/grpcx"
	"finga/pkg/hub"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
)

// World is what the handler needs from the world module; a command is ErrBusy when the character's map can't take it.
type World interface {
	Join(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink instance.Sink) (model.View, error)
	Leave(ctx context.Context, id kernel.CharacterID, conn string) error
	Input(id kernel.CharacterID, conn string, seq uint32, in sim.Input, view uint32) error
	Attack(id kernel.CharacterID, conn string, seq uint32) error
	EnterPortal(id kernel.CharacterID, conn string) error
	Say(id kernel.CharacterID, conn, name, text string) error
	Equip(ctx context.Context, id kernel.CharacterID, conn, key string, item kernel.ItemID, equipSlot string) error
	UseItem(ctx context.Context, id kernel.CharacterID, conn, key string, item kernel.ItemID, template string) error
	Unequip(ctx context.Context, id kernel.CharacterID, conn, key, equipSlot string, toSlot *int) error
	SpendPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int, points gamedata.Stats) error
	ResetPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int) error
	SpendSkillPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int, points map[string]int) error
	UseSkill(ctx context.Context, id kernel.CharacterID, conn, skill string, seq uint32) error
	Promote(ctx context.Context, id kernel.CharacterID, conn, npc string, class kernel.Class) error
	Talk(ctx context.Context, id kernel.CharacterID, conn, npc string) error
	AcceptQuest(ctx context.Context, id kernel.CharacterID, conn, npc, quest string) error
	AbandonQuest(ctx context.Context, id kernel.CharacterID, conn, quest string) error
	HandInQuest(ctx context.Context, id kernel.CharacterID, conn, npc, quest string) error
}

// queue bounds a gateway's stream: about two seconds of deltas for a few hundred players.
const queue = 8192

// ErrNotSubscribed is a Join from a gateway with no Deliver stream open.
var ErrNotSubscribed = errors.New("world: gateway has no deliver stream")

// Server serves WorldService.
type Server struct {
	worldv1.UnimplementedWorldServiceServer
	world    World
	bootID   string
	tickRate uint32
	deliver  *hub.Hub[*worldv1.DeliverResponse]
}

// Options configure the server.
type Options struct {
	// BootID is random per process start, so a gateway can tell its players are gone.
	BootID string
	// TickRateHz is the maps' ticks per second, told to each joining client.
	TickRateHz int
}

// New builds the server.
func New(w World, opts Options) *Server {
	return &Server{world: w, bootID: opts.BootID, tickRate: uint32(opts.TickRateHz), deliver: hub.New[*worldv1.DeliverResponse](queue)}
}

// Deliver streams one gateway's events, starting with Subscribed.
func (s *Server) Deliver(req *worldv1.DeliverRequest, stream grpc.ServerStreamingServer[worldv1.DeliverResponse]) error {
	if req.GetGatewayId() == "" {
		return status.Error(codes.InvalidArgument, "gateway_id is required")
	}
	first := &worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_Subscribed{Subscribed: &worldv1.Subscribed{BootId: s.bootID}}}
	return grpcx.ServeSubscription(stream, s.deliver.Subscribe(req.GetGatewayId()), func(m *worldv1.DeliverResponse) *worldv1.DeliverResponse { return m }, first)
}

// Join joins the character to its map; the Snapshot goes to the gateway's stream.
func (s *Server) Join(ctx context.Context, req *worldv1.JoinRequest) (*worldv1.JoinResponse, error) {
	sub, ok := s.deliver.Get(req.GetGatewayId())
	if !ok {
		return nil, ErrNotSubscribed
	}
	account, err := kernel.ParseAccountID(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	if req.GetConnectionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "connection_id is required")
	}
	v, err := s.world.Join(ctx, account, id, req.GetConnectionId(), &streamSink{sub: sub, character: id.String(), conn: req.GetConnectionId()})
	if err != nil {
		return nil, err
	}
	log := make([]*worldv1.QuestState, 0, len(v.Quests))
	for _, id := range slices.Sorted(maps.Keys(v.Quests)) {
		log = append(log, questState(id, v.Quests[id]))
	}
	return &worldv1.JoinResponse{Character: characterView(v), TickRateHz: s.tickRate, JoinSeq: v.JoinSeq, Quests: log, Timers: timersLeft(v.Timers, time.Now())}, nil
}

// timersLeft is the time left at now on each running cooldown, then each buff, by skill.
func timersLeft(t model.SkillTimers, now time.Time) []*worldv1.SkillTimer {
	out := make([]*worldv1.SkillTimer, 0, len(t.Cooldowns)+len(t.Buffs))
	for _, s := range slices.Sorted(maps.Keys(t.Cooldowns)) {
		if left := t.Cooldowns[s].Sub(now); left > 0 {
			out = append(out, &worldv1.SkillTimer{SkillId: s, Kind: worldv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN, LeftMs: left.Milliseconds()})
		}
	}
	for _, s := range slices.Sorted(maps.Keys(t.Buffs)) {
		if b := t.Buffs[s]; b.Until.After(now) {
			out = append(out, &worldv1.SkillTimer{SkillId: s, Kind: worldv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF, LeftMs: b.Until.Sub(now).Milliseconds(), Points: int32(b.Points)}) //nolint:gosec // points are small
		}
	}
	return out
}

// Leave removes the player if it joined through the presence's connection, once its map has.
func (s *Server) Leave(ctx context.Context, req *worldv1.LeaveRequest) (*worldv1.LeaveResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	if err := s.world.Leave(ctx, id, conn); err != nil {
		return nil, err
	}
	return &worldv1.LeaveResponse{}, nil
}

// Input queues held keys.
func (s *Server) Input(_ context.Context, req *worldv1.InputRequest) (*worldv1.InputResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	k := req.GetKeys()
	in := sim.Input{Left: k.GetLeft(), Right: k.GetRight(), Up: k.GetUp(), Down: k.GetDown(), Jump: k.GetJump()}
	if err := s.world.Input(id, conn, req.GetSeq(), in, req.GetView()); err != nil {
		return nil, err
	}
	return &worldv1.InputResponse{}, nil
}

// Attack queues a basic attack.
func (s *Server) Attack(_ context.Context, req *worldv1.AttackRequest) (*worldv1.AttackResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	if err := s.world.Attack(id, conn, req.GetSeq()); err != nil {
		return nil, err
	}
	return &worldv1.AttackResponse{}, nil
}

// PickUp does nothing: drops fly to their owners by themselves. It answers a gateway that still routes the key.
func (*Server) PickUp(context.Context, *worldv1.PickUpRequest) (*worldv1.PickUpResponse, error) {
	return &worldv1.PickUpResponse{}, nil
}

// EnterPortal takes the portal the character stands in.
func (s *Server) EnterPortal(_ context.Context, req *worldv1.EnterPortalRequest) (*worldv1.EnterPortalResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	if err := s.world.EnterPortal(id, conn); err != nil {
		return nil, err
	}
	return &worldv1.EnterPortalResponse{}, nil
}

// Say is a map chat line for everyone on the map.
func (s *Server) Say(_ context.Context, req *worldv1.SayRequest) (*worldv1.SayResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	if err := s.world.Say(id, conn, req.GetSenderName(), req.GetText()); err != nil {
		return nil, err
	}
	return &worldv1.SayResponse{}, nil
}

// Equip wears an item and answers once its stats apply.
func (s *Server) Equip(ctx context.Context, req *worldv1.EquipRequest) (*worldv1.EquipResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	item, err := kernel.ParseItemID(req.GetItemId())
	if err != nil {
		return nil, err
	}
	if err := s.world.Equip(ctx, id, conn, req.GetIdempotencyKey(), item, req.GetEquipSlot()); err != nil {
		return nil, err
	}
	return &worldv1.EquipResponse{}, nil
}

// UseItem drinks a potion and answers once its HP or MP applies.
func (s *Server) UseItem(ctx context.Context, req *worldv1.UseItemRequest) (*worldv1.UseItemResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	item, err := kernel.ParseItemID(req.GetItemId())
	if err != nil {
		return nil, err
	}
	if err := s.world.UseItem(ctx, id, conn, req.GetIdempotencyKey(), item, req.GetTemplate()); err != nil {
		return nil, err
	}
	return &worldv1.UseItemResponse{}, nil
}

// Unequip puts a worn item back in the Equip tab and answers once the stats apply.
func (s *Server) Unequip(ctx context.Context, req *worldv1.UnequipRequest) (*worldv1.UnequipResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	var to *int
	if req.ToSlot != nil {
		slot := int(req.GetToSlot())
		to = &slot
	}
	if err := s.world.Unequip(ctx, id, conn, req.GetIdempotencyKey(), req.GetEquipSlot(), to); err != nil {
		return nil, err
	}
	return &worldv1.UnequipResponse{}, nil
}

// SpendPoints adds unspent points to the character's stats and answers once the new stats apply.
func (s *Server) SpendPoints(ctx context.Context, req *worldv1.SpendPointsRequest) (*worldv1.SpendPointsResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	pts := req.GetPoints()
	add := gamedata.Stats{STR: int(pts.GetStrength()), DEX: int(pts.GetDexterity()), INT: int(pts.GetIntelligence()), AGI: int(pts.GetAgility()), VIT: int(pts.GetVitality())}
	if err := s.world.SpendPoints(ctx, id, conn, int(req.GetExpectedUnspent()), add); err != nil {
		return nil, err
	}
	return &worldv1.SpendPointsResponse{}, nil
}

// SpendSkillPoints invests unspent skill points and answers once the new sheet applies.
func (s *Server) SpendSkillPoints(ctx context.Context, req *worldv1.SpendSkillPointsRequest) (*worldv1.SpendSkillPointsResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	points := make(map[string]int, len(req.GetPoints()))
	for skill, n := range req.GetPoints() {
		points[skill] = int(n)
	}
	if err := s.world.SpendSkillPoints(ctx, id, conn, int(req.GetExpectedUnspent()), points); err != nil {
		return nil, err
	}
	return &worldv1.SpendSkillPointsResponse{}, nil
}

// UseSkill uses an active skill and answers once the tick resolves it.
func (s *Server) UseSkill(ctx context.Context, req *worldv1.UseSkillRequest) (*worldv1.UseSkillResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	if err := s.world.UseSkill(ctx, id, conn, req.GetSkill(), req.GetSeq()); err != nil {
		return nil, err
	}
	return &worldv1.UseSkillResponse{}, nil
}

// Promote moves the character to the class the NPC within reach trains and answers once it applies.
func (s *Server) Promote(ctx context.Context, req *worldv1.PromoteRequest) (*worldv1.PromoteResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	if err := s.world.Promote(ctx, id, conn, req.GetNpcId(), kernel.Class(req.GetClass())); err != nil {
		return nil, err
	}
	return &worldv1.PromoteResponse{}, nil
}

// ResetPoints returns every spent point to unspent with the free reset and answers once the new stats apply.
func (s *Server) ResetPoints(ctx context.Context, req *worldv1.ResetPointsRequest) (*worldv1.ResetPointsResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	if err := s.world.ResetPoints(ctx, id, conn, int(req.GetExpectedUnspent())); err != nil {
		return nil, err
	}
	return &worldv1.ResetPointsResponse{}, nil
}

// Talk sends the character the dialog of an NPC within reach, and answers once it is queued.
func (s *Server) Talk(ctx context.Context, req *worldv1.TalkRequest) (*worldv1.TalkResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	if err := s.world.Talk(ctx, id, conn, req.GetNpcId()); err != nil {
		return nil, err
	}
	return &worldv1.TalkResponse{}, nil
}

// AcceptQuest takes a quest the NPC within reach gives, and answers once it is in the log.
func (s *Server) AcceptQuest(ctx context.Context, req *worldv1.AcceptQuestRequest) (*worldv1.AcceptQuestResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	if err := s.world.AcceptQuest(ctx, id, conn, req.GetNpcId(), req.GetQuestId()); err != nil {
		return nil, err
	}
	return &worldv1.AcceptQuestResponse{}, nil
}

// AbandonQuest drops an active quest and its progress.
func (s *Server) AbandonQuest(ctx context.Context, req *worldv1.AbandonQuestRequest) (*worldv1.AbandonQuestResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	if err := s.world.AbandonQuest(ctx, id, conn, req.GetQuestId()); err != nil {
		return nil, err
	}
	return &worldv1.AbandonQuestResponse{}, nil
}

// HandInQuest hands a ready quest in, and answers once the item service's answer applies.
func (s *Server) HandInQuest(ctx context.Context, req *worldv1.HandInQuestRequest) (*worldv1.HandInQuestResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	if err := s.world.HandInQuest(ctx, id, conn, req.GetNpcId(), req.GetQuestId()); err != nil {
		return nil, err
	}
	return &worldv1.HandInQuestResponse{}, nil
}

// presence is the character a command is for and the connection it comes from.
func presence(p *worldv1.Presence) (kernel.CharacterID, string, error) {
	id, err := kernel.ParseCharacterID(p.GetCharacterId())
	if err != nil {
		return kernel.CharacterID{}, "", err
	}
	if p.GetConnectionId() == "" {
		return kernel.CharacterID{}, "", status.Error(codes.InvalidArgument, "presence.connection_id is required")
	}
	return id, p.GetConnectionId(), nil
}

func characterView(v model.View) *worldv1.CharacterView {
	return &worldv1.CharacterView{
		Id: v.ID.String(), Name: v.Name, Class: v.Class.String(), Level: int32(v.Level), Exp: v.Exp,
		Stats: stats(v.Stats),
		Hp:    int32(v.HP), MaxHp: int32(v.MaxHP), Mp: int32(v.MP), MaxMp: int32(v.MaxMP),
		MapId: v.MapID, X: v.X, Y: v.Y, Sheet: statSheet(v.Sheet),
	}
}
