package application

import (
	"context"
	"unicode/utf8"

	"finga/internal/kernel"
	"finga/pkg/ratelimit"
	"finga/services/social/internal/model"
)

// Parties answers who is in the sender's party.
type Parties interface {
	Members(ctx context.Context, id kernel.CharacterID) ([]kernel.CharacterID, error)
}

// ChatService cleans, limits and routes.
type ChatService struct {
	parties Parties
	limiter *ratelimit.Buckets
}

// NewChatService makes the service over the party module.
func NewChatService(parties Parties) *ChatService {
	return &ChatService{parties: parties, limiter: ratelimit.NewBuckets(model.LineRatePerSecond, model.LineBurst)}
}

// Send cleans and rate-limits a line; map lines have nil recipients, party lines the whole party.
func (s *ChatService) Send(ctx context.Context, from model.Sender, channel model.Channel, text string) (model.Message, []kernel.CharacterID, error) {
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
	members, err := s.parties.Members(ctx, from.ID)
	if err != nil {
		return model.Message{}, nil, err
	}
	if len(members) == 0 {
		return model.Message{}, nil, model.ErrChatNoParty
	}
	return msg, members, nil
}
