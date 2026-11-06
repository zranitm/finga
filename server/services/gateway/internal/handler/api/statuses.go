package api

import (
	"net/http"

	"finga/services/gateway/internal/model"
)

// statuses is every failure's HTTP status, by its class: 400 bad input, 401, 404, 408, 409 a rule the
// request ran into, 429, 503, 500. testdata/errors/codes.json is written from it.
var statuses = map[model.Public]int{
	model.MalformedBody:        http.StatusBadRequest,
	model.MissingField:         http.StatusBadRequest,
	model.BodyTooLarge:         http.StatusRequestEntityTooLarge,
	model.BodyTimeout:          http.StatusRequestTimeout,
	model.NoSuchEndpoint:       http.StatusNotFound,
	model.MethodNotAllowed:     http.StatusMethodNotAllowed,
	model.RateLimited:          http.StatusTooManyRequests,
	model.NotImplemented:       http.StatusNotImplemented,
	model.Unauthenticated:      http.StatusUnauthorized,
	model.BadID:                http.StatusBadRequest,
	model.UnknownClass:         http.StatusBadRequest,
	model.Internal:             http.StatusInternalServerError,
	model.Unavailable:          http.StatusServiceUnavailable,
	model.CharacterNotFound:    http.StatusNotFound,
	model.NameTaken:            http.StatusConflict,
	model.BadCharacterName:     http.StatusBadRequest,
	model.ClassUnavailable:     http.StatusBadRequest,
	model.CharacterSlotsFull:   http.StatusConflict,
	model.AccountNotFound:      http.StatusNotFound,
	model.UsernameTaken:        http.StatusConflict,
	model.LoginRateLimited:     http.StatusTooManyRequests,
	model.BadUsername:          http.StatusBadRequest,
	model.BadPassword:          http.StatusBadRequest,
	model.BadControlPreset:     http.StatusBadRequest,
	model.SessionRace:          http.StatusConflict,
	model.ItemNotFound:         http.StatusNotFound,
	model.SlotTaken:            http.StatusConflict,
	model.SlotHoldsOtherItem:   http.StatusConflict,
	model.StackFull:            http.StatusConflict,
	model.InventoryFull:        http.StatusConflict,
	model.IdempotencyKeyReused: http.StatusConflict,
	model.BadQuantity:          http.StatusBadRequest,
	model.BadSlot:              http.StatusBadRequest,
	model.WrongTab:             http.StatusBadRequest,
	model.UnknownItem:          http.StatusBadRequest,
	model.NotPartyLeader:       http.StatusConflict,
	model.PartyFull:            http.StatusConflict,
	model.AlreadyInParty:       http.StatusConflict,
	model.NoParty:              http.StatusNotFound,
	model.NotPartyMember:       http.StatusNotFound,
	model.TargetNotOnMap:       http.StatusNotFound,
	model.AlreadyInvited:       http.StatusConflict,
	model.InviteExpired:        http.StatusNotFound,
	model.InviteSelf:           http.StatusBadRequest,
	model.ChatEmpty:            http.StatusBadRequest,
	model.ChatTooLong:          http.StatusBadRequest,
	model.BadChatChannel:       http.StatusBadRequest,
	model.ChatNoParty:          http.StatusNotFound,
	model.ChatRateLimited:      http.StatusTooManyRequests,
	model.PartyChatUnavailable: http.StatusServiceUnavailable,
	model.PickupsUnavailable:   http.StatusServiceUnavailable,
	model.PickupBusy:           http.StatusServiceUnavailable,
}

// status is p's HTTP status; one the table lacks answers 500, and TestEveryFailureHasAStatus keeps that from happening.
func status(p model.Public) int {
	if s, ok := statuses[p]; ok {
		return s
	}
	return http.StatusInternalServerError
}
