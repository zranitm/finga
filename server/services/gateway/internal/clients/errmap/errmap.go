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
	grpcx.Key(characterv1.Reason_REASON_BAD_SKILL_BAR):        model.BadSkillBar,

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
	grpcx.Key(itemv1.Reason_REASON_EQUIP_TAB_FULL):         model.EquipTabFull,
	grpcx.Key(itemv1.Reason_REASON_USE_TAB_FULL):           model.UseTabFull,
	grpcx.Key(itemv1.Reason_REASON_ETC_TAB_FULL):           model.EtcTabFull,
	grpcx.Key(itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED): model.IdempotencyKeyReused,
	grpcx.Key(itemv1.Reason_REASON_BAD_IDEMPOTENCY_KEY):    model.BadID,
	grpcx.Key(itemv1.Reason_REASON_BAD_QUANTITY):           model.BadQuantity,
	grpcx.Key(itemv1.Reason_REASON_BAD_SLOT):               model.BadSlot,
	grpcx.Key(itemv1.Reason_REASON_WRONG_TAB):              model.WrongTab,
	grpcx.Key(itemv1.Reason_REASON_UNKNOWN_ITEM):           model.UnknownItem,
	grpcx.Key(itemv1.Reason_REASON_BAD_ID):                 model.BadID,
	grpcx.Key(itemv1.Reason_REASON_UNKNOWN_CLASS):          model.UnknownClass,
	grpcx.Key(itemv1.Reason_REASON_ITEM_WORN):              model.ItemWorn,
	grpcx.Key(itemv1.Reason_REASON_LEVEL_TOO_LOW):          model.LevelTooLow,
	grpcx.Key(itemv1.Reason_REASON_STR_TOO_LOW):            model.STRTooLow,
	grpcx.Key(itemv1.Reason_REASON_DEX_TOO_LOW):            model.DEXTooLow,
	grpcx.Key(itemv1.Reason_REASON_INT_TOO_LOW):            model.INTTooLow,
	grpcx.Key(itemv1.Reason_REASON_AGI_TOO_LOW):            model.AGITooLow,
	grpcx.Key(itemv1.Reason_REASON_VIT_TOO_LOW):            model.VITTooLow,
	grpcx.Key(itemv1.Reason_REASON_WRONG_CLASS):            model.WrongClass,
	grpcx.Key(itemv1.Reason_REASON_WRONG_EQUIP_SLOT):       model.WrongEquipSlot,
	grpcx.Key(itemv1.Reason_REASON_NOT_EQUIPMENT):          model.NotEquipment,
	grpcx.Key(itemv1.Reason_REASON_NOTHING_WORN):           model.NothingWorn,
	grpcx.Key(itemv1.Reason_REASON_NOT_ENOUGH_ITEMS):       model.NotEnoughItems,
	grpcx.Key(itemv1.Reason_REASON_NOT_USABLE):             model.NotUsable,

	grpcx.Key(socialv1.Reason_REASON_NOT_PARTY_LEADER):       model.NotPartyLeader,
	grpcx.Key(socialv1.Reason_REASON_PARTY_FULL):             model.PartyFull,
	grpcx.Key(socialv1.Reason_REASON_ALREADY_IN_PARTY):       model.AlreadyInParty,
	grpcx.Key(socialv1.Reason_REASON_TARGET_IN_PARTY):        model.TargetInParty,
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

	grpcx.Key(worldv1.Reason_REASON_BAD_ID):                     model.BadID,
	grpcx.Key(worldv1.Reason_REASON_CHARACTER_DEAD):             model.CharacterDead,
	grpcx.Key(worldv1.Reason_REASON_POINTS_CHANGED):             model.PointsChanged,
	grpcx.Key(worldv1.Reason_REASON_NOT_ENOUGH_POINTS):          model.NotEnoughPoints,
	grpcx.Key(worldv1.Reason_REASON_NO_FREE_RESET):              model.NoFreeReset,
	grpcx.Key(worldv1.Reason_REASON_BAD_POINTS):                 model.BadPoints,
	grpcx.Key(worldv1.Reason_REASON_SKILL_POINTS_CHANGED):       model.SkillPointsChanged,
	grpcx.Key(worldv1.Reason_REASON_NOT_ENOUGH_SKILL_POINTS):    model.NotEnoughSkillPoints,
	grpcx.Key(worldv1.Reason_REASON_SKILL_NOT_LEARNABLE):        model.SkillNotLearnable,
	grpcx.Key(worldv1.Reason_REASON_BAD_SKILL_POINTS):           model.BadSkillPoints,
	grpcx.Key(worldv1.Reason_REASON_SKILL_NOT_LEARNED):          model.SkillNotLearned,
	grpcx.Key(worldv1.Reason_REASON_SKILL_PASSIVE):              model.SkillPassive,
	grpcx.Key(worldv1.Reason_REASON_NOT_IN_TOWN):                model.NotInTown,
	grpcx.Key(worldv1.Reason_REASON_SKILL_COOLING_DOWN):         model.SkillCoolingDown,
	grpcx.Key(worldv1.Reason_REASON_NOT_ENOUGH_MP):              model.NotEnoughMP,
	grpcx.Key(worldv1.Reason_REASON_SKILL_WAITING):              model.SkillWaiting,
	grpcx.Key(worldv1.Reason_REASON_PROMOTION_CONDITIONS_UNMET): model.PromotionUnmet,
	grpcx.Key(worldv1.Reason_REASON_NOT_YOUR_NEXT_CLASS):        model.NotYourNextClass,
	grpcx.Key(worldv1.Reason_REASON_NPC_OUT_OF_REACH):           model.NpcOutOfReach,
	grpcx.Key(worldv1.Reason_REASON_QUEST_UNAVAILABLE):          model.QuestUnavailable,
	grpcx.Key(worldv1.Reason_REASON_QUEST_CONDITIONS_UNMET):     model.QuestConditionsUnmet,
	grpcx.Key(worldv1.Reason_REASON_QUEST_ALREADY_ACTIVE):       model.QuestAlreadyActive,
	grpcx.Key(worldv1.Reason_REASON_QUEST_ALREADY_FINISHED):     model.QuestAlreadyFinished,
	grpcx.Key(worldv1.Reason_REASON_TOO_MANY_QUESTS):            model.TooManyQuests,
	grpcx.Key(worldv1.Reason_REASON_QUEST_NOT_READY):            model.QuestNotReady,
	grpcx.Key(worldv1.Reason_REASON_HP_FULL):                    model.HPFull,
	grpcx.Key(worldv1.Reason_REASON_MP_FULL):                    model.MPFull,
	grpcx.Key(worldv1.Reason_REASON_NOT_USABLE):                 model.NotUsable,
}

// For is the Public form of a failed call: its reason's row, else Unavailable for a service down or busy, else Internal; the zero value for nil.
func For(err error) model.Public {
	if err == nil {
		return model.Public{}
	}
	if r, ok := grpcx.ReasonOf(err); ok {
		if pub, ok := reasons[r]; ok {
			return pub
		}
	}
	if status.Code(err) == codes.Unavailable {
		return model.Unavailable
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
