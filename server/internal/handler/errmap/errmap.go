// Package errmap turns the errors services return into what a client sees.
// Services return plain sentinel errors that name the exact failure
// (party.ErrFull, character.ErrNameTaken) and wrap a cause with %w for the
// logs. A service that runs as its own process (Low-level design, page 10)
// sends its failure across gRPC as a reason instead (item.v1's
// REASON_INVENTORY_FULL), which pkg/grpcx reads back. Handlers, HTTP and
// WebSocket alike, pass the error to For and get a Public: an HTTP status,
// a stable code of its own for every failure (party_full, inventory_full),
// and a message safe to show a player. The message is written here, never
// taken from the error, so a wrapped cause's text cannot reach a client. An
// error the tables do not know is Internal; the handler logs it with its
// cause.
package errmap

import (
	"cmp"
	"errors"
	"net/http"
	"slices"

	accountv1 "finga/api/gen/account/v1"
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/application/character"
	"finga/internal/application/chat"
	"finga/internal/application/party"
	"finga/internal/application/world"
	"finga/internal/model"
	"finga/pkg/grpcx"
)

// Public is an error as a client sees it. Status is for HTTP; the WebSocket
// sends only Code and Message in a Response.
type Public struct {
	Code    string `json:"code"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// The failures a handler raises itself, before or around a service call.
var (
	MalformedBody        = Public{"malformed_body", http.StatusBadRequest, "malformed request body"}
	MissingField         = Public{"missing_field", http.StatusBadRequest, "missing a required field"}
	BodyTooLarge         = Public{"body_too_large", http.StatusRequestEntityTooLarge, "request body too large"}
	NoSuchEndpoint       = Public{"no_such_endpoint", http.StatusNotFound, "no such endpoint"}
	MethodNotAllowed     = Public{"method_not_allowed", http.StatusMethodNotAllowed, "method not allowed"}
	RateLimited          = Public{"rate_limited", http.StatusTooManyRequests, "slow down"}
	NotImplemented       = Public{"not_implemented", http.StatusNotImplemented, "not available yet"}
	ChatUnavailable      = Public{"chat_unavailable", http.StatusServiceUnavailable, "chat is not available"}
	PartiesUnavailable   = Public{"parties_unavailable", http.StatusServiceUnavailable, "parties are not available"}
	InventoryUnavailable = Public{"inventory_unavailable", http.StatusServiceUnavailable, "the inventory is not available"}
	// Unauthenticated is a missing or malformed token, raised by the handler,
	// and the account service's REASON_UNAUTHENTICATED, which For maps to it.
	Unauthenticated = Public{"unauthenticated", http.StatusUnauthorized, "invalid credentials or session"}
	Internal        = Public{"internal", http.StatusInternalServerError, "something went wrong"}
)

var handlerRaised = []Public{
	MalformedBody, MissingField, BodyTooLarge, NoSuchEndpoint, MethodNotAllowed, RateLimited,
	NotImplemented, ChatUnavailable, PartiesUnavailable, InventoryUnavailable, Unauthenticated, Internal,
}

// table is every service error a client can meet. TestEveryServiceErrorIsMapped
// fails when a module declares an Err variable that has no row here.
var table = []struct {
	err error
	pub Public
}{
	{character.ErrNotFound, Public{"character_not_found", http.StatusNotFound, "character not found"}},
	{character.ErrNameTaken, Public{"name_taken", http.StatusConflict, "that name is taken"}},
	{character.ErrBadName, Public{"bad_character_name", http.StatusBadRequest, "name must be 3 to 12 letters and digits, starting with a letter"}},
	{character.ErrClassUnavailable, Public{"class_unavailable", http.StatusBadRequest, "that class is not available"}},
	{character.ErrSlotsFull, Public{"character_slots_full", http.StatusConflict, "this account already has the most characters allowed"}},

	{chat.ErrEmpty, Public{"chat_empty", http.StatusBadRequest, "nothing to say"}},
	{chat.ErrTooLong, Public{"chat_too_long", http.StatusBadRequest, "a chat line is at most 120 characters"}},
	{chat.ErrChannel, Public{"bad_chat_channel", http.StatusBadRequest, "unknown chat channel"}},
	{chat.ErrNoParty, Public{"chat_no_party", http.StatusNotFound, "you have no party"}},
	{chat.ErrRate, Public{"chat_rate_limited", http.StatusTooManyRequests, "slow down"}},
	{chat.ErrNoParties, Public{"party_chat_unavailable", http.StatusServiceUnavailable, "party chat is not available"}},

	{party.ErrNotLeader, Public{"not_party_leader", http.StatusConflict, "only the party leader can do that"}},
	{party.ErrFull, Public{"party_full", http.StatusConflict, "the party is full"}},
	{party.ErrInParty, Public{"already_in_party", http.StatusConflict, "already in a party"}},
	{party.ErrNoParty, Public{"no_party", http.StatusNotFound, "not in a party"}},
	{party.ErrNotMember, Public{"not_party_member", http.StatusNotFound, "not a member of your party"}},
	{party.ErrNotHere, Public{"target_not_on_map", http.StatusNotFound, "that character is not on your map"}},
	{party.ErrAlreadyAsked, Public{"already_invited", http.StatusConflict, "already invited"}},
	{party.ErrInviteExpired, Public{"invite_expired", http.StatusNotFound, "the invite has expired"}},
	{party.ErrSelf, Public{"invite_self", http.StatusBadRequest, "you cannot invite yourself"}},

	{world.ErrPickupsUnavailable, Public{"pickups_unavailable", http.StatusServiceUnavailable, "pickups are not available"}},
	{world.ErrPickupBusy, Public{"pickup_busy", http.StatusServiceUnavailable, "try again in a moment"}},

	{model.ErrBadID, badID},
	{model.ErrUnknownClass, unknownClass},
}

// reasons are the failures services in their own processes send across gRPC,
// by reason (pkg/grpcx). TestEveryReasonIsMapped fails when a contract's
// Reason enum has a value with no row here. A service's BAD_ID and
// UNKNOWN_CLASS answer as the gateway's own would (bad_id, unknown_class).
var reasons = map[grpcx.Reason]Public{
	grpcx.Key(accountv1.Reason_REASON_ACCOUNT_NOT_FOUND):  {"account_not_found", http.StatusNotFound, "account not found"},
	grpcx.Key(accountv1.Reason_REASON_USERNAME_TAKEN):     {"username_taken", http.StatusConflict, "that username is taken"},
	grpcx.Key(accountv1.Reason_REASON_UNAUTHENTICATED):    Unauthenticated,
	grpcx.Key(accountv1.Reason_REASON_LOGIN_RATE_LIMITED): {"login_rate_limited", http.StatusTooManyRequests, "too many login attempts, try again in a minute"},
	grpcx.Key(accountv1.Reason_REASON_BAD_USERNAME):       {"bad_username", http.StatusBadRequest, "username must be 3 to 16 letters, digits or underscores"},
	grpcx.Key(accountv1.Reason_REASON_BAD_PASSWORD):       {"bad_password", http.StatusBadRequest, "password must be 8 to 128 characters"},
	grpcx.Key(accountv1.Reason_REASON_BAD_CONTROL_PRESET): {"bad_control_preset", http.StatusBadRequest, "control preset must be classic or wasd"},
	grpcx.Key(accountv1.Reason_REASON_SESSION_RACE):       {"session_race", http.StatusConflict, "another login is in progress, try again"},

	grpcx.Key(itemv1.Reason_REASON_ITEM_NOT_FOUND):         {"item_not_found", http.StatusNotFound, "item not found"},
	grpcx.Key(itemv1.Reason_REASON_SLOT_TAKEN):             {"slot_taken", http.StatusConflict, "that slot is taken"},
	grpcx.Key(itemv1.Reason_REASON_SLOT_HOLDS_OTHER_ITEM):  {"slot_holds_other_item", http.StatusConflict, "that slot holds a different item"},
	grpcx.Key(itemv1.Reason_REASON_INVENTORY_FULL):         {"inventory_full", http.StatusConflict, "your inventory is full"},
	grpcx.Key(itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED): {"idempotency_key_reused", http.StatusConflict, "that request was already made with other arguments"},
	grpcx.Key(itemv1.Reason_REASON_BAD_QUANTITY):           {"bad_quantity", http.StatusBadRequest, "quantity must be at least 1 and no more than the item holds"},
	grpcx.Key(itemv1.Reason_REASON_BAD_SLOT):               {"bad_slot", http.StatusBadRequest, "slot must be within the tab"},
	grpcx.Key(itemv1.Reason_REASON_WRONG_TAB):              {"wrong_tab", http.StatusBadRequest, "an item stays in its own tab"},
	grpcx.Key(itemv1.Reason_REASON_UNKNOWN_ITEM):           {"unknown_item", http.StatusBadRequest, "unknown item"},
	grpcx.Key(itemv1.Reason_REASON_BAD_ID):                 badID,
	grpcx.Key(itemv1.Reason_REASON_UNKNOWN_CLASS):          unknownClass,
}

// The two shared by the gateway's own parsing and the services'.
var (
	badID        = Public{"bad_id", http.StatusBadRequest, "malformed ID"}
	unknownClass = Public{"unknown_class", http.StatusBadRequest, "unknown class"}
)

// internalOnly are service errors that never reach a client as a refusal:
// the gateway closes the connection and logs them (a join into a full or
// stopped instance, a character on a map that does not exist). Listing them
// here keeps every exported error accounted for; For answers them Internal.
var internalOnly = []error{world.ErrBusy, world.ErrNoMap, world.ErrStopped}

// For is the Public form of err: the row of the first table error found in
// err's chain, or the row of the reason a remote failure carries; Internal
// for an error neither knows (or one of internalOnly), and the zero value
// for nil.
func For(err error) Public {
	if err == nil {
		return Public{}
	}
	for _, e := range internalOnly {
		if errors.Is(err, e) {
			return Internal
		}
	}
	for _, row := range table {
		if errors.Is(err, row.err) {
			return row.pub
		}
	}
	if r, ok := grpcx.ReasonOf(err); ok {
		if pub, ok := reasons[r]; ok {
			return pub
		}
	}
	return Internal
}

// IsInternal reports whether p is the answer to an error nobody expected,
// which the handler must log with its cause.
func IsInternal(p Public) bool {
	return p.Code == Internal.Code
}

// Codes is every Public a client can receive, sorted by code; the golden
// file testdata/errors/codes.json is written from it.
func Codes() []Public {
	out := make([]Public, 0, len(table)+len(reasons)+len(handlerRaised))
	for _, row := range table {
		out = append(out, row.pub)
	}
	for _, pub := range reasons {
		out = append(out, pub)
	}
	out = append(out, handlerRaised...)
	slices.SortFunc(out, func(a, b Public) int { return cmp.Compare(a.Code, b.Code) })
	// bad_id, unknown_class and unauthenticated each have more than one row.
	return slices.CompactFunc(out, func(a, b Public) bool { return a == b })
}
