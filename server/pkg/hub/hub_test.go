package hub_test

import (
	"sync"
	"testing"

	"finga/pkg/hub"
)

func TestSendReachesOnlyItsSubscriber(t *testing.T) {
	h := hub.New[string](4)
	a, b := h.Subscribe("a"), h.Subscribe("b")
	if !a.Send("for a") {
		t.Fatal("send refused")
	}
	if got := <-a.C(); got != "for a" {
		t.Fatalf("a got %q", got)
	}
	select {
	case got := <-b.C():
		t.Fatalf("b got %q", got)
	default:
	}
}

func TestGetFindsTheCurrentSubscription(t *testing.T) {
	h := hub.New[int](4)
	if _, ok := h.Get("a"); ok {
		t.Fatal("found a subscription nobody made")
	}
	a := h.Subscribe("a")
	if got, ok := h.Get("a"); !ok || got != a {
		t.Fatal("Get does not return the subscription")
	}
	a.Close()
	if _, ok := h.Get("a"); ok {
		t.Fatal("a closed subscription is still found")
	}
}

func TestBroadcastReachesEveryone(t *testing.T) {
	h := hub.New[int](4)
	subs := []*hub.Sub[int]{h.Subscribe("a"), h.Subscribe("b"), h.Subscribe("c")}
	h.Broadcast(7)
	for i, s := range subs {
		if got := <-s.C(); got != 7 {
			t.Fatalf("subscriber %d got %d", i, got)
		}
	}
}

func TestAFullQueueClosesThatSubscription(t *testing.T) {
	h := hub.New[int](2)
	slow, fast := h.Subscribe("slow"), h.Subscribe("fast")
	for i := range 2 {
		if !slow.Send(i) {
			t.Fatalf("send %d refused", i)
		}
	}
	if slow.Send(2) {
		t.Fatal("a third send fitted a queue of two")
	}
	select {
	case <-slow.Done():
	default:
		t.Fatal("the overflowing subscription is still open")
	}
	if slow.Send(3) {
		t.Fatal("a closed subscription accepted a send")
	}
	if _, ok := h.Get("slow"); ok {
		t.Fatal("the hub still lists the closed subscription")
	}
	h.Broadcast(9) // the closed one is skipped, the open one still served
	if got := <-fast.C(); got != 9 {
		t.Fatalf("fast got %d", got)
	}
}

func TestBroadcastClosesAFullSubscription(t *testing.T) {
	h := hub.New[int](1)
	s := h.Subscribe("a")
	h.Broadcast(1)
	h.Broadcast(2)
	select {
	case <-s.Done():
	default:
		t.Fatal("still open after overflowing")
	}
}

func TestSubscribingAgainReplacesTheOld(t *testing.T) {
	h := hub.New[int](4)
	old := h.Subscribe("gw")
	fresh := h.Subscribe("gw")
	select {
	case <-old.Done():
	default:
		t.Fatal("the old subscription is still open")
	}
	if old.Send(1) {
		t.Fatal("the old subscription accepted a send")
	}
	if got, _ := h.Get("gw"); got != fresh {
		t.Fatal("Get does not return the new subscription")
	}
	old.Close() // closing the replaced one leaves the new one alone
	if got, ok := h.Get("gw"); !ok || got != fresh {
		t.Fatal("closing the old subscription removed the new one")
	}
	if !fresh.Send(2) || <-fresh.C() != 2 {
		t.Fatal("the new subscription does not receive")
	}
}

func TestCloseIsIdempotentAndSafeUnderSends(t *testing.T) {
	h := hub.New[int](8)
	s := h.Subscribe("a")
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 100 {
				s.Send(i)
				h.Broadcast(i)
			}
		}()
	}
	s.Close()
	s.Close()
	wg.Wait()
	select {
	case <-s.Done():
	default:
		t.Fatal("not closed")
	}
}
