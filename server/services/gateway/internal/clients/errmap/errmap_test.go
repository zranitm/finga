package errmap_test

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
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
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/services/gateway/internal/clients/errmap"
	"finga/services/gateway/internal/model"
)

func TestForGivesEachServiceErrorItsOwnCode(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{grpctest.Error(codes.AlreadyExists, accountv1.Reason_REASON_USERNAME_TAKEN), http.StatusConflict, "username_taken"},
		{grpctest.Error(codes.Unauthenticated, accountv1.Reason_REASON_UNAUTHENTICATED), http.StatusUnauthorized, "unauthenticated"},
		{grpctest.Error(codes.ResourceExhausted, accountv1.Reason_REASON_LOGIN_RATE_LIMITED), http.StatusTooManyRequests, "login_rate_limited"},
		{grpctest.Error(codes.InvalidArgument, accountv1.Reason_REASON_BAD_PASSWORD), http.StatusBadRequest, "bad_password"},
		{grpctest.Error(codes.Aborted, accountv1.Reason_REASON_SESSION_RACE), http.StatusConflict, "session_race"},
		{grpctest.Error(codes.AlreadyExists, characterv1.Reason_REASON_NAME_TAKEN), http.StatusConflict, "name_taken"},
		{grpctest.Error(codes.FailedPrecondition, characterv1.Reason_REASON_CHARACTER_SLOTS_FULL), http.StatusConflict, "character_slots_full"},
		{grpctest.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND), http.StatusNotFound, "character_not_found"},
		{grpctest.Error(codes.ResourceExhausted, socialv1.Reason_REASON_CHAT_RATE_LIMITED), http.StatusTooManyRequests, "chat_rate_limited"},
		{grpctest.Error(codes.Unavailable, socialv1.Reason_REASON_PARTY_CHAT_UNAVAILABLE), http.StatusServiceUnavailable, "party_chat_unavailable"},
		{grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_INVENTORY_FULL), http.StatusConflict, "inventory_full"},
		{grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED), http.StatusConflict, "idempotency_key_reused"},
		{fmt.Errorf("pickup: %w", grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_SLOT_HOLDS_OTHER_ITEM)), http.StatusConflict, "slot_holds_other_item"},
		{grpctest.Error(codes.InvalidArgument, itemv1.Reason_REASON_BAD_ID), http.StatusBadRequest, "bad_id"},
		{grpctest.Error(codes.FailedPrecondition, socialv1.Reason_REASON_PARTY_FULL), http.StatusConflict, "party_full"},
		{grpctest.Error(codes.FailedPrecondition, socialv1.Reason_REASON_NOT_PARTY_LEADER), http.StatusConflict, "not_party_leader"},
		{grpctest.Error(codes.NotFound, socialv1.Reason_REASON_INVITE_EXPIRED), http.StatusNotFound, "invite_expired"},
		{grpctest.Error(codes.Unavailable, worldv1.Reason_REASON_PICKUPS_UNAVAILABLE), http.StatusServiceUnavailable, "pickups_unavailable"},
		{grpctest.Error(codes.ResourceExhausted, worldv1.Reason_REASON_PICKUP_BUSY), http.StatusServiceUnavailable, "pickup_busy"},
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
	err := fmt.Errorf("create character: %w", fmt.Errorf("%w: %w", grpctest.Error(codes.AlreadyExists, characterv1.Reason_REASON_NAME_TAKEN), cause))
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
	if got != model.Internal {
		t.Fatalf("For(unknown) = %+v, want %+v", got, model.Internal)
	}
	if got.Status != http.StatusInternalServerError || strings.Contains(got.Message, "10.0.0.5") {
		t.Fatalf("Internal = %+v", got)
	}
	if got := errmap.For(nil); got != (errmap.Public{}) {
		t.Fatalf("For(nil) = %+v, want the zero value", got)
	}
	if model.IsInternal(errmap.For(grpctest.Error(codes.InvalidArgument, itemv1.Reason_REASON_BAD_SLOT))) || !model.IsInternal(errmap.For(errors.New("boom"))) {
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
			if got := errmap.For(tt.err); got != model.Internal {
				t.Fatalf("For = %+v", got)
			}
		})
	}
}

// contracts are the services' reason enums; every value needs a row, or a client sees internal.
var contracts = []protoreflect.EnumDescriptor{
	accountv1.Reason(0).Descriptor(),
	characterv1.Reason(0).Descriptor(),
	itemv1.Reason(0).Descriptor(),
	socialv1.Reason(0).Descriptor(),
	worldv1.Reason(0).Descriptor(),
}

// handledByTheGateway are the reasons the gateway acts on itself by closing the connection; a client sees internal.
var handledByTheGateway = []grpcx.Reason{
	grpcx.Key(worldv1.Reason_REASON_BUSY),
	grpcx.Key(worldv1.Reason_REASON_NO_MAP),
	grpcx.Key(worldv1.Reason_REASON_STOPPED),
	grpcx.Key(worldv1.Reason_REASON_NOT_SUBSCRIBED),
	grpcx.Key(worldv1.Reason_REASON_CLASS_UNAVAILABLE),
}

func TestEveryReasonIsMapped(t *testing.T) {
	for _, d := range contracts {
		for _, r := range grpctest.Reasons(d) {
			internal := model.IsInternal(errmap.For(grpcx.ErrorFor(codes.Unknown, r)))
			if handled := slices.Contains(handledByTheGateway, r); internal != handled {
				t.Errorf("%s: internal %v, handled by the gateway %v", r, internal, handled)
			}
		}
	}
}

func TestWrapNamesWhatTheUseCasesActOn(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind error // nil: none
		code string
	}{
		{"a reason is its Public", grpctest.Error(codes.FailedPrecondition, socialv1.Reason_REASON_PARTY_FULL), nil, "party_full"},
		{"the world not holding the stream", grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_NOT_SUBSCRIBED), model.ErrNotSubscribed, "internal"},
		{"a service that cannot be reached", status.Error(codes.Unavailable, "connection refused"), model.ErrUnavailable, "internal"},
		{"a failure with no reason", status.Error(codes.Internal, "boom"), nil, "internal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errmap.Wrap(tt.err)
			if got := model.PublicOf(err); got.Code != tt.code {
				t.Fatalf("PublicOf = %+v, want %s", got, tt.code)
			}
			for _, kind := range []error{model.ErrNotSubscribed, model.ErrUnavailable} {
				if errors.Is(err, kind) != errors.Is(tt.kind, kind) {
					t.Fatalf("errors.Is(%v) = %v", kind, errors.Is(err, kind))
				}
			}
			if !errors.Is(err, tt.err) {
				t.Fatal("the cause is lost")
			}
			var first, again *model.ServiceError
			if !errors.As(err, &first) || !errors.As(errmap.Wrap(err), &again) || first != again {
				t.Fatal("a wrapped failure was wrapped again")
			}
		})
	}
	if errmap.Wrap(nil) != nil {
		t.Fatal("Wrap(nil) is not nil")
	}
}
