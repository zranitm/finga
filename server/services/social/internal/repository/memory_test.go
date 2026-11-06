package repository_test

import (
	"context"
	"errors"
	"testing"

	"finga/internal/kernel"
	"finga/services/social/internal/application"
	"finga/services/social/internal/model"
	"finga/services/social/internal/repository"
)

var errAbort = errors.New("abort")

var (
	partyID  = model.NewPartyID()
	inviteID = model.NewInviteID()
)

func TestMemoryCommitsOnlyWhatSucceeds(t *testing.T) {
	ctx := t.Context()
	s := repository.NewMemory()
	ayla, robin := kernel.NewCharacterID(), kernel.NewCharacterID()
	party := model.Party{ID: partyID, Leader: ayla, Members: []model.Member{{ID: ayla, Name: "Ayla"}, {ID: robin, Name: "Robin"}}}

	t.Run("a failed transaction leaves nothing behind", func(t *testing.T) {
		err := s.InTx(ctx, func(tx application.PartyTx) error {
			tx.PutMember(model.Member{ID: ayla, Name: "Ayla"})
			tx.PutParty(party)
			tx.PutInvite(model.Invite{ID: inviteID, From: ayla, To: robin})
			if tx.NextJoinOrder() != 1 {
				t.Error("the first join order is not 1")
			}
			if _, ok := tx.PartyOf(robin); !ok {
				t.Error("a write is not visible inside its own transaction")
			}
			return errAbort
		})
		if !errors.Is(err, errAbort) {
			t.Fatalf("err = %v", err)
		}
		view(t, s, func(tx application.PartyTx) {
			_, member := tx.Member(ayla)
			_, inParty := tx.PartyOf(ayla)
			_, invited := tx.Invite(inviteID)
			if member || inParty || invited || len(tx.Parties()) != 0 || len(tx.Invites()) != 0 || tx.NextJoinOrder() != 1 {
				t.Fatal("the failed transaction's writes landed")
			}
		})
	})
	t.Run("a successful one lands, found by any member", func(t *testing.T) {
		update(t, s, func(tx application.PartyTx) {
			tx.PutMember(model.Member{ID: ayla, Name: "Ayla", Online: true})
			tx.PutParty(party)
			tx.PutInvite(model.Invite{ID: inviteID, From: ayla, To: robin})
		})
		view(t, s, func(tx application.PartyTx) {
			for _, id := range []kernel.CharacterID{ayla, robin} {
				if p, ok := tx.PartyOf(id); !ok || p.ID != partyID || len(p.Members) != 2 {
					t.Fatalf("party of %s: %+v %v", id, p, ok)
				}
			}
			if m, ok := tx.Member(ayla); !ok || !m.Online {
				t.Fatalf("member %+v %v", m, ok)
			}
			if inv, ok := tx.Invite(inviteID); !ok || inv.To != robin || len(tx.Invites()) != 1 || len(tx.Parties()) != 1 {
				t.Fatalf("invite %+v %v", inv, ok)
			}
		})
	})
	t.Run("a member the party no longer lists is no longer found by it", func(t *testing.T) {
		update(t, s, func(tx application.PartyTx) {
			p, _ := tx.PartyOf(ayla)
			p.Members = p.Members[:1]
			tx.PutParty(p)
			if _, ok := tx.PartyOf(robin); ok {
				t.Error("inside the transaction, Robin still finds the party he left")
			}
		})
		view(t, s, func(tx application.PartyTx) {
			if _, ok := tx.PartyOf(robin); ok {
				t.Fatal("Robin still finds the party he left")
			}
			if p, ok := tx.PartyOf(ayla); !ok || len(p.Members) != 1 {
				t.Fatalf("Ayla's party %+v %v", p, ok)
			}
		})
	})
	t.Run("reads are copies", func(t *testing.T) {
		view(t, s, func(tx application.PartyTx) {
			p, _ := tx.PartyOf(ayla)
			p.Members[0].Name = "changed"
		})
		view(t, s, func(tx application.PartyTx) {
			if p, _ := tx.PartyOf(ayla); p.Members[0].Name != "Ayla" {
				t.Fatal("changing a read changed the store")
			}
		})
	})
	t.Run("deleting a party and an invite removes them for everyone", func(t *testing.T) {
		update(t, s, func(tx application.PartyTx) {
			tx.DeleteParty(partyID)
			tx.DeleteInvite(inviteID)
		})
		view(t, s, func(tx application.PartyTx) {
			_, inParty := tx.PartyOf(ayla)
			_, invited := tx.Invite(inviteID)
			if inParty || invited || len(tx.Parties()) != 0 || len(tx.Invites()) != 0 {
				t.Fatal("a deleted party or invite is still there")
			}
		})
	})
	t.Run("join orders keep increasing across transactions", func(t *testing.T) {
		var first, second uint64
		update(t, s, func(tx application.PartyTx) { first = tx.NextJoinOrder() })
		update(t, s, func(tx application.PartyTx) { second = tx.NextJoinOrder() })
		if first == 0 || second <= first {
			t.Fatalf("join orders %d then %d", first, second)
		}
	})
}

func update(t *testing.T, s *repository.Memory, fn func(application.PartyTx)) {
	t.Helper()
	if err := s.InTx(context.Background(), func(tx application.PartyTx) error { fn(tx); return nil }); err != nil {
		t.Fatal(err)
	}
}

// view reads in a transaction it then aborts, so it never changes the store.
func view(t *testing.T, s *repository.Memory, fn func(application.PartyTx)) {
	t.Helper()
	if err := s.InTx(context.Background(), func(tx application.PartyTx) error { fn(tx); return errAbort }); !errors.Is(err, errAbort) {
		t.Fatal(err)
	}
}

func TestMemoryDeletesAMemberOnlyOnCommit(t *testing.T) {
	ctx := t.Context()
	s := repository.NewMemory()
	ayla, robin := kernel.NewCharacterID(), kernel.NewCharacterID()
	update(t, s, func(tx application.PartyTx) {
		tx.PutMember(model.Member{ID: ayla, Name: "Ayla"})
		tx.PutMember(model.Member{ID: robin, Name: "Robin"})
	})
	err := s.InTx(ctx, func(tx application.PartyTx) error {
		tx.DeleteMember(ayla)
		if _, ok := tx.Member(ayla); ok || len(tx.Members()) != 1 {
			t.Error("inside the transaction, the deleted member is still there")
		}
		return errAbort
	})
	if !errors.Is(err, errAbort) {
		t.Fatalf("err = %v", err)
	}
	view(t, s, func(tx application.PartyTx) {
		if _, ok := tx.Member(ayla); !ok || len(tx.Members()) != 2 {
			t.Fatal("a failed transaction's delete landed")
		}
	})
	update(t, s, func(tx application.PartyTx) { tx.DeleteMember(ayla) })
	view(t, s, func(tx application.PartyTx) {
		if _, ok := tx.Member(ayla); ok || len(tx.Members()) != 1 || tx.Members()[0].ID != robin {
			t.Fatalf("after the delete: %+v", tx.Members())
		}
	})
}
