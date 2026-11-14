package local

import (
	"errors"
	"testing"

	"finga/internal/kernel"
	"finga/services/gateway/internal/model"
)

// The kinds of module failure the use cases act on.
const (
	none          = ""
	unavailable   = "unavailable"    // model.ErrUnavailable: worth waiting for
	notSubscribed = "not subscribed" // model.ErrNotSubscribed: the world doesn't hold the stream yet
)

// Every kernel error reaches a client by its Public name, with the kind the use cases act on.
// The rows are literals, and a rule test keeps one per kernel error, so a change to PublicOf or wrap fails here.
func TestEachKernelErrorReachesTheClientByName(t *testing.T) {
	for _, tt := range []struct {
		err  error
		name string
		kind string
	}{
		// the kernel's own
		{kernel.ErrInternal, "INTERNAL", none},
		// any module
		{kernel.ErrBadID, "BAD_ID", none},
		{kernel.ErrUnknownClass, "UNKNOWN_CLASS", none},
		// account
		{kernel.ErrAccountNotFound, "ACCOUNT_NOT_FOUND", none},
		{kernel.ErrUsernameTaken, "USERNAME_TAKEN", none},
		{kernel.ErrUnauthenticated, "UNAUTHENTICATED", none},
		{kernel.ErrLoginRateLimited, "LOGIN_RATE_LIMITED", none},
		{kernel.ErrBadUsername, "BAD_USERNAME", none},
		{kernel.ErrBadPassword, "BAD_PASSWORD", none},
		{kernel.ErrBadControlPreset, "BAD_CONTROL_PRESET", none},
		{kernel.ErrSessionRace, "SESSION_RACE", none},
		{kernel.ErrBusy, "RATE_LIMITED", unavailable},
		// character
		{kernel.ErrCharacterNotFound, "CHARACTER_NOT_FOUND", none},
		{kernel.ErrNameTaken, "NAME_TAKEN", none},
		{kernel.ErrBadCharacterName, "BAD_CHARACTER_NAME", none},
		{kernel.ErrClassUnavailable, "CLASS_UNAVAILABLE", none},
		{kernel.ErrCharacterSlotsFull, "CHARACTER_SLOTS_FULL", none},
		{kernel.ErrBadQuest, "INTERNAL", none},
		{kernel.ErrBadSkill, "INTERNAL", none},
		{kernel.ErrBadSkillTimer, "INTERNAL", none},
		{kernel.ErrBadSkillBar, "BAD_SKILL_BAR", none},
		{kernel.ErrBadCheckpoint, "INTERNAL", none},
		// item
		{kernel.ErrItemNotFound, "ITEM_NOT_FOUND", none},
		{kernel.ErrSlotTaken, "SLOT_TAKEN", none},
		{kernel.ErrSlotHoldsOtherItem, "SLOT_HOLDS_OTHER_ITEM", none},
		{kernel.ErrStackFull, "STACK_FULL", none},
		{kernel.ErrEquipTabFull, "EQUIP_TAB_FULL", none},
		{kernel.ErrUseTabFull, "USE_TAB_FULL", none},
		{kernel.ErrEtcTabFull, "ETC_TAB_FULL", none},
		{kernel.ErrIdempotencyKeyReused, "IDEMPOTENCY_KEY_REUSED", none},
		{kernel.ErrBadIdempotencyKey, "BAD_ID", none},
		{kernel.ErrBadCause, "INTERNAL", none},
		{kernel.ErrBadQuantity, "BAD_QUANTITY", none},
		{kernel.ErrBadSlot, "BAD_SLOT", none},
		{kernel.ErrWrongTab, "WRONG_TAB", none},
		{kernel.ErrUnknownItem, "UNKNOWN_ITEM", none},
		{kernel.ErrBadRoll, "INTERNAL", none},
		{kernel.ErrItemWorn, "ITEM_WORN", none},
		{kernel.ErrLevelTooLow, "LEVEL_TOO_LOW", none},
		{kernel.ErrWrongClass, "WRONG_CLASS", none},
		{kernel.ErrSTRTooLow, "STR_TOO_LOW", none},
		{kernel.ErrDEXTooLow, "DEX_TOO_LOW", none},
		{kernel.ErrINTTooLow, "INT_TOO_LOW", none},
		{kernel.ErrAGITooLow, "AGI_TOO_LOW", none},
		{kernel.ErrVITTooLow, "VIT_TOO_LOW", none},
		{kernel.ErrWrongEquipSlot, "WRONG_EQUIP_SLOT", none},
		{kernel.ErrNotEquipment, "NOT_EQUIPMENT", none},
		{kernel.ErrNothingWorn, "NOTHING_WORN", none},
		{kernel.ErrBadBatchSize, "BAD_ITEM_BATCH", none},
		{kernel.ErrNotEnoughItems, "NOT_ENOUGH_ITEMS", none},
		{kernel.ErrNotUsable, "NOT_USABLE", none},
		// social
		{kernel.ErrNotPartyLeader, "NOT_PARTY_LEADER", none},
		{kernel.ErrPartyFull, "PARTY_FULL", none},
		{kernel.ErrAlreadyInParty, "ALREADY_IN_PARTY", none},
		{kernel.ErrTargetInParty, "TARGET_IN_PARTY", none},
		{kernel.ErrNoParty, "NO_PARTY", none},
		{kernel.ErrNotPartyMember, "NOT_PARTY_MEMBER", none},
		{kernel.ErrTargetNotOnMap, "TARGET_NOT_ON_MAP", none},
		{kernel.ErrAlreadyInvited, "ALREADY_INVITED", none},
		{kernel.ErrInviteExpired, "INVITE_EXPIRED", none},
		{kernel.ErrInviteSelf, "INVITE_SELF", none},
		{kernel.ErrChatEmpty, "CHAT_EMPTY", none},
		{kernel.ErrChatTooLong, "CHAT_TOO_LONG", none},
		{kernel.ErrBadChatChannel, "BAD_CHAT_CHANNEL", none},
		{kernel.ErrChatNoParty, "CHAT_NO_PARTY", none},
		{kernel.ErrChatRateLimited, "CHAT_RATE_LIMITED", none},
		// world
		{kernel.ErrMapBusy, "UNAVAILABLE", unavailable},
		{kernel.ErrNoMap, "INTERNAL", none},
		{kernel.ErrWorldStopped, "UNAVAILABLE", unavailable},
		{kernel.ErrNotSubscribed, "INTERNAL", notSubscribed},
		{kernel.ErrCharacterDead, "CHARACTER_DEAD", none},
		{kernel.ErrNpcOutOfReach, "NPC_OUT_OF_REACH", none},
		{kernel.ErrQuestUnavailable, "QUEST_UNAVAILABLE", none},
		{kernel.ErrQuestConditionsUnmet, "QUEST_CONDITIONS_UNMET", none},
		{kernel.ErrQuestAlreadyActive, "QUEST_ALREADY_ACTIVE", none},
		{kernel.ErrQuestAlreadyFinished, "QUEST_ALREADY_FINISHED", none},
		{kernel.ErrTooManyQuests, "TOO_MANY_QUESTS", none},
		{kernel.ErrQuestNotReady, "QUEST_NOT_READY", none},
		{kernel.ErrHPFull, "HP_FULL", none},
		{kernel.ErrPointsChanged, "POINTS_CHANGED", none},
		{kernel.ErrNotEnoughPoints, "NOT_ENOUGH_POINTS", none},
		{kernel.ErrNoFreeReset, "NO_FREE_RESET", none},
		{kernel.ErrBadPoints, "BAD_POINTS", none},
		{kernel.ErrSkillPointsChanged, "SKILL_POINTS_CHANGED", none},
		{kernel.ErrNotEnoughSkillPoints, "NOT_ENOUGH_SKILL_POINTS", none},
		{kernel.ErrSkillNotLearnable, "SKILL_NOT_LEARNABLE", none},
		{kernel.ErrBadSkillPoints, "BAD_SKILL_POINTS", none},
		{kernel.ErrSkillNotLearned, "SKILL_NOT_LEARNED", none},
		{kernel.ErrSkillPassive, "SKILL_PASSIVE", none},
		{kernel.ErrNotInTown, "NOT_IN_TOWN", none},
		{kernel.ErrSkillCoolingDown, "SKILL_COOLING_DOWN", none},
		{kernel.ErrNotEnoughEnergy, "NOT_ENOUGH_ENERGY", none},
		{kernel.ErrSkillWaiting, "SKILL_WAITING", none},
		{kernel.ErrPromotionConditionsUnmet, "PROMOTION_CONDITIONS_UNMET", none},
		{kernel.ErrNotYourNextClass, "NOT_YOUR_NEXT_CLASS", none},
	} {
		t.Run(tt.err.Error(), func(t *testing.T) {
			err := wrap(tt.err)
			if got := model.PublicOf(err).Name; got != tt.name {
				t.Errorf("reaches the client as %s, want %s", got, tt.name)
			}
			kind := none
			switch {
			case errors.Is(err, model.ErrNotSubscribed):
				kind = notSubscribed
			case errors.Is(err, model.ErrUnavailable):
				kind = unavailable
			}
			if kind != tt.kind {
				t.Errorf("the use cases act on %q, want %q", kind, tt.kind)
			}
		})
	}
}
