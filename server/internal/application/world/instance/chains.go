package instance

import (
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/nav"
)

// The slack a follow-up's condition gets: its client judges it from the frames it has, a little behind the world.
const (
	chainSlack = 150 * time.Millisecond
	chainReach = 0.5 // metres short of far
)

// chainReady is whether the use goes from the key it was pressed on, judged at the press: on its own key, a skill in none of its class's chains but as their root; as a follow-up, one of root's whose condition held, within the slack.
func (i *Instance) chainReady(u use, root string) error {
	c, skill := u.p.class, u.s.ID
	if root == "" || root == skill {
		if c.Keyable(skill) {
			return nil
		}
		return model.ErrChainNotReady
	}
	f, ok := c.FollowUp(root, skill)
	if !ok || !i.holds(u.p, root, f.When, u.seq, u.target) {
		return model.ErrChainNotReady
	}
	return nil
}

// holds is whether w held for p at the press, during input seq: windows count inputs, as its client does, from the root's hit or the input the dodge's frame acked; controlled and far read the lock now, a status ended within the slack still counting.
func (i *Instance) holds(p *player, root string, w gamedata.ChainCondition, seq uint32, target uint32) bool {
	press, slack := uint64(p.pressedDuring(seq)), i.ticksFor(chainSlack)
	switch {
	case w.After > 0:
		hit, ok := p.hitSeqs[root]
		return ok && hit <= press+slack && press <= hit+i.ticksFor(w.After)+slack
	case w.Dodged > 0:
		return p.dodged && press <= uint64(p.dodgedSeq)+i.ticksFor(w.Dodged)+slack
	case len(w.Controlled) > 0:
		return i.arena.Controlled(uint64(target), w.Controlled, slack)
	default:
		lock, found := i.arena.At(uint64(target))
		return found && nav.Length(lock.X-p.state.X, lock.Z-p.state.Z) > w.Far-chainReach
	}
}
