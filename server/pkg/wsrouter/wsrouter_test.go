package wsrouter_test

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"finga/pkg/wsrouter"
)

// conn is a test's connection state: it records what ran, in order.
type conn struct {
	mu    sync.Mutex
	trace []string
}

func (c *conn) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.trace = append(c.trace, s)
}

func (c *conn) got() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.trace...)
}

type move struct{ To int }

// mark records its name before and after the rest of the chain.
func mark(name string) wsrouter.HandlerFunc[*conn] {
	return func(c *wsrouter.Context[*conn]) {
		c.Session.add(name)
		c.Next()
		c.Session.add("/" + name)
	}
}

func TestTheChainRunsInOrder(t *testing.T) {
	r := wsrouter.New(mark("global"))
	wsrouter.Handle(r, "Move", func(c *wsrouter.Context[*conn], in move) (any, error) {
		c.Session.add("handler")
		return nil, nil
	}, mark("route"))
	s := &conn{}
	r.Dispatch(s, wsrouter.Message{Type: "Move", Data: move{To: 1}})
	want := []string{"global", "route", "handler", "/route", "/global"}
	if got := s.got(); !reflect.DeepEqual(got, want) {
		t.Fatalf("trace %v, want %v", got, want)
	}
}

func TestAMiddlewareThatDoesNotCallNextStopsTheChain(t *testing.T) {
	r := wsrouter.New[*conn]()
	wsrouter.Handle(r, "Move", func(c *wsrouter.Context[*conn], in move) (any, error) {
		c.Session.add("handler")
		return nil, nil
	}, func(c *wsrouter.Context[*conn]) { c.Session.add("refused") })
	s := &conn{}
	r.Dispatch(s, wsrouter.Message{Type: "Move", Data: move{}})
	if got := s.got(); !reflect.DeepEqual(got, []string{"refused"}) {
		t.Fatalf("trace %v", got)
	}
}

func TestTheHandlerGetsItsTypeAndItsAnswerIsRecorded(t *testing.T) {
	errFull := errors.New("full")
	tests := []struct {
		name       string
		reply      any
		err        error
		wantResult any
	}{
		{"a result", "moved", nil, "moved"},
		{"an error", nil, errFull, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen *wsrouter.Context[*conn]
			r := wsrouter.New(func(c *wsrouter.Context[*conn]) { c.Next(); seen = c })
			wsrouter.Handle(r, "Move", func(c *wsrouter.Context[*conn], in move) (any, error) {
				if in.To != 7 || c.Message.ReqID != "r-1" || c.Message.Seq != 3 {
					t.Errorf("handler got %+v, message %+v", in, c.Message)
				}
				return tt.reply, tt.err
			})
			r.Dispatch(&conn{}, wsrouter.Message{Type: "Move", ReqID: "r-1", Seq: 3, Data: move{To: 7}})
			if seen.Result != tt.wantResult || !errors.Is(seen.Err, tt.err) {
				t.Fatalf("result %v, err %v", seen.Result, seen.Err)
			}
		})
	}
}

func TestDataOfTheWrongTypeNeverReachesTheHandler(t *testing.T) {
	var seen *wsrouter.Context[*conn]
	r := wsrouter.New(func(c *wsrouter.Context[*conn]) { c.Next(); seen = c })
	wsrouter.Handle(r, "Move", func(*wsrouter.Context[*conn], move) (any, error) {
		t.Fatal("the handler ran")
		return nil, nil
	})
	r.Dispatch(&conn{}, wsrouter.Message{Type: "Move", Data: "not a move"})
	if seen.Err == nil || !strings.Contains(seen.Err.Error(), "Move") {
		t.Fatalf("err %v, want one naming the message", seen.Err)
	}
}

func TestAnUnroutedTypeGoesToNoRoute(t *testing.T) {
	r := wsrouter.New(mark("global"))
	s := &conn{}
	r.Dispatch(s, wsrouter.Message{Type: "Trade"})
	if got := s.got(); !reflect.DeepEqual(got, []string{"global", "/global"}) {
		t.Fatalf("with no NoRoute only the global chain runs: %v", got)
	}
	r.NoRoute(func(c *wsrouter.Context[*conn]) { c.Session.add("no route " + c.Message.Type) })
	s = &conn{}
	r.Dispatch(s, wsrouter.Message{Type: "Trade"})
	if got := s.got(); !reflect.DeepEqual(got, []string{"global", "no route Trade", "/global"}) {
		t.Fatalf("trace %v", got)
	}
}

// A middleware may continue the chain on another goroutine, as a command queue does.
func TestTheRestOfTheChainCanRunElsewhere(t *testing.T) {
	queue := make(chan func(), 1)
	r := wsrouter.New[*conn]()
	wsrouter.Handle(r, "Move", func(c *wsrouter.Context[*conn], in move) (any, error) {
		c.Session.add("handler")
		return nil, nil
	}, func(c *wsrouter.Context[*conn]) {
		c.Session.add("queued")
		queue <- c.Next
	}, mark("after the queue"))
	s := &conn{}
	r.Dispatch(s, wsrouter.Message{Type: "Move", Data: move{}})
	if got := s.got(); !reflect.DeepEqual(got, []string{"queued"}) {
		t.Fatalf("before the queue ran: %v", got)
	}
	done := make(chan struct{})
	go func() { defer close(done); (<-queue)() }()
	<-done
	want := []string{"queued", "after the queue", "handler", "/after the queue"}
	if got := s.got(); !reflect.DeepEqual(got, want) {
		t.Fatalf("trace %v, want %v", got, want)
	}
}

func TestATypeIsRoutedOnce(t *testing.T) {
	r := wsrouter.New[*conn]()
	h := func(*wsrouter.Context[*conn], move) (any, error) { return nil, nil }
	wsrouter.Handle(r, "Move", h)
	defer func() {
		if recover() == nil {
			t.Fatal("a second route for Move was accepted")
		}
	}()
	wsrouter.Handle(r, "Move", h)
}

func TestTypesListsTheRoutes(t *testing.T) {
	r := wsrouter.New[*conn]()
	h := func(*wsrouter.Context[*conn], move) (any, error) { return nil, nil }
	wsrouter.Handle(r, "Move", h)
	wsrouter.Handle(r, "Attack", h)
	if got := r.Types(); !reflect.DeepEqual(got, []string{"Attack", "Move"}) {
		t.Fatalf("types %v", got)
	}
}
