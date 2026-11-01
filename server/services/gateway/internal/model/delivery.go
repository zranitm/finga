package model

// Delivery is a service's push for one player; the use cases only shift its Tick and never read Data.
type Delivery struct {
	Type  string
	Tick  uint64
	MapID string // set when the player moves to another map
	Data  any
}

// DeliverySnapshot is the delivery type a held join waits for.
const DeliverySnapshot = "Snapshot"
