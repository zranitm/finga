// Package chat validates chat lines and decides who hears them: the map
// channel goes to everyone on the sender's map (the map instance fans it
// out), the party channel to the sender's party wherever its members are.
// No repository (TD-19): nothing is stored.
package chat

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"finga/internal/kernel"
	"finga/pkg/ratelimit"
)

// Channel is where a line goes.
type Channel string

// The channels.
const (
	Map   Channel = "map"
	Party Channel = "party"
)

// Limits (page 4 and page 5).
const (
	MaxLen        = 120 // characters, after cleaning
	RatePerSecond = 1.0
	Burst         = 3.0
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

// Parties answers who is in the sender's party.
type Parties interface {
	Members(id kernel.CharacterID) []kernel.CharacterID
}

// Options tune the service. Zero values take the defaults.
type Options struct {
	Clock func() time.Time
}

// Service cleans, limits and routes.
type Service struct {
	parties Parties
	limiter *ratelimit.Buckets
}

// NewService makes the service; parties may be nil (no party channel).
func NewService(parties Parties, opts Options) *Service {
	var lopts []ratelimit.Option
	if opts.Clock != nil {
		lopts = append(lopts, ratelimit.WithClock(opts.Clock))
	}
	return &Service{parties: parties, limiter: ratelimit.NewBuckets(RatePerSecond, Burst, lopts...)}
}

// The module's errors. Each names one failure; the service's handler sends
// it as a reason, and the gateway gives it a code, a status and the words a
// player sees (its errmap).
var (
	ErrEmpty     = errors.New("chat: empty line")
	ErrTooLong   = errors.New("chat: line too long")
	ErrChannel   = errors.New("chat: unknown channel")
	ErrNoParty   = errors.New("chat: party line from a player with no party")
	ErrRate      = errors.New("chat: rate limited")
	ErrNoParties = errors.New("chat: party chat not wired")
)

// Send cleans the text and charges the sender's limit. For the map channel
// the recipients are nil (the map instance delivers to everyone on it);
// for the party channel they are the party's members, sender included.
func (s *Service) Send(from Sender, channel Channel, text string) (Message, []kernel.CharacterID, error) {
	if channel != Map && channel != Party {
		return Message{}, nil, ErrChannel
	}
	text = Clean(text)
	if text == "" {
		return Message{}, nil, ErrEmpty
	}
	if utf8.RuneCountInString(text) > MaxLen {
		return Message{}, nil, ErrTooLong
	}
	if !s.limiter.Allow(from.ID.String(), 1) {
		return Message{}, nil, ErrRate
	}
	msg := Message{Channel: channel, Sender: from.ID, SenderName: from.Name, Text: text}
	if channel == Map {
		return msg, nil, nil
	}
	if s.parties == nil {
		return Message{}, nil, ErrNoParties
	}
	members := s.parties.Members(from.ID)
	if len(members) == 0 {
		return Message{}, nil, ErrNoParty
	}
	return msg, members, nil
}

// Clean strips control characters (newlines, tabs, escapes) and trims the
// ends; the client renders the result as plain text, never as HTML.
func Clean(text string) string {
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
