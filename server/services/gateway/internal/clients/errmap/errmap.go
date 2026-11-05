// Package errmap turns services' gRPC failure reasons into what a client sees.
package errmap

import (
	"errors"

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

// reasons maps every service failure reason to its Public; TestEveryReasonIsMapped keeps it complete.
var reasons = map[grpcx.Reason]model.Public{
	grpcx.Key(characterv1.Reason_REASON_CHARACTER_NOT_FOUND):  model.CharacterNotFound,
	grpcx.Key(characterv1.Reason_REASON_NAME_TAKEN):           model.NameTaken,
	grpcx.Key(characterv1.Reason_REASON_BAD_CHARACTER_NAME):   model.BadCharacterName,
	grpcx.Key(characterv1.Reason_REASON_CLASS_UNAVAILABLE):    model.ClassUnavailable,
	grpcx.Key(characterv1.Reason_REASON_CHARACTER_SLOTS_FULL): model.CharacterSlotsFull,
	grpcx.Key(characterv1.Reason_REASON_BAD_ID):               model.BadID,
	grpcx.Key(characterv1.Reason_REASON_UNKNOWN_CLASS):        model.UnknownClass,

	grpcx.Key(accountv1.Reason_REASON_ACCOUNT_NOT_FOUND):  model.AccountNotFound,
	grpcx.Key(accountv1.Reason_REASON_USERNAME_TAKEN):     model.UsernameTaken,
	grpcx.Key(accountv1.Reason_REASON_UNAUTHENTICATED):    model.Unauthenticated,
	grpcx.Key(accountv1.Reason_REASON_LOGIN_RATE_LIMITED): model.LoginRateLimited,
	grpcx.Key(accountv1.Reason_REASON_BAD_USERNAME):       model.BadUsername,
	grpcx.Key(accountv1.Reason_REASON_BAD_PASSWORD):       model.BadPassword,
	grpcx.Key(accountv1.Reason_REASON_BAD_CONTROL_PRESET): model.BadControlPreset,
	grpcx.Key(accountv1.Reason_REASON_SESSION_RACE):       model.SessionRace,
	grpcx.Key(accountv1.Reason_REASON_BAD_ID):             model.BadID,
	grpcx.Key(accountv1.Reason_REASON_BUSY):               model.RateLimited,

	grpcx.Key(itemv1.Reason_REASON_ITEM_NOT_FOUND):         model.ItemNotFound,
	grpcx.Key(itemv1.Reason_REASON_SLOT_TAKEN):             model.SlotTaken,
	grpcx.Key(itemv1.Reason_REASON_SLOT_HOLDS_OTHER_ITEM):  model.SlotHoldsOtherItem,
	grpcx.Key(itemv1.Reason_REASON_STACK_FULL):             model.StackFull,
	grpcx.Key(itemv1.Reason_REASON_INVENTORY_FULL):         model.InventoryFull,
	grpcx.Key(itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED): model.IdempotencyKeyReused,
	grpcx.Key(itemv1.Reason_REASON_BAD_IDEMPOTENCY_KEY):    model.BadID,
	grpcx.Key(itemv1.Reason_REASON_BAD_QUANTITY):           model.BadQuantity,
	grpcx.Key(itemv1.Reason_REASON_BAD_SLOT):               model.BadSlot,
	grpcx.Key(itemv1.Reason_REASON_WRONG_TAB):              model.WrongTab,
	grpcx.Key(itemv1.Reason_REASON_UNKNOWN_ITEM):           model.UnknownItem,
	grpcx.Key(itemv1.Reason_REASON_BAD_ID):                 model.BadID,
	grpcx.Key(itemv1.Reason_REASON_UNKNOWN_CLASS):          model.UnknownClass,

	grpcx.Key(socialv1.Reason_REASON_NOT_PARTY_LEADER):       model.NotPartyLeader,
	grpcx.Key(socialv1.Reason_REASON_PARTY_FULL):             model.PartyFull,
	grpcx.Key(socialv1.Reason_REASON_ALREADY_IN_PARTY):       model.AlreadyInParty,
	grpcx.Key(socialv1.Reason_REASON_NO_PARTY):               model.NoParty,
	grpcx.Key(socialv1.Reason_REASON_NOT_PARTY_MEMBER):       model.NotPartyMember,
	grpcx.Key(socialv1.Reason_REASON_TARGET_NOT_ON_MAP):      model.TargetNotOnMap,
	grpcx.Key(socialv1.Reason_REASON_ALREADY_INVITED):        model.AlreadyInvited,
	grpcx.Key(socialv1.Reason_REASON_INVITE_EXPIRED):         model.InviteExpired,
	grpcx.Key(socialv1.Reason_REASON_INVITE_SELF):            model.InviteSelf,
	grpcx.Key(socialv1.Reason_REASON_CHAT_EMPTY):             model.ChatEmpty,
	grpcx.Key(socialv1.Reason_REASON_CHAT_TOO_LONG):          model.ChatTooLong,
	grpcx.Key(socialv1.Reason_REASON_BAD_CHAT_CHANNEL):       model.BadChatChannel,
	grpcx.Key(socialv1.Reason_REASON_CHAT_NO_PARTY):          model.ChatNoParty,
	grpcx.Key(socialv1.Reason_REASON_CHAT_RATE_LIMITED):      model.ChatRateLimited,
	grpcx.Key(socialv1.Reason_REASON_PARTY_CHAT_UNAVAILABLE): model.PartyChatUnavailable,
	grpcx.Key(socialv1.Reason_REASON_BAD_ID):                 model.BadID,

	grpcx.Key(worldv1.Reason_REASON_PICKUPS_UNAVAILABLE): model.PickupsUnavailable,
	grpcx.Key(worldv1.Reason_REASON_PICKUP_BUSY):         model.PickupBusy,
	grpcx.Key(worldv1.Reason_REASON_BAD_ID):              model.BadID,
}

// For is the Public form of a failed call: its reason's row, else Internal; the zero value for nil.
func For(err error) model.Public {
	if err == nil {
		return model.Public{}
	}
	if r, ok := grpcx.ReasonOf(err); ok {
		if pub, ok := reasons[r]; ok {
			return pub
		}
	}
	return model.Internal
}

// Wrap turns a failed call into a model.ServiceError with its Public and kind; nil stays nil.
func Wrap(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*model.ServiceError](err); ok {
		return err
	}
	e := &model.ServiceError{Public: For(err), Err: err}
	if r, ok := grpcx.ReasonOf(err); ok && r == grpcx.Key(worldv1.Reason_REASON_NOT_SUBSCRIBED) {
		e.Kind = model.ErrNotSubscribed
	} else if status.Code(err) == codes.Unavailable {
		e.Kind = model.ErrUnavailable // with a reason too: a busy or stopping world is worth waiting for
	}
	return e
}
