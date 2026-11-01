package application_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/pkg/auth"
	"finga/services/gateway/internal/application"
	"finga/services/gateway/internal/model"
)

// The use cases run without a socket: a test calls them with a fake Conn,
// as the WebSocket handler does with a connection. The handler's own
// tests (handler/ws) cover the wire.

var (
	account   = kernel.NewAccountID()
	sessionID = kernel.NewSessionID()
	ayla      = kernel.NewCharacterID()
)

const token = "tok"

func TestCommandsForAServiceTheGatewayRunsWithout(t *testing.T) {
	g := application.NewGateway(application.Dependencies{Logger: slog.New(slog.DiscardHandler), Sessions: guard{}, World: &world{}})
	s := g.Open(&conn{})
	ctx := t.Context()
	other := kernel.NewCharacterID()
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"moving an item", second(g.MoveItem(ctx, s, model.ItemMove{IdempotencyKey: "k", Item: kernel.NewItemID(), Tab: "etc", Slot: 1})), model.ErrNoInventory},
		{"discarding an item", second(g.DiscardItem(ctx, s, model.ItemDiscard{IdempotencyKey: "k", Item: kernel.NewItemID(), Quantity: 1})), model.ErrNoInventory},
		{"inviting", g.InviteToParty(ctx, s, other), model.ErrNoParties},
		{"answering an invite", g.AnswerInvite(ctx, s, "inv", true), model.ErrNoParties},
		{"leaving a party", g.LeaveParty(ctx, s), model.ErrNoParties},
		{"kicking from a party", g.KickFromParty(ctx, s, other), model.ErrNoParties},
		{"promoting", g.PromoteInParty(ctx, s, other), model.ErrNoParties},
		{"chatting", g.Chat(ctx, s, protocol.ChatMap, "hi"), model.ErrNoChat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, tt.want) {
				t.Fatalf("err = %v, want %v", tt.err, tt.want)
			}
		})
	}
}

func TestHello(t *testing.T) {
	t.Run("a Hello that joins welcomes the player through the Conn with the rotated token", func(t *testing.T) {
		w := &world{}
		g := application.NewGateway(application.Dependencies{Logger: slog.New(slog.DiscardHandler), Sessions: guard{}, World: w, Clock: func() time.Time { return time.UnixMilli(1000) }})
		c := &conn{}
		s := g.Open(c)
		if err := g.Hello(t.Context(), s, token, ayla); err != nil {
			t.Fatal(err)
		}
		welcome := c.welcomed()
		if welcome == nil || welcome.Token != "rotated" || welcome.Character.ID != ayla.String() || welcome.MapID != "town" || !welcome.ServerTime.Equal(time.UnixMilli(1000)) {
			t.Fatalf("welcome %+v", welcome)
		}
		if s.Character() != ayla {
			t.Fatalf("session plays %s", s.Character())
		}
	})
	for _, tt := range []struct {
		name  string
		token string
		join  error
	}{
		{"a token that is not valid", "nope", nil},
		{"a character that is not the account's", token, &model.ServiceError{Public: model.Public{Code: model.CharacterNotFound}, Err: errors.New("not found")}},
	} {
		t.Run(tt.name+" is rejected, and nobody is welcomed", func(t *testing.T) {
			g := application.NewGateway(application.Dependencies{Logger: slog.New(slog.DiscardHandler), Sessions: guard{}, World: &world{joinErr: tt.join}})
			c := &conn{}
			if err := g.Hello(t.Context(), g.Open(c), tt.token, ayla); !errors.Is(err, model.ErrHelloRejected) {
				t.Fatalf("err = %v, want ErrHelloRejected", err)
			}
			if c.welcomed() != nil {
				t.Fatal("welcomed")
			}
		})
	}
	t.Run("a world that cannot join is a failure the client retries, not a rejection", func(t *testing.T) {
		g := application.NewGateway(application.Dependencies{Logger: slog.New(slog.DiscardHandler), Sessions: guard{}, World: &world{joinErr: errors.New("boom")}})
		err := g.Hello(t.Context(), g.Open(&conn{}), token, ayla)
		if err == nil || errors.Is(err, model.ErrHelloRejected) {
			t.Fatalf("err = %v", err)
		}
	})
}

func second[T any](_ T, err error) error { return err }

// conn records what the use cases tell the player.
type conn struct {
	mu      sync.Mutex
	welcome *model.Welcome
}

func (c *conn) ID() string { return "c-1" }
func (c *conn) Welcome(w model.Welcome) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.welcome = &w
}
func (c *conn) MapChange(string, float64, float64) {}
func (c *conn) PartyGone()                         {}
func (c *conn) Deliver(model.Delivery)             {}
func (c *conn) Kick(protocol.KickReason)           {}
func (c *conn) Close(string)                       {}

func (c *conn) welcomed() *model.Welcome {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.welcome
}

// guard knows one token, and rotates it to "rotated".
type guard struct{}

func (guard) Validate(_ context.Context, tok string) (auth.Principal, error) {
	if tok != token {
		return auth.Principal{}, errors.New("unknown token")
	}
	return auth.Principal{AccountID: uuid.UUID(account), SessionID: uuid.UUID(sessionID)}, nil
}
func (guard) Revoke(context.Context, uuid.UUID) error           { return nil }
func (guard) Rotate(context.Context, uuid.UUID) (string, error) { return "rotated", nil }
func (guard) Check(context.Context, uuid.UUID) error            { return nil }
func (guard) RevokeAll(context.Context, uuid.UUID) error        { return nil }

// world joins every character on the town, or fails with joinErr.
type world struct{ joinErr error }

func (w *world) Join(_ context.Context, _, _ string, _ kernel.AccountID, c kernel.CharacterID) (model.Joined, error) {
	if w.joinErr != nil {
		return model.Joined{}, w.joinErr
	}
	return model.Joined{Character: protocol.CharacterView{ID: c.String(), Name: "Ayla", Level: 1}, MapID: "town"}, nil
}
func (w *world) Leave(context.Context, model.Presence, string) error              { return nil }
func (w *world) Input(context.Context, model.Presence, uint32, model.Keys) error  { return nil }
func (w *world) Attack(context.Context, model.Presence) error                     { return nil }
func (w *world) EnterPortal(context.Context, model.Presence) error                { return nil }
func (w *world) PickUp(context.Context, model.Presence) error                     { return nil }
func (w *world) Say(context.Context, model.Presence, string, string) error        { return nil }
func (w *world) FollowDeliveries(context.Context, string, func(model.WorldEvent)) {}
