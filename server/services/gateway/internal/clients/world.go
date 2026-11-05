package clients

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/panics"
	"finga/services/gateway/internal/clients/errmap"
	"finga/services/gateway/internal/model"
)

// World is the application's World over the world service.
type World struct {
	c      worldv1.WorldServiceClient
	log    *slog.Logger
	panics *panics.Recorder
}

// NewWorld adapts the world client; log hears the stream's breaks, and rec a panic handling it.
func NewWorld(c worldv1.WorldServiceClient, log *slog.Logger, rec *panics.Recorder) World {
	return World{c: c, log: log, panics: rec}
}

// Join joins a character through one of this gateway's connections.
func (w World) Join(ctx context.Context, gateway, conn string, account kernel.AccountID, character kernel.CharacterID) (model.Joined, error) {
	res, err := w.c.Join(ctx, &worldv1.JoinRequest{GatewayId: gateway, ConnectionId: conn, AccountId: account.String(), CharacterId: character.String()})
	if err != nil {
		return model.Joined{}, errmap.Wrap(err)
	}
	c := res.GetCharacter()
	return model.Joined{Character: characterView(c), MapID: c.GetMapId(), X: c.GetX(), Y: c.GetY()}, nil
}

// Leave removes the player if it joined through the presence's connection.
func (w World) Leave(ctx context.Context, p model.Presence) error {
	_, err := w.c.Leave(ctx, &worldv1.LeaveRequest{Presence: presence(p)})
	return errmap.Wrap(err)
}

// Input sends one input.
func (w World) Input(ctx context.Context, p model.Presence, seq uint32, k model.Keys) error {
	_, err := w.c.Input(ctx, &worldv1.InputRequest{Presence: presence(p), Seq: seq, Keys: &worldv1.Keys{Left: k.Left, Right: k.Right, Up: k.Up, Down: k.Down, Jump: k.Jump}})
	return errmap.Wrap(err)
}

// Attack attacks.
func (w World) Attack(ctx context.Context, p model.Presence) error {
	_, err := w.c.Attack(ctx, &worldv1.AttackRequest{Presence: presence(p)})
	return errmap.Wrap(err)
}

// EnterPortal takes the portal the player stands in.
func (w World) EnterPortal(ctx context.Context, p model.Presence) error {
	_, err := w.c.EnterPortal(ctx, &worldv1.EnterPortalRequest{Presence: presence(p)})
	return errmap.Wrap(err)
}

// PickUp picks up the drop under the player.
func (w World) PickUp(ctx context.Context, p model.Presence) error {
	_, err := w.c.PickUp(ctx, &worldv1.PickUpRequest{Presence: presence(p)})
	return errmap.Wrap(err)
}

// Say hands a cleaned map line to the player's map.
func (w World) Say(ctx context.Context, p model.Presence, name, text string) error {
	_, err := w.c.Say(ctx, &worldv1.SayRequest{Presence: presence(p), SenderName: name, Text: text})
	return errmap.Wrap(err)
}

// FollowDeliveries follows this gateway's delivery stream until ctx ends.
func (w World) FollowDeliveries(ctx context.Context, gateway string, deliver func(model.WorldEvent)) {
	grpcx.Follow(ctx, func(ctx context.Context) (grpc.ServerStreamingClient[worldv1.DeliverResponse], error) {
		return w.c.Deliver(ctx, &worldv1.DeliverRequest{GatewayId: gateway})
	}, func(m *worldv1.DeliverResponse) {
		if m.GetSubscribed() != nil {
			deliver(model.WorldEvent{Subscribed: true})
			return
		}
		character, _ := kernel.ParseCharacterID(m.GetCharacterId()) // zero if bad: the delivery still routes by connection
		if m.GetDetached() != nil {
			deliver(model.WorldEvent{Connection: m.GetConnectionId(), Character: character, Detached: true})
			return
		}
		p, ok := worldPush(m)
		if !ok {
			return
		}
		if f := m.GetPickupFailed(); f != nil && f.GetErrorReason() == "" {
			w.log.WarnContext(ctx, "pickup failed for a reason the world could not name", "character_id", m.GetCharacterId())
		}
		deliver(model.WorldEvent{Connection: m.GetConnectionId(), Character: character, Delivery: model.Delivery{Tick: m.GetTick(), Push: p}})
	}, grpcx.FollowOptions{Panics: w.panics, OnBreak: func(err error) {
		w.log.WarnContext(ctx, "world stream broke; subscribing again", "err", err)
	}})
}

func presence(p model.Presence) *worldv1.Presence {
	return &worldv1.Presence{CharacterId: p.Character.String(), ConnectionId: p.Connection}
}
