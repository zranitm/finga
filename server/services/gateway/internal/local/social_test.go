package local

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/panics"
	"finga/services/gateway/internal/model"
	"finga/services/social"
)

// leaving counts the social module's GatewayLeft calls.
type leaving struct {
	*social.Module
	mu   sync.Mutex
	left []string
}

func (l *leaving) GatewayLeft(gateway string) {
	l.mu.Lock()
	l.left = append(l.left, gateway)
	l.mu.Unlock()
	l.Module.GatewayLeft(gateway)
}

// newSocial is the social module on a host never run, over the game data.
func newSocial(t *testing.T) *social.Module {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	h, err := host.New(host.Options{Name: "finga", Settings: host.Settings{DataDir: "../../../../data"}, Logger: log, Registry: prometheus.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	data, err := h.GameData()
	if err != nil {
		t.Fatal(err)
	}
	m, err := h.Module("social")
	if err != nil {
		t.Fatal(err)
	}
	s, err := social.New(t.Context(), social.Deps{Module: m, Data: data}) // its retention never runs on a host never run
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A stream that breaks is followed again at once, and the gateway registers its players again: the module's sweep of the gone gateway finds it back, and every party stays.
func TestABrokenSocialStreamFollowedAgainKeepsItsParties(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mod := &leaving{Module: newSocial(t)}
		s := NewSocial(mod, slog.New(slog.DiscardHandler), panics.NewRecorder(slog.New(slog.DiscardHandler), nil))
		ann := model.Player{ID: kernel.NewCharacterID(), Name: "Ann", Class: "warrior", Level: 3, HP: 90, MaxHP: 100}
		bo := model.Player{ID: kernel.NewCharacterID(), Name: "Bo", Class: "warrior", Level: 3, HP: 90, MaxHP: 100}
		register := func() {
			for k, p := range []model.Player{ann, bo} {
				if err := s.Online(context.Background(), "gw", "conn-"+p.Name, uint64(k+1), p, "forest"); err != nil {
					t.Error(err)
				}
			}
		}
		var mu sync.Mutex
		subscribed, invites, parties := 0, []string{}, map[kernel.CharacterID]*model.PartyView{}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go s.FollowDeliveries(ctx, "gw", func(e model.SocialEvent) {
			mu.Lock()
			defer mu.Unlock()
			if _, ok := e.(model.SocialSubscribed); ok {
				subscribed++
				go register() // as the push flow does, off the stream's goroutine
			}
			d, _ := e.(model.SocialDelivery)
			if p, ok := d.Delivery.Push.(model.PartyInvited); ok {
				invites = append(invites, p.InviteID)
			}
			if p, ok := d.Delivery.Push.(model.PartyState); ok {
				parties[d.Character] = p.Party
			}
		})
		synctest.Wait()
		if err := s.Invite(ctx, ann.ID, bo.ID); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err := s.Respond(ctx, bo.ID, invites[0], true); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()

		mod.Subscribe("gw") // a newer subscription under the gateway's ID closes the stream, as a full queue does
		synctest.Wait()
		time.Sleep(time.Second) // past the follower's longest backoff
		synctest.Wait()
		time.Sleep(time.Minute) // past the module's grace, when its sweep of the gateway looks
		synctest.Wait()

		mu.Lock()
		defer mu.Unlock()
		if subscribed != 2 || len(mod.left) != 1 || mod.left[0] != "gw" {
			t.Fatalf("subscribed %d times, the gateway left %v; want a second subscription after one GatewayLeft", subscribed, mod.left)
		}
		for _, id := range []kernel.CharacterID{ann.ID, bo.ID} {
			p := parties[id]
			if p == nil || len(p.Members) != 2 || !p.Members[0].Online || !p.Members[1].Online {
				t.Fatalf("%s's party %+v; want both members, online", id, p)
			}
		}
		var members int
		if err := mod.Memberships(ctx, func(_ social.PartyID, in []kernel.CharacterID) { members += len(in) }); err != nil || members != 2 {
			t.Fatalf("the module holds %d members in parties (%v)", members, err)
		}
		cancel()
	})
}
