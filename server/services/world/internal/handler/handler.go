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
	"finga/services/world/internal/application"
	"finga/services/world/internal/model"
)

// World is what the handler needs from the world module.
type World interface {
	Join(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink application.Sink) (model.View, error)
	Instance(mapID string) (*application.Instance, error)
}

// DefaultQueue bounds a gateway's stream: about two seconds of deltas for a few hundred players.
const DefaultQueue = 8192

// ErrNotSubscribed is a Join from a gateway with no Deliver stream open.
var ErrNotSubscribed = errors.New("world: gateway has no deliver stream")

// Server serves WorldService.
type Server struct {
	worldv1.UnimplementedWorldServiceServer
	world   World
	bootID  string
	deliver *hub.Hub[*worldv1.DeliverResponse]
	log     *slog.Logger
}

// Options configure the server.
type Options struct {
	// BootID is random per process start, so a gateway can tell its players are gone.
	BootID string
	Queue  int
	Logger *slog.Logger
}

// New builds the server.
func New(w World, opts Options) *Server {
	if opts.Queue <= 0 {
		opts.Queue = DefaultQueue
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	return &Server{world: w, bootID: opts.BootID, deliver: hub.New[*worldv1.DeliverResponse](opts.Queue), log: opts.Logger}
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
	v, err := s.world.Join(ctx, account, id, req.GetConnectionId(), &streamSink{sub: sub, character: id.String(), log: s.log})
	if err != nil {
		return nil, err
	}
	return &worldv1.JoinResponse{Character: characterView(v)}, nil
}

// Leave removes the player if it joined through the named connection.
func (s *Server) Leave(_ context.Context, req *worldv1.LeaveRequest) (*worldv1.LeaveResponse, error) {
	if req.GetConnectionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "connection_id is required")
	}
	return &worldv1.LeaveResponse{}, s.command(req.GetPresence(), func(i *application.Instance, id kernel.CharacterID) bool {
		return i.LeaveConnection(id, req.GetConnectionId())
	})
}

// Input queues held keys.
func (s *Server) Input(_ context.Context, req *worldv1.InputRequest) (*worldv1.InputResponse, error) {
	k := req.GetKeys()
	in := sim.Input{Left: k.GetLeft(), Right: k.GetRight(), Up: k.GetUp(), Down: k.GetDown(), Jump: k.GetJump()}
	return &worldv1.InputResponse{}, s.command(req.GetPresence(), func(i *application.Instance, id kernel.CharacterID) bool {
		return i.Input(id, req.GetSeq(), in)
	})
}

// Attack queues a basic attack.
func (s *Server) Attack(_ context.Context, req *worldv1.AttackRequest) (*worldv1.AttackResponse, error) {
	return &worldv1.AttackResponse{}, s.command(req.GetPresence(), (*application.Instance).Attack)
}

// PickUp asks for the nearest eligible drop.
func (s *Server) PickUp(_ context.Context, req *worldv1.PickUpRequest) (*worldv1.PickUpResponse, error) {
	return &worldv1.PickUpResponse{}, s.command(req.GetPresence(), (*application.Instance).PickUp)
}

// EnterPortal takes the portal the character stands in.
func (s *Server) EnterPortal(_ context.Context, req *worldv1.EnterPortalRequest) (*worldv1.EnterPortalResponse, error) {
	return &worldv1.EnterPortalResponse{}, s.command(req.GetPresence(), (*application.Instance).EnterPortal)
}

// Say is a map chat line for everyone on the map.
func (s *Server) Say(_ context.Context, req *worldv1.SayRequest) (*worldv1.SayResponse, error) {
	return &worldv1.SayResponse{}, s.command(req.GetPresence(), func(i *application.Instance, id kernel.CharacterID) bool {
		return i.Chat(id, req.GetSenderName(), req.GetText())
	})
}

// command posts to the instance of the presence's map; a full inbox is ErrBusy.
func (s *Server) command(p *worldv1.Presence, post func(*application.Instance, kernel.CharacterID) bool) error {
	id, err := kernel.ParseCharacterID(p.GetCharacterId())
	if err != nil {
		return err
	}
	inst, err := s.world.Instance(p.GetMapId())
	if err != nil {
		return err
	}
	if !post(inst, id) {
		return model.ErrBusy
	}
	return nil
}

func characterView(v model.View) *worldv1.CharacterView {
	return &worldv1.CharacterView{
		Id: v.ID.String(), Name: v.Name, Class: v.Class.String(), Level: int32(v.Level), Exp: v.Exp,
		Stats: stats(v.Stats),
		Hp:    int32(v.HP), MaxHp: int32(v.MaxHP), Mp: int32(v.MP), MaxMp: int32(v.MaxMP),
		MapId: v.MapID, X: v.X, Y: v.Y,
	}
}
