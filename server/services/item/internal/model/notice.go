package model

import "time"

// NoticeKind says what other services hear happened to an item.
type NoticeKind string

// The notice kinds.
const (
	NoticeGranted   NoticeKind = "granted"
	NoticeLooted    NoticeKind = "looted"
	NoticeDiscarded NoticeKind = "discarded"
	NoticeHandedIn  NoticeKind = "handed_in"
)

// Notice tells other services about one item's change; it commits with the change.
type Notice struct {
	Kind       NoticeKind
	Item       Item
	Quantity   int // this item's change, as its event's QuantityDelta
	Cause      Cause
	OccurredAt time.Time
}
