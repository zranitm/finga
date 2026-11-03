package application_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/kernel"
	"finga/pkg/auth"
	"finga/pkg/panics"
	"finga/services/gateway/internal/application"
	"finga/services/gateway/internal/model"
)

var (
	account   = kernel.NewAccountID()
	sessionID = kernel.NewSessionID()
	ayla      = kernel.NewCharacterID()
)

const token = "tok"

func TestCommandsForAServiceTheGatewayRunsWithout(t *testing.T) {
	g := application.NewGateway(application.Dependencies{Logger: slog.New(slog.DiscardHandler), Sessions: guard{}, World: &world{}})
	s := g.Open("c-1", &outbox{})
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
		{"chatting", g.Chat(ctx, s, clientv1.ChatChannel_CHAT_CHANNEL_MAP, "hi"), model.ErrNoChat},
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
	t.Run("a Hello that joins welcomes the player through the Outbox with the rotated token", func(t *testing.T) {
		w := &world{}
		g := application.NewGateway(application.Dependencies{Logger: slog.New(slog.DiscardHandler), Sessions: guard{}, World: w, Clock: func() time.Time { return time.UnixMilli(1000) }})
		c := &outbox{}
		s := g.Open("c-1", c)
		if err := g.Hello(t.Context(), s, token, ayla); err != nil {
			t.Fatal(err)
		}
		welcome := c.welcomed()
		if welcome == nil || welcome.Token != "rotated" || welcome.Character.GetId() != ayla.String() || welcome.MapID != "town" || !welcome.ServerTime.Equal(time.UnixMilli(1000)) {
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
			c := &outbox{}
			if err := g.Hello(t.Context(), g.Open("c-1", c), tt.token, ayla); !errors.Is(err, model.ErrHelloRejected) {
				t.Fatalf("err = %v, want ErrHelloRejected", err)
			}
			if c.welcomed() != nil {
				t.Fatal("welcomed")
			}
		})
	}
	t.Run("a world that cannot join is a failure the client retries, not a rejection", func(t *testing.T) {
		g := application.NewGateway(application.Dependencies{Logger: slog.New(slog.DiscardHandler), Sessions: guard{}, World: &world{joinErr: errors.New("boom")}})
		err := g.Hello(t.Context(), g.Open("c-1", &outbox{}), token, ayla)
		if err == nil || errors.Is(err, model.ErrHelloRejected) {
			t.Fatalf("err = %v", err)
		}
	})
}

func second[T any](_ T, err error) error { return err }

// outbox keeps the Welcome and how the session first ended.
type outbox struct {
	mu      sync.Mutex
	welcome *model.Welcome
	ends    chan model.EndReason // nil: ends are not kept
}

func (o *outbox) Welcome(w model.Welcome) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.welcome = &w
}
func (o *outbox) Deliver(model.Delivery) {}
func (o *outbox) End(r model.EndReason) {
	select {
	case o.ends <- r:
	default:
	}
}

func (o *outbox) welcomed() *model.Welcome {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.welcome
}

func (o *outbox) ended(t *testing.T) model.EndReason {
	t.Helper()
	select {
	case r := <-o.ends:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("the session never ended")
		return ""
	}
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

// world joins every character on the town, or fails with joinErr; a join first delivers flood
// deliveries, its Snapshot first, through the stream the gateway follows.
type world struct {
	mu          sync.Mutex
	joinErr     error
	flood       int
	followPanic string
	deliver     func(model.WorldEvent)
	following   chan struct{} // closed once the gateway follows the stream
}

func (w *world) Join(_ context.Context, _, _ string, _ kernel.AccountID, c kernel.CharacterID) (model.Joined, error) {
	w.mu.Lock()
	err, flood, deliver := w.joinErr, w.flood, w.deliver
	w.mu.Unlock()
	if err != nil {
		return model.Joined{}, err
	}
	for i := range flood {
		d := model.Delivery{Push: model.Delta{}}
		if i == 0 {
			d.Push = model.Snapshot{}
		}
		deliver(model.WorldEvent{Character: c, Delivery: d})
	}
	return model.Joined{Character: &clientv1.CharacterView{Id: c.String(), Name: "Ayla", Level: 1}, MapID: "town"}, nil
}

func (w *world) failJoins(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.joinErr = err
}

// reconnect is the stream breaking and opening again.
func (w *world) reconnect() {
	w.mu.Lock()
	deliver := w.deliver
	w.mu.Unlock()
	deliver(model.WorldEvent{Subscribed: true})
}
func (w *world) Leave(context.Context, model.Presence, string) error             { return nil }
func (w *world) Input(context.Context, model.Presence, uint32, model.Keys) error { return nil }
func (w *world) Attack(context.Context, model.Presence) error                    { return nil }
func (w *world) EnterPortal(context.Context, model.Presence) error               { return nil }
func (w *world) PickUp(context.Context, model.Presence) error                    { return nil }
func (w *world) Say(context.Context, model.Presence, string, string) error       { return nil }
func (w *world) FollowDeliveries(ctx context.Context, _ string, deliver func(model.WorldEvent)) {
	if w.followPanic != "" {
		panic(w.followPanic)
	}
	w.mu.Lock()
	w.deliver = deliver
	w.mu.Unlock()
	deliver(model.WorldEvent{Subscribed: true})
	close(w.following)
	<-ctx.Done()
}

// running runs a gateway on w until the test ends.
func running(t *testing.T, w *world) *application.Gateway {
	t.Helper()
	w.following = make(chan struct{})
	g := application.NewGateway(application.Dependencies{Logger: slog.New(slog.DiscardHandler), Sessions: guard{}, World: w, SessionCheckInterval: time.Hour})
	go func() { _ = g.Run(t.Context()) }()
	select {
	case <-w.following:
	case <-time.After(5 * time.Second):
		t.Fatal("the gateway never followed the world")
	}
	return g
}

func TestSessionsTheGatewayEnds(t *testing.T) {
	t.Run("a join holding more deliveries than it may ends as delivery_backlog", func(t *testing.T) {
		g := running(t, &world{flood: 200})
		o := &outbox{ends: make(chan model.EndReason, 1)}
		if err := g.Hello(t.Context(), g.Open("c-1", o), token, ayla); err != nil {
			t.Fatal(err)
		}
		if got := o.ended(t); got != model.EndDeliveryBacklog {
			t.Fatalf("ended as %s", got)
		}
	})
	t.Run("a rejoin the world refuses after its stream broke ends as rejoin_failed", func(t *testing.T) {
		w := &world{}
		g := running(t, w)
		o := &outbox{ends: make(chan model.EndReason, 1)}
		if err := g.Hello(t.Context(), g.Open("c-1", o), token, ayla); err != nil {
			t.Fatal(err)
		}
		w.failJoins(errors.New("boom"))
		w.reconnect()
		if got := o.ended(t); got != model.EndRejoinFailed {
			t.Fatalf("ended as %s", got)
		}
	})
}

func TestAPanickingFollowerStopsRun(t *testing.T) {
	g := application.NewGateway(application.Dependencies{Logger: slog.New(slog.DiscardHandler), Sessions: guard{}, World: &world{followPanic: "bad stream"}, SessionCheckInterval: time.Hour})
	done := make(chan error, 1)
	go func() { done <- g.Run(context.Background()) }()
	select {
	case err := <-done:
		if e, ok := errors.AsType[*panics.Error](err); !ok || e.Value != "bad stream" {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept going after a follower panicked")
	}
}
