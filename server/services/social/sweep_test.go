package social

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/pkg/hub"
	"finga/pkg/panics"
	"finga/services/social/internal/application"
	"finga/services/social/internal/model"
	"finga/services/social/internal/repository"
)

// faulty runs fault inside the module before each Online and Offline the party service takes, to hold or panic there.
type faulty struct {
	parties
	fault func(call string, id kernel.CharacterID)
}

func (f faulty) Online(ctx context.Context, m model.Member) error {
	f.fault("online", m.ID)
	return f.parties.Online(ctx, m)
}

func (f faulty) Offline(ctx context.Context, id kernel.CharacterID, conn string) error {
	f.fault("offline", id)
	return f.parties.Offline(ctx, id, conn)
}

// newLive is the module over the real party and chat services, with fault run inside them and rec recording its panics.
func newLive(t *testing.T, fault func(call string, id kernel.CharacterID), rec *panics.Recorder) *Module {
	t.Helper()
	notes := newNotifier()
	ps := application.NewPartyService(application.PartyDeps{Store: repository.NewMemory(), Notifier: notes})
	return build(t, faulty{parties: ps, fault: fault}, application.NewChatService(application.ChatDeps{Parties: ps, LineMax: 120}), notes, rec)
}

func discarding() *panics.Recorder { return panics.NewRecorder(slog.New(slog.DiscardHandler), nil) }

// at is id online through gateway, on the forest map, behind the world's join of sequence seq.
func at(gateway string, id kernel.CharacterID, seq uint64) Presence {
	return Presence{Member: Member{ID: id, Name: "X", Level: 3, HP: 90, MaxHP: 100, MapID: "forest"}, Gateway: gateway, Connection: "conn-" + id.String(), JoinSeq: seq}
}

func register(t *testing.T, m *Module, p Presence) {
	t.Helper()
	if err := m.Online(t.Context(), p); err != nil {
		t.Fatal(err)
	}
}

func formParty(t *testing.T, m *Module, leader, member kernel.CharacterID) {
	t.Helper()
	inv, err := m.Invite(t.Context(), leader, member)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Respond(t.Context(), member, inv, true); err != nil {
		t.Fatal(err)
	}
}

// online is whether each member was online in the last party state sub heard them in.
func online(sub *hub.Sub[Delivery]) map[kernel.CharacterID]bool {
	out := map[kernel.CharacterID]bool{}
	for _, d := range drain(sub) {
		if s, ok := d.(PartyState); ok && s.Party != nil {
			for _, m := range s.Party.Members {
				out[m.ID] = m.Online
			}
		}
	}
	return out
}

// A sweep cut short, here by its timeout while an Online holds the registration, tries again, as a gateway gone for good never resubscribes to finish it.
func TestASweepCutShortTriesAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ann, bo, cy, di := kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID()
		release := make(chan struct{})
		m := newLive(t, func(call string, id kernel.CharacterID) {
			if call == "online" && id == cy {
				<-release
			}
		}, discarding())
		gone, stays := m.Subscribe("gw-a"), m.Subscribe("gw-b")
		register(t, m, at("gw-a", ann, 1))
		register(t, m, at("gw-a", bo, 2))
		register(t, m, at("gw-b", di, 3)) // Di, on a gateway that stays, hears the party
		formParty(t, m, ann, bo)
		formParty(t, m, ann, di)
		held := make(chan error, 1)
		go func() { held <- m.Online(t.Context(), at("gw-b", cy, 4)) }()
		synctest.Wait()
		gone.Close()
		m.GatewayLeft("gw-a")
		time.Sleep(45 * time.Second) // the sweep starts after the grace and times out waiting on Cy's registration
		close(release)
		if err := <-held; err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if got := online(stays); got[ann] || got[bo] || !got[di] {
			t.Fatalf("Di's party %v, after Ann and Bo's gateway went and the sweep's first try timed out", got)
		}
	})
}

// countedPanics counts recorded panics by flow.
type countedPanics struct {
	mu     sync.Mutex
	byFlow map[string]int
}

func (c *countedPanics) Inc(flow string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byFlow[flow]++
}

// A bug the sweep meets in the module would fail every retry, so the sweep records it once, as its job's, and stops.
func TestASweepThatPanicsInTheModuleStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		count := &countedPanics{byFlow: map[string]int{}}
		m := newLive(t, func(call string, _ kernel.CharacterID) {
			if call == "offline" {
				panic("store corrupt")
			}
		}, panics.NewRecorder(slog.New(slog.DiscardHandler), count))
		sub := m.Subscribe("gw-a")
		ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
		register(t, m, at("gw-a", ann, 1))
		register(t, m, at("gw-a", bo, 2))
		formParty(t, m, ann, bo)
		sub.Close()
		m.GatewayLeft("gw-a")
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		count.mu.Lock()
		defer count.mu.Unlock()
		if len(count.byFlow) != 1 || count.byFlow[panics.FlowJob] != 1 {
			t.Fatalf("panics recorded in ten minutes, by flow: %v; want one, the sweep's job", count.byFlow)
		}
	})
}

// A gateway that comes back while its players are being taken offline keeps each one it registers again.
func TestAGatewayBackMidSweepKeepsThePlayersItRegistersAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate, reached := make(chan struct{}), make(chan struct{}, 1)
		m := newLive(t, func(call string, _ kernel.CharacterID) {
			if call != "offline" {
				return
			}
			select {
			case reached <- struct{}{}:
			default:
			}
			<-gate
		}, discarding())
		gone, stays := m.Subscribe("gw-a"), m.Subscribe("gw-b")
		ann, bo, di := kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID()
		register(t, m, at("gw-a", ann, 1))
		register(t, m, at("gw-a", bo, 2))
		register(t, m, at("gw-b", di, 3))
		formParty(t, m, ann, bo)
		formParty(t, m, ann, di)
		gone.Close()
		m.GatewayLeft("gw-a")
		<-reached // the sweep is taking its first player offline
		m.Subscribe("gw-a")
		again := make(chan error, 2)
		for i, id := range []kernel.CharacterID{ann, bo} {
			go func() { again <- m.Online(t.Context(), at("gw-a", id, uint64(4+i))) }()
		}
		synctest.Wait()
		close(gate)
		for range 2 {
			if err := <-again; err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		if got := online(stays); !got[ann] || !got[bo] {
			t.Fatalf("Di's party %v, after Ann and Bo's gateway registered them again", got)
		}
	})
}
