package world

// Channels are a region's open channels and how full each is, against the players a channel takes before the next opens.
type Channels struct {
	Open []ChannelLoad
	Cap  int
}

// ChannelLoad is a channel's number and its players, those on their way included.
type ChannelLoad struct {
	Channel int
	Players int
}
