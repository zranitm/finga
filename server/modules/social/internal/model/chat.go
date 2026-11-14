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

// The rate a sender's lines may come at; the length a line may have is game data (chat.yaml).
const (
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
)

// CleanLine strips control and format characters (bidi overrides, zero-width marks) and trims the ends.
func CleanLine(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// brailleBlank is the braille pattern with no dots: a symbol that draws nothing.
const brailleBlank = '⠀'

// Blank reports whether a line shows nothing: no rune in it is graphic, not a space, and not one that renders as nothing.
func Blank(text string) bool {
	for _, r := range text {
		if unicode.IsGraphic(r) && !unicode.IsSpace(r) && !unicode.In(r, unicode.Other_Default_Ignorable_Code_Point, unicode.Variation_Selector) && r != brailleBlank {
			return false
		}
	}
	return true
}
