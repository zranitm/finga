package local_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/pkg/hub"
	"finga/pkg/uuidv7"
	"finga/services/social"
	"finga/services/world/internal/local"
	"finga/services/world/internal/model"
)

type table struct {
	party   social.PartyID
	members []kernel.CharacterID
}

// socialFake follows the social module's calls: each subscription is a real hub's, and the table is read under a call.
type socialFake struct {
	hub *hub.Hub[social.MembershipEvent]

	mu       sync.Mutex
	worlds   []string
	subs     []*hub.Sub[social.MembershipEvent]
	snapshot []table
	failFor  int // the first Memberships calls fail
	calls    int
	updates  []social.MemberUpdate
	news     []social.Announcement
	err      error
}

func newSocialFake() *socialFake { return &socialFake{hub: hub.New[social.MembershipEvent](8)} }

func (f *socialFake) SubscribeMemberships(world string) *hub.Sub[social.MembershipEvent] {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.worlds = append(f.worlds, world)
	sub := f.hub.Subscribe(world)
	f.subs = append(f.subs, sub)
	return sub
}

func (f *socialFake) Memberships(_ context.Context, visit func(social.PartyID, []kernel.CharacterID)) error {
	f.mu.Lock()
	f.calls++
	fail, snapshot := f.calls <= f.failFor, f.snapshot
	f.mu.Unlock()
	if fail {
		return errors.New("table unavailable")
	}
	for _, e := range snapshot {
		visit(e.party, e.members)
	}
	return nil
}

func (f *socialFake) UpdateMember(_ context.Context, u social.MemberUpdate) error {
	f.updates = append(f.updates, u)
	return f.err
}

func (f *socialFake) Announce(_ context.Context, a social.Announcement) error {
	f.news = append(f.news, a)
	return f.err
}

func TestFollowPartiesTellsSubscribedThenTheTableThenEachChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ann, bo, cy := kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID()
		p1 := social.PartyID{}
		f := newSocialFake()
		f.snapshot = []table{{party: p1, members: []kernel.CharacterID{ann}}}
		ctx, cancel := context.WithCancel(t.Context())
		var got []model.PartyEvent
		go local.NewSocial(f, nil).FollowParties(ctx, "world-1", func(e model.PartyEvent) { got = append(got, e) })
		synctest.Wait()
		members := []kernel.CharacterID{bo, cy}
		f.hub.Broadcast(social.Membership{Characters: []kernel.CharacterID{bo, cy, ann}, Members: members, Party: p1})
		f.hub.Broadcast(social.Membership{Characters: []kernel.CharacterID{ann}})
		synctest.Wait()
		cancel()
		synctest.Wait()

		want := []model.PartyEvent{
			model.PartiesSubscribed{},
			model.Membership{Characters: []kernel.CharacterID{ann}, Party: model.Party{Members: []kernel.CharacterID{ann}}},
			model.Membership{Characters: []kernel.CharacterID{bo, cy, ann}, Party: model.Party{Members: members}},
			model.Membership{Characters: []kernel.CharacterID{ann}, Party: model.Party{Members: []kernel.CharacterID{}}},
		}
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(f.worlds, []string{"world-1"}) {
			t.Fatalf("got %#v\nwant %#v\nworlds %v", got, want, f.worlds)
		}
	})
}

func TestFollowPartiesGivesAPartyItsIDAndCopiesTheSlices(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
		var id social.PartyID
		if err := id.UnmarshalText([]byte(uuidv7.NewString())); err != nil {
			t.Fatal(err)
		}
		f := newSocialFake()
		ctx, cancel := context.WithCancel(t.Context())
		var got []model.PartyEvent
		go local.NewSocial(f, nil).FollowParties(ctx, "w", func(e model.PartyEvent) { got = append(got, e) })
		synctest.Wait()
		sent := social.Membership{Characters: []kernel.CharacterID{ann, bo}, Members: []kernel.CharacterID{ann, bo}, Party: id}
		f.hub.Broadcast(sent)
		synctest.Wait()
		cancel()
		synctest.Wait()

		m, ok := got[1].(model.Membership)
		if !ok || m.Party.ID != id.String() {
			t.Fatalf("got %#v", got)
		}
		sent.Characters[0], sent.Members[0] = bo, bo
		if m.Characters[0] != ann || m.Party.Members[0] != ann {
			t.Fatal("the party copy shares a slice with the module's event")
		}
	})
}

func TestFollowPartiesSubscribesAgainAfterAClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ann := kernel.NewCharacterID()
		f := newSocialFake()
		f.snapshot = []table{{members: []kernel.CharacterID{ann}}}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var got []model.PartyEvent
		go local.NewSocial(f, nil).FollowParties(ctx, "w", func(e model.PartyEvent) { got = append(got, e) })
		synctest.Wait()
		sub, _ := f.hub.Get("w")
		sub.Close() // as a full queue closes it
		synctest.Wait()
		time.Sleep(time.Second) // past the longest backoff
		synctest.Wait()

		if len(got) != 5 {
			t.Fatalf("got %#v", got)
		}
		lost, ok := got[2].(model.PartiesLost)
		if !ok || lost.Err == nil {
			t.Fatalf("after the close: %#v", got[2])
		}
		if _, ok := got[3].(model.PartiesSubscribed); !ok {
			t.Fatalf("after the loss: %#v", got[3])
		}
		if _, ok := got[4].(model.Membership); !ok || !reflect.DeepEqual(f.worlds, []string{"w", "w"}) {
			t.Fatalf("the table was not told again: %#v, worlds %v", got[4], f.worlds)
		}
	})
}

func TestFollowPartiesClosesAndRetriesASubscriptionWhoseTableFailed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSocialFake()
		f.failFor = 1
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var got []model.PartyEvent
		go local.NewSocial(f, nil).FollowParties(ctx, "w", func(e model.PartyEvent) { got = append(got, e) })
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()

		if len(got) != 3 || len(f.worlds) != 2 {
			t.Fatalf("got %#v, worlds %v", got, f.worlds)
		}
		if lost, ok := got[1].(model.PartiesLost); !ok || lost.Err == nil || lost.Err.Error() != "table unavailable" {
			t.Fatalf("after the failed table: %#v", got[1])
		}
		if _, ok := got[2].(model.PartiesSubscribed); !ok {
			t.Fatalf("the retry: %#v", got[2])
		}
		select {
		case <-f.subs[0].Done():
		default:
			t.Fatal("the failed subscription was left open")
		}
	})
}

func TestUpdateMemberTellsTheSocialModuleWhatAMapSaw(t *testing.T) {
	f := newSocialFake()
	ann := kernel.NewCharacterID()
	u := model.MemberUpdate{ID: ann, MapID: "cave", Class: kernel.Class("bowman"), Level: 5, HP: 40, MaxHP: 120}
	if err := local.NewSocial(f, nil).UpdateMember(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	want := social.MemberUpdate{ID: ann, MapID: "cave", Class: kernel.Class("bowman"), Level: 5, HP: 40, MaxHP: 120}
	if !reflect.DeepEqual(f.updates, []social.MemberUpdate{want}) {
		t.Fatalf("update %+v", f.updates)
	}
	cause := errors.New("down")
	f.err = cause
	if err := local.NewSocial(f, nil).UpdateMember(t.Context(), u); !errors.Is(err, cause) {
		t.Fatalf("got %v", err)
	}
}

func TestAnnounceSendsABossAppearing(t *testing.T) {
	f := newSocialFake()
	s := local.NewSocial(f, nil)
	if err := s.Announce(t.Context(), model.BossAppeared{Boss: "king_slime", Map: "forest"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.news, []social.Announcement{social.BossAppeared{Boss: "king_slime", MapID: "forest"}}) {
		t.Fatalf("news %+v", f.news)
	}
	cause := errors.New("down")
	f.err = cause
	if err := s.Announce(t.Context(), model.BossAppeared{}); !errors.Is(err, cause) {
		t.Fatalf("got %v", err)
	}
}
