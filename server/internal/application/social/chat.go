package social

import (
	"context"
	"unicode/utf8"

	model "finga/internal/model/social"
	"finga/internal/shared/kernel"
	"finga/pkg/ratelimit"
)

// Parties answers who is in the sender's party.
type Parties interface {
	Members(ctx context.Context, id kernel.CharacterID) ([]kernel.CharacterID, error)
}

// chatCases cleans, limits and routes.
type chatCases struct {
	parties Parties
	lineMax int
	limiter *ratelimit.Buckets
}

// chatCasesDeps are what the chat use cases is built over.
type chatCasesDeps struct {
	Parties Parties
	LineMax int // characters a line holds, after cleaning
}

// newChatCases makes the use cases over deps.
func newChatCases(deps chatCasesDeps) *chatCases {
	return &chatCases{parties: deps.Parties, lineMax: deps.LineMax, limiter: ratelimit.NewBuckets(model.LineRatePerSecond, model.LineBurst)}
}

// Send cleans and rate-limits a line; map lines have nil recipients, party lines the whole party.
func (s *chatCases) Send(ctx context.Context, from model.Sender, channel model.Channel, text string) (model.Message, []kernel.CharacterID, error) {
	if channel != model.ChannelMap && channel != model.ChannelParty {
		return model.Message{}, nil, model.ErrChannel
	}
	text = model.CleanLine(text)
	if model.Blank(text) {
		return model.Message{}, nil, model.ErrEmpty
	}
	if utf8.RuneCountInString(text) > s.lineMax {
		return model.Message{}, nil, model.ErrTooLong
	}
	if !s.limiter.Allow(from.ID.String()) {
		return model.Message{}, nil, model.ErrRate
	}
	msg := model.Message{Channel: channel, Sender: from.ID, SenderName: from.Name, Text: text}
	if channel == model.ChannelMap {
		return msg, nil, nil
	}
	members, err := s.parties.Members(ctx, from.ID)
	if err != nil {
		return model.Message{}, nil, err
	}
	if len(members) == 0 {
		return model.Message{}, nil, model.ErrChatNoParty
	}
	return msg, members, nil
}
