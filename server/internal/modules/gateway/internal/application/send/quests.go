package send

import (
	"context"

	"finga/modules/gateway/internal/application/session"
	"finga/modules/gateway/internal/model"
)

// Promote moves the player to the class the NPC within reach trains, once the trainer's weapon is given; Promoted follows on the world's stream.
func (g *Gateway) Promote(ctx context.Context, s *session.Session, npc, class string) error {
	return g.deps.World.Promote(ctx, s.Presence(), npc, class)
}

// Talk opens the dialog of an NPC within reach; the NpcDialog follows on the world's stream.
func (g *Gateway) Talk(ctx context.Context, s *session.Session, npc string) error {
	return g.deps.World.Talk(ctx, s.Presence(), npc)
}

// AcceptQuest takes a quest the NPC within reach gives; QuestChanged follows.
func (g *Gateway) AcceptQuest(ctx context.Context, s *session.Session, in model.QuestAsk) error {
	return g.deps.World.AcceptQuest(ctx, s.Presence(), in)
}

// AbandonQuest drops an active quest and its progress.
func (g *Gateway) AbandonQuest(ctx context.Context, s *session.Session, quest string) error {
	return g.deps.World.AbandonQuest(ctx, s.Presence(), quest)
}

// HandInQuest hands a ready quest in to the NPC within reach; InventoryChanged, QuestChanged and ExpGained follow.
func (g *Gateway) HandInQuest(ctx context.Context, s *session.Session, in model.QuestAsk) error {
	return g.deps.World.HandInQuest(ctx, s.Presence(), in)
}
