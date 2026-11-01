package model

import "google.golang.org/protobuf/proto"

// Delivery is a service's push for one player; the use cases only shift its Tick and never read Data.
type Delivery struct {
	Type  string // Data's message name
	Tick  uint64
	MapID string // set when the player moves to another map
	Data  proto.Message
}

// DeliverySnapshot is the delivery type a held join waits for.
const DeliverySnapshot = "Snapshot"
