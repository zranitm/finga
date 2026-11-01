package model

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"finga/internal/kernel"
)

// Channel is where a line goes.
type Channel string

// The channels.
const (
	ChannelMap   Channel = "map"
	ChannelParty Channel = "party"
)

// Limits.
const (
	LineMaxLen        = 120 // characters, after cleaning
	LineRatePerSecond = 1.0
	LineBurst         = 3
)

// Sender is who speaks.
type Sender struct {
	ID   kernel.CharacterID
	Name string
}

// Message is a cleaned line ready to deliver.
type Message struct {
	Channel    Channel
	Sender     kernel.CharacterID
	SenderName string
	Text       string
}

// The chat errors; the handler sends each as a contract reason.
var (
	ErrEmpty       = errors.New("chat: empty line")
	ErrTooLong     = errors.New("chat: line too long")
	ErrChannel     = errors.New("chat: unknown channel")
	ErrChatNoParty = errors.New("chat: party line from a player with no party")
	ErrRate        = errors.New("chat: rate limited")
	ErrNoParties   = errors.New("chat: party chat not wired")
)

// CleanLine strips control characters and trims the ends; the client renders it as plain text.
func CleanLine(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if unicode.IsControl(r) || r == utf8.RuneError {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
