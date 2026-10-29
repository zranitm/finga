// Package handler is the world service's transport layer: the WorldService
// gRPC server (api/proto/internal/world/v1) over the world module. Each
// gateway node holds one Deliver stream with a bounded queue; every player
// who joined through that gateway sends its events there, and a stream that
// falls behind is closed, which drops its players at their next event. A
// command names the map its caller believes the player is on and goes to
// that instance, which ignores a player it does not hold.
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
	"finga/services/world/internal/world"
)

// World is what the handler needs from the world module.
type World interface {
	Join(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink world.Sink) (world.View, error)
	Instance(mapID string) (*world.Instance, error)
}

// DefaultQueue is what may wait for one gateway before its stream ends:
// about two seconds of deltas for a few hundred players.
const DefaultQueue = 8192

// ErrNotSubscribed is a Join from a gateway with no Deliver stream open:
// the Snapshot would have nowhere to go.
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
	// BootID is random per process start: a gateway that sees a new one
	// knows every player it had joined is gone.
	BootID string
	// Queue bounds each gateway's stream; DefaultQueue when zero.
	Queue int
	// Logger logs a pickup failure nobody expected; nil discards it.
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

// Deliver streams one gateway's events: Subscribed, then everything bound
// for the players who joined through it.
func (s *Server) Deliver(req *worldv1.DeliverRequest, stream grpc.ServerStreamingServer[worldv1.DeliverResponse]) error {
	if req.GetGatewayId() == "" {
		return status.Error(codes.InvalidArgument, "gateway_id is required")
	}
	first := &worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_Subscribed{Subscribed: &worldv1.Subscribed{BootId: s.bootID}}}
	return grpcx.ServeSubscription(stream, s.deliver.Subscribe(req.GetGatewayId()), first)
}

// Join loads the character, grants its starter items if needed and joins
// it to its map; the Snapshot goes to the gateway's stream.
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
	return &worldv1.LeaveResponse{}, s.command(req.GetPresence(), func(i *world.Instance, id kernel.CharacterID) bool {
		return i.LeaveConnection(id, req.GetConnectionId())
	})
}

// Input queues held keys.
func (s *Server) Input(_ context.Context, req *worldv1.InputRequest) (*worldv1.InputResponse, error) {
	k := req.GetKeys()
	in := sim.Input{Left: k.GetLeft(), Right: k.GetRight(), Up: k.GetUp(), Down: k.GetDown(), Jump: k.GetJump()}
	return &worldv1.InputResponse{}, s.command(req.GetPresence(), func(i *world.Instance, id kernel.CharacterID) bool {
		return i.Input(id, req.GetSeq(), in)
	})
}

// Attack queues a basic attack.
func (s *Server) Attack(_ context.Context, req *worldv1.AttackRequest) (*worldv1.AttackResponse, error) {
	return &worldv1.AttackResponse{}, s.command(req.GetPresence(), (*world.Instance).Attack)
}

// PickUp asks for the nearest eligible drop.
func (s *Server) PickUp(_ context.Context, req *worldv1.PickUpRequest) (*worldv1.PickUpResponse, error) {
	return &worldv1.PickUpResponse{}, s.command(req.GetPresence(), (*world.Instance).PickUp)
}

// EnterPortal takes the portal the character stands in.
func (s *Server) EnterPortal(_ context.Context, req *worldv1.EnterPortalRequest) (*worldv1.EnterPortalResponse, error) {
	return &worldv1.EnterPortalResponse{}, s.command(req.GetPresence(), (*world.Instance).EnterPortal)
}

// Say is a map chat line for everyone on the map.
func (s *Server) Say(_ context.Context, req *worldv1.SayRequest) (*worldv1.SayResponse, error) {
	return &worldv1.SayResponse{}, s.command(req.GetPresence(), func(i *world.Instance, id kernel.CharacterID) bool {
		return i.Chat(id, req.GetSenderName(), req.GetText())
	})
}

// command posts one command to the instance of the presence's map; a full
// inbox is ErrBusy (the gateway drops the command: the next one supersedes
// it).
func (s *Server) command(p *worldv1.Presence, post func(*world.Instance, kernel.CharacterID) bool) error {
	id, err := kernel.ParseCharacterID(p.GetCharacterId())
	if err != nil {
		return err
	}
	inst, err := s.world.Instance(p.GetMapId())
	if err != nil {
		return err
	}
	if !post(inst, id) {
		return world.ErrBusy
	}
	return nil
}

func characterView(v world.View) *worldv1.CharacterView {
	return &worldv1.CharacterView{
		Id: v.ID.String(), Name: v.Name, Class: v.Class.String(), Level: int32(v.Level), Exp: v.Exp,
		Stats: stats(v.Stats),
		Hp:    int32(v.HP), MaxHp: int32(v.MaxHP), Mp: int32(v.MP), MaxMp: int32(v.MaxMP),
		MapId: v.MapID, X: v.X, Y: v.Y,
	}
}
