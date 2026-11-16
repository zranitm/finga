package e2etest

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// Take removes and returns the first recorded element matching ok, waiting up to d; others are kept.
func Take[T any](t *testing.T, l *Looter, what string, recorded *[]T, d time.Duration, ok func(T) bool) T {
	t.Helper()
	var got T
	found := l.Until(d, func() bool {
		for k, v := range *recorded {
			if ok == nil || ok(v) {
				got = v
				*recorded = append((*recorded)[:k], (*recorded)[k+1:]...)
				return true
			}
		}
		return false
	})
	if !found {
		t.Fatalf("%s: no %s within %s (recorded %+v)", l.ID, what, d, *recorded)
	}
	return got
}

// PartyState waits for a PartyState with n members (0: no party).
func (l *Looter) PartyState(t *testing.T, n int) *clientv1.PartyState {
	t.Helper()
	return Take(t, l, "PartyState", &l.PartyStates, 5*time.Second, func(ps *clientv1.PartyState) bool {
		if n == 0 {
			return ps.Party == nil
		}
		return ps.Party != nil && len(ps.GetParty().GetMembers()) == n
	})
}

// Command sends d under the request id req and waits for its Response.
func (l *Looter) Command(t *testing.T, req string, d proto.Message) *clientv1.Response {
	t.Helper()
	if err := l.trySend(wsconn.Message{ReqID: req, Data: d}); err != nil {
		t.Fatal(err)
	}
	return Take(t, l, "Response "+req, &l.Responses, 5*time.Second, func(r Reply) bool { return r.Req == req }).R
}

// Response waits for a Response without a request ID (a refused chat line).
func (l *Looter) Response(t *testing.T) *clientv1.Response {
	t.Helper()
	return Take(t, l, "Response", &l.Responses, 5*time.Second, func(r Reply) bool { return r.Req == "" }).R
}

// Say sends text on channel.
func (l *Looter) Say(t *testing.T, channel clientv1.ChatChannel, text string) {
	t.Helper()
	if err := l.trySend(wsconn.Message{Data: &clientv1.ChatSend{Channel: channel, Text: text}}); err != nil {
		t.Fatal(err)
	}
}

// Hears waits up to 5 s for a chat line of text.
func (l *Looter) Hears(t *testing.T, text string) *clientv1.ChatMessage {
	t.Helper()
	return Take(t, l, "ChatMessage "+text, &l.ChatLines, 5*time.Second, func(cm *clientv1.ChatMessage) bool { return cm.Text == text })
}

// DoesNotHear is whether no chat line of text comes within d.
func (l *Looter) DoesNotHear(text string, d time.Duration) bool {
	return !l.Until(d, func() bool {
		for _, cm := range l.ChatLines {
			if cm.Text == text {
				return true
			}
		}
		return false
	})
}
