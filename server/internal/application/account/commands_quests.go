package account

import (
	"context"

	model "finga/internal/model/account"
)

// Promote moves the player to the class the NPC within reach trains, once the trainer's weapon is given; Promoted follows on the world's stream.
func (g *Commands) Promote(ctx context.Context, s *Connection, npc, class string) error {
	return g.deps.World.Promote(ctx, s.Presence(), npc, class)
}

// Talk opens the dialog of an NPC within reach; the NpcDialog follows on the world's stream.
func (g *Commands) Talk(ctx context.Context, s *Connection, npc string) error {
	return g.deps.World.Talk(ctx, s.Presence(), npc)
}

// AcceptQuest takes a quest the NPC within reach gives; QuestChanged follows.
func (g *Commands) AcceptQuest(ctx context.Context, s *Connection, in model.QuestAsk) error {
	return g.deps.World.AcceptQuest(ctx, s.Presence(), in)
}

// AbandonQuest drops an active quest and its progress.
func (g *Commands) AbandonQuest(ctx context.Context, s *Connection, quest string) error {
	return g.deps.World.AbandonQuest(ctx, s.Presence(), quest)
}

// HandInQuest hands a ready quest in to the NPC within reach; InventoryChanged, QuestChanged and ExpGained follow.
func (g *Commands) HandInQuest(ctx context.Context, s *Connection, in model.QuestAsk) error {
	return g.deps.World.HandInQuest(ctx, s.Presence(), in)
}
