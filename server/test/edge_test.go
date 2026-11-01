package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestEdge runs against a stack that is already up (make up): nginx named
// by FINGA_EDGE_URL and the gateway by FINGA_GATEWAY_URL; without both the
// test is skipped. It checks that limits per client address are the edge's
// alone (TD-12): the gateway and the services never read the address. It
// uses up its address's login allowance for a minute, so CI runs it after
// the smoke tests.
func TestEdge(t *testing.T) {
	url, gateway := os.Getenv("FINGA_EDGE_URL"), os.Getenv("FINGA_GATEWAY_URL")
	if url == "" || gateway == "" {
		t.Skip("FINGA_EDGE_URL or FINGA_GATEWAY_URL not set")
	}
	run := time.Now().Format("150405")

	t.Run("the gateway itself limits no address: every login reaches the account service", func(t *testing.T) {
		for i := range 10 {
			body := fmt.Sprintf(`{"username":"direct%s%d","password":"wrong password"}`, run, i)
			if status, _, got := edgeCall(t, http.MethodPost, gateway+"/api/sessions", body); status != http.StatusUnauthorized {
				t.Fatalf("attempt %d: status %d: %s, want the account service's 401", i, status, got)
			}
		}
	})

	t.Run("logins from one address are limited to 5 a minute, refused as the gateway refuses", func(t *testing.T) {
		// Each attempt names a fresh username, so the account service's own
		// throttle (per username) never answers; its 401 is what passes.
		passed := 0
		for i := range 10 {
			body := fmt.Sprintf(`{"username":"edge%s%d","password":"wrong password"}`, run, i)
			status, header, got := edgeCall(t, http.MethodPost, url+"/api/sessions", body)
			if status == http.StatusUnauthorized {
				passed++
				continue
			}
			if status != http.StatusTooManyRequests {
				t.Fatalf("attempt %d: status %d: %s", i, status, got)
			}
			if passed == 0 || passed > 5 {
				t.Fatalf("limited after %d attempts passed, want 1 to 5 (an earlier login from this address counts)", passed)
			}
			if ct := header.Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type %q", ct)
			}
			var e struct{ Code, Message string }
			if err := json.Unmarshal([]byte(got), &e); err != nil || e.Code != "login_rate_limited" || e.Message != "too many login attempts, try again in a minute" {
				t.Fatalf("body %q (err %v), want the gateway's login_rate_limited", got, err)
			}
			return
		}
		t.Fatalf("10 logins from one address passed the edge")
	})

	t.Run("only logging in counts as a login: logging out everywhere still reaches the gateway", func(t *testing.T) {
		if status, _, got := edgeCall(t, http.MethodDelete, url+"/api/sessions", ""); status != http.StatusUnauthorized {
			t.Fatalf("status %d: %s, want the gateway's 401 for a missing token", status, got)
		}
	})

	t.Run("every request to the gateway counts against the address's general limit", func(t *testing.T) {
		for i := range 100 {
			status, _, got := edgeCall(t, http.MethodGet, url+"/api/characters", "")
			switch status {
			case http.StatusUnauthorized:
			case http.StatusTooManyRequests:
				if got != `{"code":"rate_limited","message":"slow down"}` {
					t.Fatalf("body %q, want the gateway's rate_limited", got)
				}
				return
			default:
				t.Fatalf("request %d: status %d: %s", i, status, got)
			}
		}
		t.Fatal("100 requests in a row from one address passed the edge")
	})
}

func edgeCall(t *testing.T, method, url, body string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, strings.TrimSpace(string(raw))
}
