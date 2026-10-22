// Package chat validates chat lines and decides who hears them: the map
// channel goes to everyone on the sender's map (the map instance fans it
// out), the party channel to the sender's party wherever its members are.
// No repository (TD-19): nothing is stored.
package chat

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"finga/internal/model"
	"finga/pkg/errs"
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
	ID   model.CharacterID
	Name string
}

// Message is a cleaned line ready to deliver.
type Message struct {
	Channel    Channel
	Sender     model.CharacterID
	SenderName string
	Text       string
}

// Parties answers who is in the sender's party.
type Parties interface {
	Members(id model.CharacterID) []model.CharacterID
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

// Errors.
var (
	ErrEmpty     = errs.Invalid("nothing to say")
	ErrTooLong   = errs.Invalid("a chat line is at most 120 characters")
	ErrChannel   = errs.Invalid("unknown chat channel")
	ErrNoParty   = errs.New(errs.NotFound, "you have no party")
	ErrRate      = errs.New(errs.RateLimited, "slow down")
	ErrNoParties = errs.New(errs.Unavailable, "party chat is not available")
)

// Send cleans the text and charges the sender's limit. For the map channel
// the recipients are nil (the map instance delivers to everyone on it);
// for the party channel they are the party's members, sender included.
func (s *Service) Send(from Sender, channel Channel, text string) (Message, []model.CharacterID, error) {
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
