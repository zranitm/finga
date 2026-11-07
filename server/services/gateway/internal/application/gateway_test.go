package application_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace/noop"

	"finga/internal/kernel"
	"finga/pkg/auth"
	"finga/pkg/panics"
	"finga/services/gateway/internal/application/push"
	"finga/services/gateway/internal/application/send"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/model"
)

var (
	account   = kernel.NewAccountID()
	sessionID = kernel.NewSessionID()
	ayla      = kernel.NewCharacterID()
)

const token = "tok"

// gateway is both flows over one registry, as the composition root builds them.
type gateway struct {
	*send.Gateway
	pushes *push.Gateway
}

const joinTimeout = 5 * time.Second

func newGateway(w *world) gateway { return newGatewayWith(w, &guard{}, time.Hour) }

func newGatewayWith(w *world, g *guard, checkEvery time.Duration) gateway {
	log := slog.New(slog.DiscardHandler)
	sessions := session.NewRegistry(session.Deps{World: w, Social: idle{}, Logger: log, Panics: panics.NewRecorder(log, nil), Traces: noop.NewTracerProvider()}, session.Options{Gateway: "gw-1", JoinTimeout: joinTimeout})
	return gateway{
		Gateway: send.New(send.Deps{Sessions: g, Registry: sessions, World: w, Items: idle{}, Social: idle{}, Logger: log, Observer: idle{}}, send.Options{JoinTimeout: joinTimeout}),
		pushes:  push.New(push.Deps{Sessions: g, Registry: sessions, World: w, Social: idle{}, Account: idle{}, Logger: log, Traces: noop.NewTracerProvider()}, push.Options{SessionCheckInterval: checkEvery}),
	}
}

// idle is every service these tests do not exercise: it answers nothing went wrong, and its streams wait for their end.
type idle struct{}

func (idle) Inventory(context.Context, kernel.CharacterID) (model.Inventory, error) {
	return model.Inventory{}, nil
}
func (idle) Move(context.Context, model.Actor, model.ItemMove) (model.InventoryChanged, error) {
	return model.InventoryChanged{}, nil
}
func (idle) Discard(context.Context, model.Actor, model.ItemDiscard) (model.InventoryChanged, error) {
	return model.InventoryChanged{}, nil
}
func (idle) Chat(_ context.Context, _ model.Player, channel model.ChatChannel, text string) (model.ChatLine, error) {
	return model.ChatLine{Channel: channel, Text: text}, nil
}
func (idle) Invite(context.Context, kernel.CharacterID, kernel.CharacterID) error       { return nil }
func (idle) Respond(context.Context, kernel.CharacterID, string, bool) error            { return nil }
func (idle) Leave(context.Context, kernel.CharacterID) error                            { return nil }
func (idle) Kick(context.Context, kernel.CharacterID, kernel.CharacterID) error         { return nil }
func (idle) Promote(context.Context, kernel.CharacterID, kernel.CharacterID) error      { return nil }
func (idle) Online(context.Context, string, string, uint64, model.Player, string) error { return nil }
func (idle) Offline(context.Context, kernel.CharacterID, string) error                  { return nil }
func (idle) OnlineChanged(int)                                                          {}
func (idle) FollowDeliveries(ctx context.Context, _ string, _ func(model.SocialEvent)) {
	<-ctx.Done()
}
func (idle) FollowSessionEnds(ctx context.Context, _ string, _ func(model.SessionEnd)) {
	<-ctx.Done()
}

func TestHello(t *testing.T) {
	t.Run("a Hello that joins welcomes the player through the Outbox with the rotated token", func(t *testing.T) {
		g := newGateway(&world{})
		c := &outbox{}
		s := g.Open("c-1", c)
		before := time.Now()
		if err := g.Hello(t.Context(), s, token, ayla); err != nil {
			t.Fatal(err)
		}
		welcome := c.welcomed()
		if welcome == nil || welcome.Token != "rotated" || welcome.Character.ID != ayla.String() || welcome.MapID != "town" ||
			welcome.ServerTime.Before(before) || welcome.ServerTime.After(time.Now()) {
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
		{"a character that is not the account's", token, &model.ServiceError{Public: model.CharacterNotFound, Err: errors.New("not found")}},
	} {
		t.Run(tt.name+" is rejected, and nobody is welcomed", func(t *testing.T) {
			g := newGateway(&world{joinErr: tt.join})
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
		g := newGateway(&world{joinErr: errors.New("boom")})
		err := g.Hello(t.Context(), g.Open("c-1", &outbox{}), token, ayla)
		if err == nil || errors.Is(err, model.ErrHelloRejected) {
			t.Fatalf("err = %v", err)
		}
	})
}

// The account service's answers as the guard's client gives them.
var (
	unauthenticated = &model.ServiceError{Public: model.Unauthenticated, Err: errors.New("session ended")}
	unavailable     = &model.ServiceError{Public: model.Internal, Kind: model.ErrUnavailable, Err: errors.New("connection refused")}
)

// Only the account service saying unauthenticated rejects a Hello; anything else is a failure the client retries.
func TestHelloAndTheAccountService(t *testing.T) {
	t.Run("an account service that is down fails the Hello without rejecting it", func(t *testing.T) {
		g := newGatewayWith(&world{}, &guard{validate: unavailable}, time.Hour)
		c := &outbox{}
		err := g.Hello(t.Context(), g.Open("c-1", c), token, ayla)
		if err == nil || errors.Is(err, model.ErrHelloRejected) || c.welcomed() != nil {
			t.Fatalf("err = %v, welcomed %v", err, c.welcomed() != nil)
		}
	})
	t.Run("a join that fails rotates no token, so the client's still works", func(t *testing.T) {
		auth := &guard{}
		g := newGatewayWith(&world{joinErr: errors.New("boom")}, auth, time.Hour)
		if err := g.Hello(t.Context(), g.Open("c-1", &outbox{}), token, ayla); err == nil {
			t.Fatal("the Hello joined")
		}
		if auth.rotated() != 0 {
			t.Fatalf("%d rotations before any join succeeded", auth.rotated())
		}
	})
	for _, tt := range []struct {
		name     string
		err      error
		rejected bool
	}{
		{"a rotation the account service cannot make", unavailable, false},
		{"a rotation of a session that ended", unauthenticated, true},
	} {
		t.Run(tt.name+" welcomes nobody and takes the player off the map", func(t *testing.T) {
			w := &world{}
			g := newGatewayWith(w, &guard{rotate: tt.err}, time.Hour)
			c := &outbox{}
			s := g.Open("c-1", c)
			err := g.Hello(t.Context(), s, token, ayla)
			if err == nil || errors.Is(err, model.ErrHelloRejected) != tt.rejected || c.welcomed() != nil {
				t.Fatalf("err = %v, welcomed %v", err, c.welcomed() != nil)
			}
			g.Closed(s, "hello failed")
			if w.left() != 1 {
				t.Fatalf("%d leaves after the Hello failed past its join", w.left())
			}
		})
	}
}

// The minute check ends a session only when the account service says it ended; an outage or a hung call ends nobody.
func TestTheSessionCheckKicksOnlyEndedSessions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		auth := &guard{check: unavailable}
		w := &world{following: make(chan struct{})}
		g := newGatewayWith(w, auth, time.Minute)
		go func() { _ = g.pushes.Run(t.Context()) }()
		<-w.following
		o := &outbox{ends: make(chan model.EndReason, 1)}
		if err := g.Hello(t.Context(), g.Open("c-1", o), token, ayla); err != nil {
			t.Fatal(err)
		}
		still := func(when string) {
			t.Helper()
			select {
			case r := <-o.ends:
				t.Fatalf("ended as %s %s", r, when)
			default:
			}
		}
		time.Sleep(time.Minute + time.Second)
		synctest.Wait()
		still("while the account service was down")
		auth.set(func(g *guard) { g.check, g.hang = nil, true })
		time.Sleep(time.Minute + 10*time.Second)
		synctest.Wait()
		still("while its check hung")
		auth.set(func(g *guard) { g.check, g.hang = unauthenticated, false })
		time.Sleep(time.Minute)
		synctest.Wait()
		select {
		case r := <-o.ends:
			if r != model.EndSessionExpired {
				t.Fatalf("ended as %s", r)
			}
		default:
			t.Fatal("a session the account service ended is still open")
		}
	})
}

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

// guard knows one token, and rotates it to "rotated"; each set error is what that call fails with.
type guard struct {
	mu        sync.Mutex
	validate  error
	rotate    error
	check     error
	hang      bool // Check waits until its context ends
	rotations int
}

func (g *guard) set(change func(g *guard)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	change(g)
}

func (g *guard) rotated() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.rotations
}

func (g *guard) Validate(_ context.Context, tok string) (auth.Principal, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.validate != nil {
		return auth.Principal{}, g.validate
	}
	if tok != token {
		return auth.Principal{}, unauthenticated
	}
	return auth.Principal{AccountID: uuid.UUID(account), SessionID: uuid.UUID(sessionID)}, nil
}

func (g *guard) Rotate(context.Context, uuid.UUID) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.rotate != nil {
		return "", g.rotate
	}
	g.rotations++
	return "rotated", nil
}

func (g *guard) Check(ctx context.Context, _ uuid.UUID) error {
	g.mu.Lock()
	err, hang := g.check, g.hang
	g.mu.Unlock()
	if hang {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}

func (*guard) Revoke(context.Context, uuid.UUID) error    { return nil }
func (*guard) RevokeAll(context.Context, uuid.UUID) error { return nil }

// world joins every character on the town, or fails with joinErr; a join delivers flood deliveries, Snapshot first.
type world struct {
	mu          sync.Mutex
	joinErr     error
	flood       int
	followPanic string
	deliver     func(model.WorldEvent)
	following   chan struct{} // closed once the gateway follows the stream
	leaves      int
}

func (w *world) left() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.leaves
}

func (w *world) Join(_ context.Context, _, conn string, _ kernel.AccountID, c kernel.CharacterID) (model.Joined, error) {
	w.mu.Lock()
	err, flood, deliver := w.joinErr, w.flood, w.deliver
	w.mu.Unlock()
	if err != nil {
		return model.Joined{}, err
	}
	for i := range flood {
		d := model.Delivery{Push: model.Delta{Frame: &model.Frame{}}}
		if i == 0 {
			d.Push = model.Snapshot{}
		}
		deliver(model.WorldDelivery{Connection: conn, Delivery: d})
	}
	return model.Joined{Character: model.CharacterView{ID: c.String(), Name: "Ayla", Level: 1}, MapID: "town"}, nil
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
	deliver(model.WorldSubscribed{})
}
func (w *world) Leave(context.Context, model.Presence) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.leaves++
	return nil
}
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
	deliver(model.WorldSubscribed{})
	close(w.following)
	<-ctx.Done()
}

// running runs a gateway's push flow on w until the test ends.
func running(t *testing.T, w *world) gateway {
	t.Helper()
	w.following = make(chan struct{})
	g := newGateway(w)
	go func() { _ = g.pushes.Run(t.Context()) }()
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
	g := newGateway(&world{followPanic: "bad stream"})
	done := make(chan error, 1)
	go func() { done <- g.pushes.Run(context.Background()) }()
	select {
	case err := <-done:
		if e, ok := errors.AsType[*panics.Error](err); !ok || e.Value != "bad stream" {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept going after a follower panicked")
	}
}
