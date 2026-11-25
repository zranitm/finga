package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"gopkg.in/yaml.v3"
)

// gateway is a fake gateway's sign-up calls: each path refused 429 its first refuse[path] times, and how many hashing calls ran at once at most.
type gateway struct {
	mu      sync.Mutex
	refuse  map[string]int
	calls   map[string]int
	hashing int
	most    int
	arrived chan struct{} // a value as each hashing call starts
	release chan struct{} // each hashing call waits for a value, so calls pile up
}

func (g *gateway) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.calls[r.URL.Path]++
	if g.refuse[r.URL.Path] > 0 {
		g.refuse[r.URL.Path]--
		g.mu.Unlock()
		http.Error(rw, `{"code":100007,"message":"slow down"}`, http.StatusTooManyRequests)
		return
	}
	hashes := r.URL.Path != "/api/characters"
	if hashes {
		g.hashing++
		g.most = max(g.most, g.hashing)
	}
	g.mu.Unlock()
	if hashes && g.release != nil {
		g.arrived <- struct{}{}
		<-g.release
	}
	g.mu.Lock()
	if hashes {
		g.hashing--
	}
	g.mu.Unlock()
	rw.WriteHeader(http.StatusCreated)
	_, _ = rw.Write([]byte(`{"token":"t","id":"c"}`))
}

// RoundTrip serves r in process, so a test's backoff runs on synctest's clock.
func (g *gateway) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, r)
	return rec.Result(), nil
}

func signUpBot(url string, k int, hashing chan struct{}) *bot {
	b := newBot(url, "bot"+strings.Repeat("x", k), "Bot", k)
	b.hashing = hashing
	return b
}

// A busy refusal is tried again, each wait about twice the last, until it's let through; any other refusal ends the sign-up.
func TestASignUpRetriesABusyRefusal(t *testing.T) {
	g := &gateway{refuse: map[string]int{"/api/accounts": 3, "/api/sessions": 1, "/api/characters": 1}, calls: map[string]int{}}
	transport := http.DefaultClient.Transport
	http.DefaultClient.Transport = g
	t.Cleanup(func() { http.DefaultClient.Transport = transport })
	synctest.Test(t, func(t *testing.T) {
		b := signUpBot("http://gateway", 0, make(chan struct{}, gatewayHashes))
		start := time.Now()
		if token, err := b.signUp(t.Context()); err != nil || token != "t" || b.id != "c" {
			t.Fatalf("signed up %q as %q: %v", token, b.id, err)
		}
		if g.calls["/api/accounts"] != 4 || g.calls["/api/sessions"] != 2 || g.calls["/api/characters"] != 2 {
			t.Errorf("calls %v", g.calls)
		}
		// the account waits about 0.5, 1 and 2 s, the login and the character 0.5 s each, every wait jittered by half
		if waited := time.Since(start); waited < 2250*time.Millisecond || waited > 6750*time.Millisecond {
			t.Errorf("waited %s", waited)
		}
	})
	http.DefaultClient.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		http.Error(rec, `{"code":110001,"message":"username taken"}`, http.StatusConflict)
		return rec.Result(), nil
	})
	if _, err := signUpBot("http://gateway", 0, make(chan struct{}, 1)).signUp(t.Context()); err == nil || busy(err) {
		t.Errorf("a conflict: %v", err)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A crowd of bots signs up never more than the gateway's hashes at once, and every one gets in.
func TestBotsSignUpNoMoreHashesAtOnceThanTheGatewayRuns(t *testing.T) {
	const n = 12
	g := &gateway{refuse: map[string]int{}, calls: map[string]int{}, arrived: make(chan struct{}, 2*n), release: make(chan struct{}, 2*n)}
	srv := httptest.NewServer(g)
	defer srv.Close()
	hashing := make(chan struct{}, gatewayHashes)
	var wg sync.WaitGroup
	errs := make([]error, n)
	for k := range n {
		b := signUpBot(srv.URL, k+1, hashing)
		wg.Go(func() { _, errs[k] = b.signUp(t.Context()) })
	}
	for range gatewayHashes {
		<-g.arrived
	}
	select { // the four hold every slot till released, so no fifth starts
	case <-g.arrived:
		t.Error("a hash past the gateway's started")
	case <-time.After(100 * time.Millisecond):
	}
	for range 2 * n { // an account and a login each
		g.release <- struct{}{}
	}
	wg.Wait()
	for k, err := range errs {
		if err != nil {
			t.Errorf("bot %d: %v", k, err)
		}
	}
	if g.most != gatewayHashes {
		t.Errorf("%d hashes at once; want %d", g.most, gatewayHashes)
	}
}

// The bots' hashes at once are the gateway's, as config/config.yaml sets them.
func TestTheBotsPaceToTheGatewaysHashes(t *testing.T) {
	raw, err := os.ReadFile("../../config/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Account struct {
			Password struct {
				Concurrent int `yaml:"concurrent"`
			} `yaml:"password"`
		} `yaml:"account"`
	}
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if c.Account.Password.Concurrent != gatewayHashes {
		t.Errorf("the gateway runs %d hashes at once; the bots pace to %d", c.Account.Password.Concurrent, gatewayHashes)
	}
}
