package model

// Delivery is a service's push for one player, as the client will see it:
// a client protocol message the clients layer built from the service's
// event (Data), its type, and the server tick. The use cases forward it as
// it came, apart from shifting the tick, and never read Data: Type tells a
// Snapshot, and MapID the map a MapChange moves the player to.
type Delivery struct {
	Type  string
	Tick  uint64
	MapID string // set on a delivery that moves the player to another map
	Data  any
}

// DeliverySnapshot is the type of the delivery that places everyone on a
// map: a join's first, and the one a held join waits for.
const DeliverySnapshot = "Snapshot"
