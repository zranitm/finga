package transport_test

import (
	"context"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/account"
	"finga/internal/application/character"
	model "finga/internal/model/gateway"
	"finga/internal/shared/kernel"
	"finga/pkg/ratelimit"
)

// limited is a restHarness whose accounts get burst calls, refilled perSecond, listing their characters.
func limited(perSecond float64, burst int) *restHarness {
	h := newLimitedHarness(ratelimit.NewBuckets(perSecond, burst))
	h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) { return nil, nil }
	return h
}

func TestAnAccountPastItsBurstIsRateLimitedTillItsBucketRefills(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := limited(2, 3)
		for k := range 3 {
			if rec := h.do(http.MethodGet, "/api/characters", "", authed()); rec.Code != http.StatusOK {
				t.Fatalf("call %d answered %d, want 200 within the burst", k+1, rec.Code)
			}
		}
		assertError(t, h.do(http.MethodGet, "/api/characters", "", authed()), http.StatusTooManyRequests, model.RateLimited)
		time.Sleep(500 * time.Millisecond) // a token at 2 a second
		if rec := h.do(http.MethodGet, "/api/characters", "", authed()); rec.Code != http.StatusOK {
			t.Fatalf("after the refill answered %d, want 200", rec.Code)
		}
		assertError(t, h.do(http.MethodPut, "/api/settings", `{}`, authed()), http.StatusTooManyRequests, model.RateLimited) // every route draws on the one bucket
	})
}

func TestTwoAccountsDoNotShareABucket(t *testing.T) {
	h := limited(2, 1)
	other := account.Principal{AccountID: kernel.NewAccountID(), SessionID: kernel.NewSessionID()}
	h.accounts.validate = func(token string) (account.Principal, error) {
		if token == "tok-other-0123456789abcdef" {
			return other, nil
		}
		return principal, nil
	}
	if rec := h.do(http.MethodGet, "/api/characters", "", authed()); rec.Code != http.StatusOK {
		t.Fatalf("the first account answered %d, want 200", rec.Code)
	}
	assertError(t, h.do(http.MethodGet, "/api/characters", "", authed()), http.StatusTooManyRequests, model.RateLimited)
	if rec := h.do(http.MethodGet, "/api/characters", "", map[string]string{"Authorization": "Bearer tok-other-0123456789abcdef"}); rec.Code != http.StatusOK {
		t.Fatalf("the other account answered %d past the first's limit, want 200", rec.Code)
	}
}

func TestARequestWithoutAValidTokenIsRefusedBeforeTheLimit(t *testing.T) {
	h := limited(2, 1)
	for range 3 {
		assertError(t, h.do(http.MethodGet, "/api/characters", "", map[string]string{"Authorization": "Bearer tok-bad"}), http.StatusUnauthorized, model.Unauthenticated)
	}
	if rec := h.do(http.MethodGet, "/api/characters", "", authed()); rec.Code != http.StatusOK {
		t.Fatalf("the account answered %d after refused tokens, want 200: they draw on no bucket", rec.Code)
	}
}
