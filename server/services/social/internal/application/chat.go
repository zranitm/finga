package application

import (
	"time"
	"unicode/utf8"

	"finga/internal/kernel"
	"finga/pkg/ratelimit"
	"finga/services/social/internal/model"
)

// Parties answers who is in the sender's party.
type Parties interface {
	Members(id kernel.CharacterID) []kernel.CharacterID
}

// ChatOptions tune the service. Zero values take the defaults.
type ChatOptions struct {
	Clock func() time.Time
}

// ChatService cleans, limits and routes.
type ChatService struct {
	parties Parties
	limiter *ratelimit.Buckets
}

// NewChatService makes the service; parties may be nil (no party channel).
func NewChatService(parties Parties, opts ChatOptions) *ChatService {
	var lopts []ratelimit.Option
	if opts.Clock != nil {
		lopts = append(lopts, ratelimit.WithClock(opts.Clock))
	}
	return &ChatService{parties: parties, limiter: ratelimit.NewBuckets(model.LineRatePerSecond, model.LineBurst, lopts...)}
}

// Send cleans the text and charges the sender's limit. For the map channel
// the recipients are nil (the map instance delivers to everyone on it);
// for the party channel they are the party's members, sender included.
func (s *ChatService) Send(from model.Sender, channel model.Channel, text string) (model.Message, []kernel.CharacterID, error) {
	if channel != model.ChannelMap && channel != model.ChannelParty {
		return model.Message{}, nil, model.ErrChannel
	}
	text = model.CleanLine(text)
	if text == "" {
		return model.Message{}, nil, model.ErrEmpty
	}
	if utf8.RuneCountInString(text) > model.LineMaxLen {
		return model.Message{}, nil, model.ErrTooLong
	}
	if !s.limiter.Allow(from.ID.String()) {
		return model.Message{}, nil, model.ErrRate
	}
	msg := model.Message{Channel: channel, Sender: from.ID, SenderName: from.Name, Text: text}
	if channel == model.ChannelMap {
		return msg, nil, nil
	}
	if s.parties == nil {
		return model.Message{}, nil, model.ErrNoParties
	}
	members := s.parties.Members(from.ID)
	if len(members) == 0 {
		return model.Message{}, nil, model.ErrChatNoParty
	}
	return msg, members, nil
}
