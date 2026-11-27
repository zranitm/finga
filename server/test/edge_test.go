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

// TestEdge checks per-address limits are nginx's alone; it uses up a minute's logins, so CI runs it after smoke tests.
func TestEdge(t *testing.T) {
	t.Parallel()
	url, direct := os.Getenv("FINGA_EDGE_URL"), os.Getenv("FINGA_SERVER_URL")
	if url == "" || direct == "" {
		t.Skip("FINGA_EDGE_URL or FINGA_SERVER_URL not set")
	}
	run := time.Now().Format("150405")

	t.Run("the server itself limits no address: every login reaches the account module", func(t *testing.T) {
		for i := range 10 {
			body := fmt.Sprintf(`{"username":"direct%s%d","password":"wrong password"}`, run, i)
			if status, _, got := edgeCall(t, http.MethodPost, direct+"/api/sessions", body); status != http.StatusUnauthorized {
				t.Fatalf("attempt %d: status %d: %s, want the account module's 401", i, status, got)
			}
		}
	})

	t.Run("logins from one address are limited to 5 a minute, refused as the server refuses", func(t *testing.T) {
		// Fresh usernames dodge the per-username throttle, so a 401 means the edge let it pass.
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
			var e struct {
				Code    int32
				Message string
			}
			if err := json.Unmarshal([]byte(got), &e); err != nil || e.Code != code("LOGIN_RATE_LIMITED") || e.Message != "too many login attempts, try again in a minute" {
				t.Fatalf("body %q (err %v), want the server's LOGIN_RATE_LIMITED", got, err)
			}
			return
		}
		t.Fatalf("10 logins from one address passed the edge")
	})

	t.Run("only logging in counts as a login: logging out everywhere still reaches the server", func(t *testing.T) {
		if status, _, got := edgeCall(t, http.MethodDelete, url+"/api/sessions", ""); status != http.StatusUnauthorized {
			t.Fatalf("status %d: %s, want the server's 401 for a missing token", status, got)
		}
	})

	t.Run("every request to the server counts against the address's general limit", func(t *testing.T) {
		for i := range 100 {
			status, _, got := edgeCall(t, http.MethodGet, url+"/api/characters", "")
			switch status {
			case http.StatusUnauthorized:
			case http.StatusTooManyRequests:
				if want := fmt.Sprintf(`{"code":%d,"message":"slow down"}`, code("RATE_LIMITED")); got != want {
					t.Fatalf("body %q, want the server's RATE_LIMITED %s", got, want)
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
