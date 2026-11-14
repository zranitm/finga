package model

import "finga/internal/kernel"

// Chat is a map-channel line, sent to everyone on the map.
type Chat struct {
	Tick       uint64
	Sender     kernel.CharacterID
	SenderName string
	Text       string
}

func (Chat) event() {}
