// Package handler is the world service's gRPC server: one bounded Deliver stream per gateway.
package handler

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	worldv1 "finga/api/gen/world/v1"
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
	Input(id kernel.CharacterID, conn string, seq uint32, in sim.Input) error
	Attack(id kernel.CharacterID, conn string) error
	PickUp(id kernel.CharacterID, conn string) error
	EnterPortal(id kernel.CharacterID, conn string) error
	Say(id kernel.CharacterID, conn, name, text string) error
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
	log      *slog.Logger
}

// Options configure the server.
type Options struct {
	// BootID is random per process start, so a gateway can tell its players are gone.
	BootID string
	// TickRateHz is the maps' ticks per second, told to each joining client.
	TickRateHz int
	Logger     *slog.Logger
}

// New builds the server.
func New(w World, opts Options) *Server {
	return &Server{world: w, bootID: opts.BootID, tickRate: uint32(opts.TickRateHz), deliver: hub.New[*worldv1.DeliverResponse](queue), log: opts.Logger}
}

// Deliver streams one gateway's events, starting with Subscribed.
func (s *Server) Deliver(req *worldv1.DeliverRequest, stream grpc.ServerStreamingServer[worldv1.DeliverResponse]) error {
	if req.GetGatewayId() == "" {
		return status.Error(codes.InvalidArgument, "gateway_id is required")
	}
	first := &worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_Subscribed{Subscribed: &worldv1.Subscribed{BootId: s.bootID}}}
	return grpcx.ServeSubscription(stream, s.deliver.Subscribe(req.GetGatewayId()), first)
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
	v, err := s.world.Join(ctx, account, id, req.GetConnectionId(), &streamSink{sub: sub, character: id.String(), conn: req.GetConnectionId(), log: s.log})
	if err != nil {
		return nil, err
	}
	return &worldv1.JoinResponse{Character: characterView(v), TickRateHz: s.tickRate}, nil
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
	if err := s.world.Input(id, conn, req.GetSeq(), in); err != nil {
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
	if err := s.world.Attack(id, conn); err != nil {
		return nil, err
	}
	return &worldv1.AttackResponse{}, nil
}

// PickUp asks for the nearest eligible drop.
func (s *Server) PickUp(_ context.Context, req *worldv1.PickUpRequest) (*worldv1.PickUpResponse, error) {
	id, conn, err := presence(req.GetPresence())
	if err != nil {
		return nil, err
	}
	if err := s.world.PickUp(id, conn); err != nil {
		return nil, err
	}
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
		MapId: v.MapID, X: v.X, Y: v.Y,
	}
}
