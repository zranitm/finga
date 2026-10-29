package errmap_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/sentinels"
	"finga/services/gateway/internal/handler/errmap"
)

var update = flag.Bool("update", false, "rewrite testdata/errors/codes.json")

// codesFile is the list the client checks its own code list against.
var codesFile = filepath.Join("..", "..", "..", "..", "..", "testdata", "errors", "codes.json")

func TestForGivesEachServiceErrorItsOwnCode(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{grpcx.Error(codes.AlreadyExists, accountv1.Reason_REASON_USERNAME_TAKEN), http.StatusConflict, "username_taken"},
		{grpcx.Error(codes.Unauthenticated, accountv1.Reason_REASON_UNAUTHENTICATED), http.StatusUnauthorized, "unauthenticated"},
		{grpcx.Error(codes.ResourceExhausted, accountv1.Reason_REASON_LOGIN_RATE_LIMITED), http.StatusTooManyRequests, "login_rate_limited"},
		{grpcx.Error(codes.InvalidArgument, accountv1.Reason_REASON_BAD_PASSWORD), http.StatusBadRequest, "bad_password"},
		{grpcx.Error(codes.Aborted, accountv1.Reason_REASON_SESSION_RACE), http.StatusConflict, "session_race"},
		{grpcx.Error(codes.AlreadyExists, characterv1.Reason_REASON_NAME_TAKEN), http.StatusConflict, "name_taken"},
		{grpcx.Error(codes.FailedPrecondition, characterv1.Reason_REASON_CHARACTER_SLOTS_FULL), http.StatusConflict, "character_slots_full"},
		{grpcx.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND), http.StatusNotFound, "character_not_found"},
		{grpcx.Error(codes.ResourceExhausted, socialv1.Reason_REASON_CHAT_RATE_LIMITED), http.StatusTooManyRequests, "chat_rate_limited"},
		{grpcx.Error(codes.Unavailable, socialv1.Reason_REASON_PARTY_CHAT_UNAVAILABLE), http.StatusServiceUnavailable, "party_chat_unavailable"},
		{grpcx.Error(codes.FailedPrecondition, itemv1.Reason_REASON_INVENTORY_FULL), http.StatusConflict, "inventory_full"},
		{grpcx.Error(codes.FailedPrecondition, itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED), http.StatusConflict, "idempotency_key_reused"},
		{fmt.Errorf("pickup: %w", grpcx.Error(codes.FailedPrecondition, itemv1.Reason_REASON_SLOT_HOLDS_OTHER_ITEM)), http.StatusConflict, "slot_holds_other_item"},
		{grpcx.Error(codes.InvalidArgument, itemv1.Reason_REASON_BAD_ID), http.StatusBadRequest, "bad_id"},
		{grpcx.Error(codes.FailedPrecondition, socialv1.Reason_REASON_PARTY_FULL), http.StatusConflict, "party_full"},
		{grpcx.Error(codes.FailedPrecondition, socialv1.Reason_REASON_NOT_PARTY_LEADER), http.StatusConflict, "not_party_leader"},
		{grpcx.Error(codes.NotFound, socialv1.Reason_REASON_INVITE_EXPIRED), http.StatusNotFound, "invite_expired"},
		{kernel.ErrBadID, http.StatusBadRequest, "bad_id"},
		{grpcx.Error(codes.Unavailable, worldv1.Reason_REASON_PICKUPS_UNAVAILABLE), http.StatusServiceUnavailable, "pickups_unavailable"},
		{grpcx.Error(codes.ResourceExhausted, worldv1.Reason_REASON_PICKUP_BUSY), http.StatusServiceUnavailable, "pickup_busy"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := errmap.For(tt.err)
			if got.Status != tt.status || got.Code != tt.code || got.Message == "" {
				t.Fatalf("For(%v) = %+v, want status %d, code %s and a message", tt.err, got, tt.status, tt.code)
			}
		})
	}
}

func TestForFindsTheErrorThroughWrappingAndNeverShowsTheCause(t *testing.T) {
	cause := errors.New(`duplicate key value violates unique constraint "characters_name_key"`)
	err := fmt.Errorf("create character: %w", fmt.Errorf("%w: %w", grpcx.Error(codes.AlreadyExists, characterv1.Reason_REASON_NAME_TAKEN), cause))
	got := errmap.For(err)
	if got.Code != "name_taken" || got.Status != http.StatusConflict {
		t.Fatalf("For = %+v, want name_taken 409", got)
	}
	if strings.Contains(got.Message, "duplicate") || strings.Contains(got.Message, "characters_name_key") {
		t.Fatalf("the cause leaked into the message: %q", got.Message)
	}
}

func TestForTurnsAnUnknownErrorIntoInternalWithoutItsText(t *testing.T) {
	got := errmap.For(errors.New("dial tcp 10.0.0.5:5432: connection refused"))
	if got != errmap.Internal {
		t.Fatalf("For(unknown) = %+v, want %+v", got, errmap.Internal)
	}
	if got.Status != http.StatusInternalServerError || strings.Contains(got.Message, "10.0.0.5") {
		t.Fatalf("Internal = %+v", got)
	}
	if got := errmap.For(nil); got != (errmap.Public{}) {
		t.Fatalf("For(nil) = %+v, want the zero value", got)
	}
	if errmap.IsInternal(errmap.For(grpcx.Error(codes.InvalidArgument, itemv1.Reason_REASON_BAD_SLOT))) || !errmap.IsInternal(errmap.For(errors.New("boom"))) {
		t.Fatal("IsInternal disagrees with For")
	}
}

func TestARemoteFailureWithoutAKnownReasonIsInternal(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"no reason (the service could not explain it)", status.Error(codes.Internal, "internal error")},
		{"unavailable", status.Error(codes.Unavailable, "connection refused")},
		{"a reason no table maps", grpcx.ErrorFor(codes.FailedPrecondition, grpcx.Reason{Domain: "internal.shop.v1", Name: "REASON_CLOSED"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errmap.For(tt.err); got != errmap.Internal {
				t.Fatalf("For = %+v", got)
			}
		})
	}
}

// contracts are the reason enums of the services that run in their own
// processes; every value must have a row, or a client would see internal.
var contracts = []protoreflect.EnumDescriptor{
	accountv1.Reason(0).Descriptor(),
	characterv1.Reason(0).Descriptor(),
	itemv1.Reason(0).Descriptor(),
	socialv1.Reason(0).Descriptor(),
	worldv1.Reason(0).Descriptor(),
}

// TestEveryReasonIsMapped: every reason has a row, or is one the gateway
// handles itself, which For answers Internal.
func TestEveryReasonIsMapped(t *testing.T) {
	for _, d := range contracts {
		for _, r := range grpcx.Reasons(d) {
			internal := errmap.IsInternal(errmap.For(grpcx.ErrorFor(codes.Unknown, r)))
			if internal != errmap.IsInternalOnly(r) {
				t.Errorf("%s: internal %v, listed as internal only %v", r, internal, errmap.IsInternalOnly(r))
			}
		}
	}
}

// TestEveryServiceErrorIsMapped reads the source of model, whose errors the
// gateway's own parsing raises: an exported Err variable the table leaves
// out would reach a player as "something went wrong" with code internal.
// Every module runs in a service now; each is checked by its own handler's
// guard and TestEveryReasonIsMapped.
func TestEveryServiceErrorIsMapped(t *testing.T) {
	declared, err := sentinels.Declared(filepath.Join("..", "..", "..", "..", "..", "internal", "kernel"))
	if err != nil || len(declared) == 0 {
		t.Fatalf("declared %v, err %v: is the walk broken?", declared, err)
	}
	mapped, err := sentinels.Referenced("errmap.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range declared {
		if !mapped[name] {
			t.Errorf("%s has no row in the table", name)
		}
	}
}

func TestCodesAreUniqueSnakeCaseAndMatchTheGoldenList(t *testing.T) {
	codes := errmap.Codes()
	snake := regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`)
	seen := map[string]bool{}
	for _, p := range codes {
		if !snake.MatchString(p.Code) {
			t.Errorf("code %q is not snake_case", p.Code)
		}
		if seen[p.Code] {
			t.Errorf("code %q is used twice; every failure gets its own", p.Code)
		}
		seen[p.Code] = true
		if p.Status < 400 || p.Status > 599 || p.Message == "" {
			t.Errorf("%+v needs an error status and a message", p)
		}
	}
	got, err := json.MarshalIndent(codes, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *update {
		if err := os.MkdirAll(filepath.Dir(codesFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(codesFile, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(codesFile)
	if err != nil {
		t.Fatalf("%v (run go test ./services/gateway/internal/handler/errmap -update)", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("%s is stale; run go test ./services/gateway/internal/handler/errmap -update", codesFile)
	}
}
