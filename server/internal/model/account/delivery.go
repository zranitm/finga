package account

import "time"

// Delivery is what a module tells a player at a tick; the use cases route it, hold it and shift its Tick, and the handlers encode its Event by its type.
type Delivery struct {
	Tick   uint64
	Event  any    // the module's own event, which the handler of that module turns into its message
	Starts bool   // the player's Snapshot: the first of a join's world deliveries, which may start the ticks again
	MapID  string // set by a map change: the map the player is on from now
}

// Push marks the model's own pushes, those a module other than the world tells a player, which the handlers encode by their type; the world's are its facade's events.
type Push interface{ push() }

// PiecesAdded tells of new wardrobe entries, each with what gave it.
type PiecesAdded struct {
	Pieces []AddedPiece
}

// AddedPiece is one new wardrobe entry: the piece and its cause, item, rank, quest or feat.
type AddedPiece struct {
	ID    string
	Cause string
}

// ChatMessage is a map or party line.
type ChatMessage struct {
	Channel            ChatChannel
	Sender, SenderName string
	Text               string
}

// InventoryChanged is the slots and equip slots a grant, a move or an equip changed, at the inventory's revision after it.
type InventoryChanged struct {
	Revision  uint64
	Slots     []InventorySlot
	Equipment []EquipSlot
}

// PartyState is the player's party; nil means none.
type PartyState struct {
	Party *PartyView
}

// PartyInvited is an invite to the sender's party.
type PartyInvited struct {
	InviteID       string
	From, FromName string
}

// PvpQueued says the player is in a mode's queue, having waited Since, and whether their match waits for room for its bots.
type PvpQueued struct {
	Mode        string
	Since       time.Duration
	Members     []string
	WaitingRoom bool
}

// PvpUnqueued says the player left the queue without a match; By is the members whose answer took the ticket out.
type PvpUnqueued struct {
	Reason PvpUnqueueReason
	By     []string
}

// PvpUnqueueReason is why.
type PvpUnqueueReason int

// The reasons.
const (
	PvpUnqueueLeft PvpUnqueueReason = iota
	PvpUnqueueDeclined
	PvpUnqueueLapsed
	PvpUnqueuePartyChanged
)

// PvpOffer is a match found, with how many bots and how long it stands.
type PvpOffer struct {
	Offer, Mode string
	Bots        int
	Until       time.Duration
}

// PvpOfferCancelled says an offer fell through, and whether the player is back in the queue.
type PvpOfferCancelled struct {
	Offer    string
	Reason   PvpGoneReason
	Requeued bool
}

// PvpGoneReason is why an offer fell through.
type PvpGoneReason int

// The reasons.
const (
	PvpGoneDeclined PvpGoneReason = iota
	PvpGoneLapsed
)

// MatchEnded is a match's outcome for the player, with their rating's change in a rated mode.
type MatchEnded struct {
	Mode   string
	Result MatchOutcome
	Battle bool // fought for points: Score is its, and it has no rounds
	Wins   [2]int
	Score  [2]int
	Rating *PvpRatingChange
	Left   bool
}

// MatchOutcome is a match's end for the player's side.
type MatchOutcome int

// The outcomes.
const (
	MatchWin MatchOutcome = iota
	MatchLoss
	MatchDraw
)

// PvpRatingChange is the shown rating and rank before and after, and the placement matches left.
type PvpRatingChange struct {
	Before, After         int
	RankBefore, RankAfter string
	PlacementLeft         int
}

func (PiecesAdded) push() {}

func (ChatMessage) push() {}

func (PartyState) push() {}

func (PartyInvited) push() {}

func (PvpQueued) push() {}

func (PvpUnqueued) push() {}

func (PvpOffer) push() {}

func (PvpOfferCancelled) push() {}

func (MatchEnded) push() {}
