package account

// Delivery is what a module tells a player at a tick; the use cases route it, hold it and shift its Tick, and the handlers encode its Event by its type.
type Delivery struct {
	Tick   uint64
	Event  any    // the module's own event, which the handler of that module turns into its message
	Starts bool   // the player's Snapshot: the first of a join's world deliveries, which may start the ticks again
	MapID  string // set by a map change: the map the player is on from now
}
