// Package errmap turns the failures services send into what a client
// sees. A service sends its failure across gRPC as a reason (item.v1's
// REASON_INVENTORY_FULL), which pkg/grpcx reads back; For gives its Public
// (page 11's model): an HTTP status, a stable code of the gateway's own
// for every failure (party_full, inventory_full), and a message safe to
// show a player. The message is written here, never taken from the error,
// so a wrapped cause's text cannot reach a client. A failure the table
// does not know is Internal; the caller logs it with its cause. Wrap hands
// a failed call to the use cases as a model.ServiceError, with the kinds
// they act on.
package errmap

import (
	"cmp"
	"errors"
	"net/http"
	"slices"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/pkg/grpcx"
	"finga/services/gateway/internal/model"
)

// Public is a failure as a client sees it (the gateway's model).
type Public = model.Public

// reasons are the failures services in their own processes send across gRPC,
// by reason (pkg/grpcx). TestEveryReasonIsMapped fails when a contract's
// Reason enum has a value with no row here. A service's BAD_ID and
// UNKNOWN_CLASS answer as the gateway's own would (bad_id, unknown_class).
var reasons = map[grpcx.Reason]Public{
	grpcx.Key(characterv1.Reason_REASON_CHARACTER_NOT_FOUND):  {Code: "character_not_found", Status: http.StatusNotFound, Message: "character not found"},
	grpcx.Key(characterv1.Reason_REASON_NAME_TAKEN):           {Code: "name_taken", Status: http.StatusConflict, Message: "that name is taken"},
	grpcx.Key(characterv1.Reason_REASON_BAD_CHARACTER_NAME):   {Code: "bad_character_name", Status: http.StatusBadRequest, Message: "name must be 3 to 12 letters and digits, starting with a letter"},
	grpcx.Key(characterv1.Reason_REASON_CLASS_UNAVAILABLE):    {Code: "class_unavailable", Status: http.StatusBadRequest, Message: "that class is not available"},
	grpcx.Key(characterv1.Reason_REASON_CHARACTER_SLOTS_FULL): {Code: "character_slots_full", Status: http.StatusConflict, Message: "this account already has the most characters allowed"},
	grpcx.Key(characterv1.Reason_REASON_BAD_ID):               model.BadID,
	grpcx.Key(characterv1.Reason_REASON_UNKNOWN_CLASS):        model.UnknownClass,

	grpcx.Key(accountv1.Reason_REASON_ACCOUNT_NOT_FOUND):  {Code: "account_not_found", Status: http.StatusNotFound, Message: "account not found"},
	grpcx.Key(accountv1.Reason_REASON_USERNAME_TAKEN):     {Code: "username_taken", Status: http.StatusConflict, Message: "that username is taken"},
	grpcx.Key(accountv1.Reason_REASON_UNAUTHENTICATED):    model.Unauthenticated,
	grpcx.Key(accountv1.Reason_REASON_LOGIN_RATE_LIMITED): {Code: "login_rate_limited", Status: http.StatusTooManyRequests, Message: "too many login attempts, try again in a minute"},
	grpcx.Key(accountv1.Reason_REASON_BAD_USERNAME):       {Code: "bad_username", Status: http.StatusBadRequest, Message: "username must be 3 to 16 letters, digits or underscores"},
	grpcx.Key(accountv1.Reason_REASON_BAD_PASSWORD):       {Code: "bad_password", Status: http.StatusBadRequest, Message: "password must be 8 to 128 characters"},
	grpcx.Key(accountv1.Reason_REASON_BAD_CONTROL_PRESET): {Code: "bad_control_preset", Status: http.StatusBadRequest, Message: "control preset must be classic or wasd"},
	grpcx.Key(accountv1.Reason_REASON_SESSION_RACE):       {Code: "session_race", Status: http.StatusConflict, Message: "another login is in progress, try again"},

	grpcx.Key(itemv1.Reason_REASON_ITEM_NOT_FOUND):         {Code: "item_not_found", Status: http.StatusNotFound, Message: "item not found"},
	grpcx.Key(itemv1.Reason_REASON_SLOT_TAKEN):             {Code: "slot_taken", Status: http.StatusConflict, Message: "that slot is taken"},
	grpcx.Key(itemv1.Reason_REASON_SLOT_HOLDS_OTHER_ITEM):  {Code: "slot_holds_other_item", Status: http.StatusConflict, Message: "that slot holds a different item"},
	grpcx.Key(itemv1.Reason_REASON_INVENTORY_FULL):         {Code: "inventory_full", Status: http.StatusConflict, Message: "your inventory is full"},
	grpcx.Key(itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED): {Code: "idempotency_key_reused", Status: http.StatusConflict, Message: "that request was already made with other arguments"},
	grpcx.Key(itemv1.Reason_REASON_BAD_QUANTITY):           {Code: "bad_quantity", Status: http.StatusBadRequest, Message: "quantity must be at least 1 and no more than the item holds"},
	grpcx.Key(itemv1.Reason_REASON_BAD_SLOT):               {Code: "bad_slot", Status: http.StatusBadRequest, Message: "slot must be within the tab"},
	grpcx.Key(itemv1.Reason_REASON_WRONG_TAB):              {Code: "wrong_tab", Status: http.StatusBadRequest, Message: "an item stays in its own tab"},
	grpcx.Key(itemv1.Reason_REASON_UNKNOWN_ITEM):           {Code: "unknown_item", Status: http.StatusBadRequest, Message: "unknown item"},
	grpcx.Key(itemv1.Reason_REASON_BAD_ID):                 model.BadID,
	grpcx.Key(itemv1.Reason_REASON_UNKNOWN_CLASS):          model.UnknownClass,

	grpcx.Key(socialv1.Reason_REASON_NOT_PARTY_LEADER):       {Code: "not_party_leader", Status: http.StatusConflict, Message: "only the party leader can do that"},
	grpcx.Key(socialv1.Reason_REASON_PARTY_FULL):             {Code: "party_full", Status: http.StatusConflict, Message: "the party is full"},
	grpcx.Key(socialv1.Reason_REASON_ALREADY_IN_PARTY):       {Code: "already_in_party", Status: http.StatusConflict, Message: "already in a party"},
	grpcx.Key(socialv1.Reason_REASON_NO_PARTY):               {Code: "no_party", Status: http.StatusNotFound, Message: "not in a party"},
	grpcx.Key(socialv1.Reason_REASON_NOT_PARTY_MEMBER):       {Code: "not_party_member", Status: http.StatusNotFound, Message: "not a member of your party"},
	grpcx.Key(socialv1.Reason_REASON_TARGET_NOT_ON_MAP):      {Code: "target_not_on_map", Status: http.StatusNotFound, Message: "that character is not on your map"},
	grpcx.Key(socialv1.Reason_REASON_ALREADY_INVITED):        {Code: "already_invited", Status: http.StatusConflict, Message: "already invited"},
	grpcx.Key(socialv1.Reason_REASON_INVITE_EXPIRED):         {Code: "invite_expired", Status: http.StatusNotFound, Message: "the invite has expired"},
	grpcx.Key(socialv1.Reason_REASON_INVITE_SELF):            {Code: "invite_self", Status: http.StatusBadRequest, Message: "you cannot invite yourself"},
	grpcx.Key(socialv1.Reason_REASON_CHAT_EMPTY):             {Code: "chat_empty", Status: http.StatusBadRequest, Message: "nothing to say"},
	grpcx.Key(socialv1.Reason_REASON_CHAT_TOO_LONG):          {Code: "chat_too_long", Status: http.StatusBadRequest, Message: "a chat line is at most 120 characters"},
	grpcx.Key(socialv1.Reason_REASON_BAD_CHAT_CHANNEL):       {Code: "bad_chat_channel", Status: http.StatusBadRequest, Message: "unknown chat channel"},
	grpcx.Key(socialv1.Reason_REASON_CHAT_NO_PARTY):          {Code: "chat_no_party", Status: http.StatusNotFound, Message: "you have no party"},
	grpcx.Key(socialv1.Reason_REASON_CHAT_RATE_LIMITED):      {Code: "chat_rate_limited", Status: http.StatusTooManyRequests, Message: "slow down"},
	grpcx.Key(socialv1.Reason_REASON_PARTY_CHAT_UNAVAILABLE): {Code: "party_chat_unavailable", Status: http.StatusServiceUnavailable, Message: "party chat is not available"},
	grpcx.Key(socialv1.Reason_REASON_BAD_ID):                 model.BadID,

	grpcx.Key(worldv1.Reason_REASON_PICKUPS_UNAVAILABLE): {Code: "pickups_unavailable", Status: http.StatusServiceUnavailable, Message: "pickups are not available"},
	grpcx.Key(worldv1.Reason_REASON_PICKUP_BUSY):         {Code: "pickup_busy", Status: http.StatusServiceUnavailable, Message: "try again in a moment"},
	grpcx.Key(worldv1.Reason_REASON_BAD_ID):              model.BadID,
}

// internalOnly are reasons that never reach a client as a refusal: the
// gateway closes the connection and logs them (a join into a full or
// stopped instance, a character on a map or of a class the world does not
// have, a join before the gateway's delivery stream is open). For answers
// them Internal; listing them keeps every reason accounted for.
var internalOnly = map[grpcx.Reason]bool{
	grpcx.Key(worldv1.Reason_REASON_BUSY):              true,
	grpcx.Key(worldv1.Reason_REASON_NO_MAP):            true,
	grpcx.Key(worldv1.Reason_REASON_STOPPED):           true,
	grpcx.Key(worldv1.Reason_REASON_NOT_SUBSCRIBED):    true,
	grpcx.Key(worldv1.Reason_REASON_CLASS_UNAVAILABLE): true,
}

// IsInternalOnly reports whether r is a reason the gateway handles itself
// and never shows a client.
func IsInternalOnly(r grpcx.Reason) bool { return internalOnly[r] }

// For is the Public form of a failed call: the row of the reason it
// carries; Internal for a reason the table does not know (or one of
// internalOnly) and for a failure with no reason; the zero value for nil.
func For(err error) Public {
	if err == nil {
		return Public{}
	}
	if r, ok := grpcx.ReasonOf(err); ok {
		if pub, ok := reasons[r]; ok {
			return pub
		}
	}
	return model.Internal
}

// Wrap hands a failed call to the use cases: a model.ServiceError with its
// Public, the kind the gateway acts on (the world not holding this
// gateway's stream yet; a service that cannot be reached), and the cause
// for the logs. nil stays nil.
func Wrap(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*model.ServiceError](err); ok {
		return err
	}
	e := &model.ServiceError{Public: For(err), Err: err}
	if r, ok := grpcx.ReasonOf(err); ok {
		if r == grpcx.Key(worldv1.Reason_REASON_NOT_SUBSCRIBED) {
			e.Kind = model.ErrNotSubscribed
		}
	} else if status.Code(err) == codes.Unavailable {
		e.Kind = model.ErrUnavailable
	}
	return e
}

// Codes is every Public a client can receive, sorted by code; the golden
// file testdata/errors/codes.json is written from it.
func Codes() []Public {
	raised := model.Raised()
	out := make([]Public, 0, len(reasons)+len(raised))
	for _, pub := range reasons {
		out = append(out, pub)
	}
	out = append(out, raised...)
	slices.SortFunc(out, func(a, b Public) int { return cmp.Compare(a.Code, b.Code) })
	// bad_id, unknown_class and unauthenticated each have more than one row.
	return slices.CompactFunc(out, func(a, b Public) bool { return a == b })
}
