// Package model holds the gateway's use-case data: client-facing failures, command inputs and deliveries.
package model

import (
	"errors"

	"finga/internal/kernel"
	"finga/pkg/transient"
)

// Public is a failure as a client sees it; Message is the gateway's own, never an error's text, so causes never leak.
type Public struct {
	Code    int    // six digits: the area, then the case (failures.go)
	Name    string // EQUIP_TAB_FULL: the code in source and in logs
	Message string
}

// ErrHelloRejected is a Hello the gateway refuses itself.
var ErrHelloRejected = errors.New("gateway: hello rejected")

// The kinds of service failure the gateway acts on itself.
var (
	ErrUnavailable   = errors.New("gateway: service unavailable")
	ErrNotSubscribed = errors.New("gateway: the world does not hold this gateway's stream")
)

// ServiceError is a service failure: what the client sees, the kind the gateway acts on (or nil), and the cause.
type ServiceError struct {
	Public Public
	Kind   error
	Err    error
}

func (e *ServiceError) Error() string { return e.Err.Error() }

// Unwrap exposes the kind and the cause to errors.Is and errors.As.
func (e *ServiceError) Unwrap() []error {
	if e.Kind != nil {
		return []error{e.Kind, e.Err}
	}
	return []error{e.Err}
}

// ownErrors are the gateway's own failures and every module's, each as the client saw it over gRPC.
var ownErrors = []struct {
	err error
	pub Public
}{
	{ErrHelloRejected, Unauthenticated},
	{kernel.ErrBadID, BadID},
	{kernel.ErrUnknownClass, UnknownClass},
	{kernel.ErrInternal, Internal}, // a module's own failure, as anything unexpected reads
	// account
	{kernel.ErrAccountNotFound, AccountNotFound},
	{kernel.ErrUsernameTaken, UsernameTaken},
	{kernel.ErrUnauthenticated, Unauthenticated},
	{kernel.ErrLoginRateLimited, LoginRateLimited},
	{kernel.ErrBadUsername, BadUsername},
	{kernel.ErrBadPassword, BadPassword},
	{kernel.ErrBadControlPreset, BadControlPreset},
	{kernel.ErrSessionRace, SessionRace},
	{kernel.ErrBusy, RateLimited},
	// character
	{kernel.ErrCharacterNotFound, CharacterNotFound},
	{kernel.ErrNameTaken, NameTaken},
	{kernel.ErrBadCharacterName, BadCharacterName},
	{kernel.ErrClassUnavailable, ClassUnavailable},
	{kernel.ErrCharacterSlotsFull, CharacterSlotsFull},
	{kernel.ErrBadQuest, Internal},
	{kernel.ErrBadSkill, Internal},
	{kernel.ErrBadSkillTimer, Internal},
	{kernel.ErrBadCheckpoint, Internal},
	{kernel.ErrBadSkillBar, BadSkillBar},
	// item
	{kernel.ErrItemNotFound, ItemNotFound},
	{kernel.ErrSlotTaken, SlotTaken},
	{kernel.ErrSlotHoldsOtherItem, SlotHoldsOtherItem},
	{kernel.ErrStackFull, StackFull},
	{kernel.ErrEquipTabFull, EquipTabFull},
	{kernel.ErrUseTabFull, UseTabFull},
	{kernel.ErrEtcTabFull, EtcTabFull},
	{kernel.ErrIdempotencyKeyReused, IdempotencyKeyReused},
	{kernel.ErrBadIdempotencyKey, BadID},
	{kernel.ErrBadCause, Internal},
	{kernel.ErrBadQuantity, BadQuantity},
	{kernel.ErrBadSlot, BadSlot},
	{kernel.ErrWrongTab, WrongTab},
	{kernel.ErrUnknownItem, UnknownItem},
	{kernel.ErrBadRoll, Internal},
	{kernel.ErrItemWorn, ItemWorn},
	{kernel.ErrLevelTooLow, LevelTooLow},
	{kernel.ErrWrongClass, WrongClass},
	{kernel.ErrSTRTooLow, STRTooLow},
	{kernel.ErrDEXTooLow, DEXTooLow},
	{kernel.ErrINTTooLow, INTTooLow},
	{kernel.ErrAGITooLow, AGITooLow},
	{kernel.ErrVITTooLow, VITTooLow},
	{kernel.ErrWrongEquipSlot, WrongEquipSlot},
	{kernel.ErrNotEquipment, NotEquipment},
	{kernel.ErrNothingWorn, NothingWorn},
	{kernel.ErrBadBatchSize, BadItemBatch},
	{kernel.ErrNotEnoughItems, NotEnoughItems},
	{kernel.ErrNotUsable, NotUsable},
	// social
	{kernel.ErrNotPartyLeader, NotPartyLeader},
	{kernel.ErrPartyFull, PartyFull},
	{kernel.ErrAlreadyInParty, AlreadyInParty},
	{kernel.ErrTargetInParty, TargetInParty},
	{kernel.ErrNoParty, NoParty},
	{kernel.ErrNotPartyMember, NotPartyMember},
	{kernel.ErrTargetNotOnMap, TargetNotOnMap},
	{kernel.ErrAlreadyInvited, AlreadyInvited},
	{kernel.ErrInviteExpired, InviteExpired},
	{kernel.ErrInviteSelf, InviteSelf},
	{kernel.ErrChatEmpty, ChatEmpty},
	{kernel.ErrChatTooLong, ChatTooLong},
	{kernel.ErrBadChatChannel, BadChatChannel},
	{kernel.ErrChatNoParty, ChatNoParty},
	{kernel.ErrChatRateLimited, ChatRateLimited},
	// world
	{kernel.ErrMapBusy, Unavailable},
	{kernel.ErrNoMap, Internal},
	{kernel.ErrWorldStopped, Unavailable},
	{kernel.ErrCharacterDead, CharacterDead},
	{kernel.ErrPointsChanged, PointsChanged},
	{kernel.ErrNotEnoughPoints, NotEnoughPoints},
	{kernel.ErrNoFreeReset, NoFreeReset},
	{kernel.ErrBadPoints, BadPoints},
	{kernel.ErrSkillPointsChanged, SkillPointsChanged},
	{kernel.ErrNotEnoughSkillPoints, NotEnoughSkillPoints},
	{kernel.ErrSkillNotLearnable, SkillNotLearnable},
	{kernel.ErrBadSkillPoints, BadSkillPoints},
	{kernel.ErrSkillNotLearned, SkillNotLearned},
	{kernel.ErrSkillPassive, SkillPassive},
	{kernel.ErrNotInTown, NotInTown},
	{kernel.ErrSkillCoolingDown, SkillCoolingDown},
	{kernel.ErrNotEnoughEnergy, NotEnoughEnergy},
	{kernel.ErrSkillWaiting, SkillWaiting},
	{kernel.ErrPromotionConditionsUnmet, PromotionUnmet},
	{kernel.ErrNotYourNextClass, NotYourNextClass},
	{kernel.ErrNpcOutOfReach, NpcOutOfReach},
	{kernel.ErrQuestUnavailable, QuestUnavailable},
	{kernel.ErrQuestConditionsUnmet, QuestConditionsUnmet},
	{kernel.ErrQuestAlreadyActive, QuestAlreadyActive},
	{kernel.ErrQuestAlreadyFinished, QuestAlreadyFinished},
	{kernel.ErrTooManyQuests, TooManyQuests},
	{kernel.ErrQuestNotReady, QuestNotReady},
	{kernel.ErrHPFull, HPFull},
	{kernel.ErrNotSubscribed, Internal},
}

// PublicOf is err as a client sees it: Internal for anything unexpected, the zero Public for nil.
func PublicOf(err error) Public {
	if err == nil {
		return Public{}
	}
	if se, ok := errors.AsType[*ServiceError](err); ok {
		return se.Public
	}
	for _, row := range ownErrors {
		if errors.Is(err, row.err) {
			return row.pub
		}
	}
	if errors.Is(err, transient.ErrUnavailable) {
		return Unavailable // a module down or busy for now, as one unreachable over gRPC
	}
	return Internal
}

// IsInternal reports whether p answers an unexpected error, which the caller should log.
func IsInternal(p Public) bool { return p.Code == Internal.Code }
