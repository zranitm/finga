package instance

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/transient"
	"finga/services/world/internal/application/quests"
	"finga/services/world/internal/model"
)

// handInTimeout bounds one hand-in call to the item service.
const handInTimeout = 5 * time.Second

type questAction uint8

const (
	acceptQuest questAction = iota + 1
	abandonQuest
	handInQuest
)

// questMsg asks to accept, abandon or hand in a quest; done hears the outcome once it applies.
type questMsg struct {
	action questAction
	id     kernel.CharacterID
	conn   string
	npc    string // none for an abandon
	quest  string
	done   chan error
}

// handedInMsg is a hand-in call's answer, for the tick to apply.
type handedInMsg struct {
	id         kernel.CharacterID
	conn       string
	quest      string
	completion int // the TimesFinished it makes
	result     model.Inventory
	err        error
	done       chan error
}

func (questMsg) message()    {}
func (handedInMsg) message() {}

// AcceptQuest takes a quest the NPC within reach gives, and returns once it is in the log and saved.
func (i *Instance) AcceptQuest(ctx context.Context, id kernel.CharacterID, conn, npc, quest string) error {
	return i.askQuest(ctx, questMsg{action: acceptQuest, id: id, conn: conn, npc: npc, quest: quest})
}

// AbandonQuest drops an active quest and its progress, and returns once saved; one not active is left as it is.
func (i *Instance) AbandonQuest(ctx context.Context, id kernel.CharacterID, conn, quest string) error {
	return i.askQuest(ctx, questMsg{action: abandonQuest, id: id, conn: conn, quest: quest})
}

// HandInQuest hands a ready quest in to the NPC within reach, and returns once the item service's answer applies.
func (i *Instance) HandInQuest(ctx context.Context, id kernel.CharacterID, conn, npc, quest string) error {
	return i.askQuest(ctx, questMsg{action: handInQuest, id: id, conn: conn, npc: npc, quest: quest})
}

func (i *Instance) askQuest(ctx context.Context, m questMsg) error {
	m.done = make(chan error, 1)
	return i.ask(ctx, m, m.done)
}

func (i *Instance) quest(m questMsg) {
	if m.action == abandonQuest {
		p, ok := i.players[m.id]
		if !ok || p.Connection != m.conn {
			m.done <- fmt.Errorf("%w: the character is not on this map through that connection", transient.ErrUnavailable)
			return
		}
		m.done <- i.abandon(p, m.quest)
		return
	}
	p, err := i.alive(m.id, m.conn)
	if err != nil {
		m.done <- err
		return
	}
	npc, ok := i.reachable(p, m.npc)
	if !ok {
		m.done <- model.ErrNpcOutOfReach
		return
	}
	if m.action == acceptQuest {
		m.done <- i.accept(p, npc, m.quest)
		return
	}
	if err := i.handIn(p, npc, m); err != nil {
		m.done <- err
	}
}

// accept puts a quest the NPC gives in the log with no progress, keeping the count of a repeatable one, and saves at once.
func (i *Instance) accept(p *player, npc gamedata.NPC, id string) error {
	q, ok := i.data.Quest(id)
	if !ok || !slices.Contains(npc.Gives, id) {
		return model.ErrQuestUnavailable
	}
	switch quests.Offer(q, i.questsOf(p)).State {
	case model.OfferActive:
		return model.ErrQuestActive
	case model.OfferFinished:
		return model.ErrQuestFinished
	case model.OfferUnmet:
		return model.ErrQuestUnmet
	case model.OfferAvailable:
	}
	if quests.Active(p.Quests) >= quests.MaxActive {
		return model.ErrTooManyQuests
	}
	p.Quests[id] = model.Quest{Status: model.QuestActive, Progress: make([]int, len(q.Objectives)), TimesFinished: p.Quests[id].TimesFinished}
	i.questChanged(p, id, model.QuestAccepted)
	i.checkpoint(p)
	return nil
}

// abandon drops an active quest's progress: one never finished leaves the log, a repeatable one finished before is finished again. It saves at once.
func (i *Instance) abandon(p *player, id string) error {
	q, ok := p.Quests[id]
	switch {
	case !ok || q.Status != model.QuestActive:
		return nil
	case p.handingIn[id]:
		return fmt.Errorf("%w: its hand-in is in flight", transient.ErrUnavailable)
	case q.TimesFinished == 0:
		delete(p.Quests, id)
	default:
		p.Quests[id] = model.Quest{Status: model.QuestFinished, TimesFinished: q.TimesFinished}
	}
	i.questChanged(p, id, model.QuestAbandoned)
	i.checkpoint(p)
	return nil
}

// handIn checks a quest is ready, then has a worker call the item service, whose answer the tick applies; one with no items to take or give finishes at once.
func (i *Instance) handIn(p *player, npc gamedata.NPC, m questMsg) error {
	q, ok := i.data.Quest(m.quest)
	if !ok || !slices.Contains(npc.Takes, m.quest) {
		return model.ErrQuestUnavailable
	}
	entry, active := p.Quests[m.quest]
	switch {
	case !active || entry.Status != model.QuestActive || !quests.Ready(q, entry, p.Level):
		return model.ErrQuestNotReady
	case p.handingIn[m.quest]:
		return fmt.Errorf("%w: its hand-in is in flight", transient.ErrUnavailable)
	}
	completion := entry.TimesFinished + 1
	take, give := quests.Items(i.data, q, i.between)
	if len(take)+len(give) == 0 {
		i.finish(p, q, completion, model.Inventory{})
		m.done <- nil
		return nil
	}
	call := model.HandInCall{Account: p.Account, Character: p.ID, Key: quests.HandInKey(p.ID, q.ID, completion), Quest: q.ID, Take: take, Give: give}
	submitted := i.deps.ItemCalls.Submit(func(ctx context.Context) {
		callCtx, cancel := context.WithTimeout(ctx, handInTimeout)
		result, err := i.deps.HandIns.HandIn(callCtx, call)
		cancel()
		select {
		case i.inbox <- handedInMsg{id: p.ID, conn: p.Connection, quest: q.ID, completion: completion, result: result, err: err, done: m.done}:
		case <-ctx.Done(): // the map stopped first; its inbox may never drain
		}
	})
	if !submitted {
		return fmt.Errorf("%w: every item-call worker is busy", transient.ErrUnavailable)
	}
	p.itemCalls++
	if p.handingIn == nil {
		p.handingIn = map[string]bool{}
	}
	p.handingIn[q.ID] = true
	return nil
}

// settleHandIn finishes the quest once the item service has taken and given; a refusal changed nothing anywhere. A player gone meanwhile hands it in again on return, under the same key.
func (i *Instance) settleHandIn(m handedInMsg) {
	p, ok := i.players[m.id]
	if ok && p.Connection == m.conn {
		p.itemCalls--
		delete(p.handingIn, m.quest)
	}
	switch {
	case m.err != nil:
		m.done <- m.err
	case !ok || p.Connection != m.conn:
		m.done <- fmt.Errorf("%w: the character left the map before the hand-in applied", transient.ErrUnavailable)
	default:
		q, _ := i.data.Quest(m.quest)
		i.finish(p, q, m.completion, m.result)
		m.done <- nil
	}
}

// finish marks the quest finished for the completion-th time, awards its EXP, tells the player what the items did, and saves at once.
func (i *Instance) finish(p *player, q gamedata.Quest, completion int, items model.Inventory) {
	p.Quests[q.ID] = model.Quest{Status: model.QuestFinished, TimesFinished: completion}
	i.questChanged(p, q.ID, model.QuestHandedIn)
	if len(items.Slots) > 0 {
		p.private = append(p.private, model.InventoryChanged{Tick: i.tick, Revision: items.Revision, Slots: items.Slots})
	}
	i.award(p, q.Rewards.Exp)
	i.checkpoint(p)
}

// countKill adds the kill to every active kill objective for the mob, of each player who shares the kill's EXP.
func (i *Instance) countKill(killer *player, mob string) {
	for _, p := range i.present(killer, i.deps.Parties.Party(killer.ID)) {
		i.progress(p, gamedata.Kill, mob)
	}
}

// progress adds one to each of p's active objectives of kind for target, up to its count.
func (i *Instance) progress(p *player, kind gamedata.ObjectiveKind, target string) {
	for id, entry := range p.Quests {
		q, ok := i.data.Quest(id)
		if !ok || entry.Status != model.QuestActive {
			continue
		}
		for k, o := range q.Objectives {
			if o.Kind != kind || o.Target != target || k >= len(entry.Progress) || entry.Progress[k] >= o.Count {
				continue
			}
			entry.Progress[k]++
			i.questChanged(p, id, model.QuestProgressed)
		}
	}
}

// questChanged notes a change to tell at the tick's end; several in one tick make one message, with the most telling change.
func (i *Instance) questChanged(p *player, id string, c model.QuestChange) {
	if p.questChanges == nil {
		p.questChanges = map[string]model.QuestChange{}
	}
	p.questChanges[id] = max(p.questChanges[id], c)
}

// tellQuests tells each player the quests that changed this tick, as they stand now.
func (i *Instance) tellQuests() {
	for _, id := range i.order {
		p := i.players[id]
		for _, quest := range slices.Sorted(maps.Keys(p.questChanges)) {
			e := model.QuestChanged{Tick: i.tick, ID: quest, Change: p.questChanges[quest]}
			if q, ok := p.Quests[quest]; ok {
				q.Progress = slices.Clone(q.Progress)
				e.Quest = q
			}
			p.private = append(p.private, e)
		}
		clear(p.questChanges)
	}
}

// between picks a whole number from lo to hi with the map's random source, as a drop's roll does.
func (i *Instance) between(lo, hi int) int {
	return min(lo+int(i.roll.Float64()*float64(hi-lo+1)), hi)
}
