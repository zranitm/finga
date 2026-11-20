package e2etest

import (
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// SwitchChannel asks to switch to the region's channel n, pressed during the last input sent, and returns its Response.
func (l *Looter) SwitchChannel(t *testing.T, req string, n int) *clientv1.Response {
	t.Helper()
	if err := l.trySend(wsconn.Message{ReqID: req, Seq: l.Seq, Data: &clientv1.SwitchChannel{Channel: uint32(n)}}); err != nil {
		t.Fatal(err)
	}
	return Take(t, l, "Response "+req, &l.Responses, 5*time.Second, func(r Reply) bool { return r.Req == req }).R
}

// Channels asks for the open channels of the looter's region, and returns the Response carrying them.
func (l *Looter) Channels(t *testing.T, req string) *clientv1.Response {
	t.Helper()
	if err := l.trySend(wsconn.Message{ReqID: req, Data: &clientv1.Channels{}}); err != nil {
		t.Fatal(err)
	}
	return Take(t, l, "Response "+req, &l.Responses, 5*time.Second, func(r Reply) bool { return r.Req == req }).R
}
