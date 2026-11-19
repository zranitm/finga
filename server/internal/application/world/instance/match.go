package instance

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"finga/internal/application/world/bots"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
	"finga/pkg/transient"
)

// Match is what makes an instance a match's: its spec, its mode, and where its outcome goes.
type Match struct {
	Spec    model.MatchSpec
	Mode    gamedata.PvpMode
	Results Results
}

// match is a match instance's state: its phase, rounds and fighters.
type match struct {
	Match
	rules    gamedata.PvpRules
	phase    model.MatchPhase
	round    int
	wins     [2]int
	ends     uint64 // the tick the phase ends
	started  time.Time
	fighters map[kernel.CharacterID]*fighter
	order    [2][]kernel.CharacterID // each side's, in the spec's order
	winner   int                     // once decided: 1 or 2, 0 a draw
	falling  []*player               // at 0 HP this tick: down once all the tick's hits have landed
	players  bool                    // the spec has players, not bots alone
	ring     *bots.Ring              // what its bots see; nil in a match with none
	seen     []bots.Seen             // the ring's frame as it's filled
}

// fighter is a place in the match and how its fighter did.
type fighter struct {
	side    int // 0 or 1
	slot    int // the side's spot
	name    string
	class   kernel.Class
	bot     *model.Bot
	arrived bool
	left    bool // logged out, dropped or forfeited before the outcome
	gone    bool // off the match's map
	downs   int
}

// The messages a match's players send besides the world's.
type (
	forfeitMsg struct {
		id   kernel.CharacterID
		conn string
		done chan error
	}
	// toMatchMsg hands a player on a world map to a match's instance.
	toMatchMsg struct {
		id      kernel.CharacterID
		matchID string // the match's instance, as the Mover finds it
		mapID   string // its map
		spot    gamedata.Spot
		done    chan error
	}
	// fightMsg asks whether a player could go into a match now.
	fightMsg struct {
		id   kernel.CharacterID
		done chan error
	}
)

func (forfeitMsg) message() {}
func (toMatchMsg) message() {}
func (fightMsg) message()   {}

func newMatch(m Match, rules gamedata.PvpRules, tick func(time.Duration) uint64) *match {
	mt := &match{Match: m, rules: rules, phase: model.PhasePrep, round: 1, ends: tick(rules.Rounds.Prep), started: time.Now(), fighters: map[kernel.CharacterID]*fighter{}}
	for s, side := range m.Spec.Sides {
		for k, f := range side {
			id := f.Character
			fr := &fighter{side: s, slot: k, bot: f.Bot}
			if f.Bot != nil {
				id, fr.name, fr.class = f.Bot.ID, f.Bot.Name, f.Bot.Class
			}
			mt.fighters[id] = fr
			mt.order[s] = append(mt.order[s], id)
			mt.players = mt.players || f.Bot == nil
			if f.Bot != nil && mt.ring == nil {
				mt.ring = bots.NewRing(int(tick(gamedata.BotMemory))) //nolint:gosec // a second's ticks
			}
		}
	}
	return mt
}

// Forfeit takes the player out of their match back to where they came from; outside one it's ErrNotInMatch.
func (i *Instance) Forfeit(ctx context.Context, id kernel.CharacterID, conn string) error {
	m := forfeitMsg{id: id, conn: conn, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// ToMatch hands the player to a match's instance at a spot on its map, once nothing in flight keeps them here.
func (i *Instance) ToMatch(ctx context.Context, id kernel.CharacterID, matchID, mapID string, spot gamedata.Spot) error {
	m := toMatchMsg{id: id, matchID: matchID, mapID: mapID, spot: spot, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// CanFight is nil when the player could go into a match now: on this map, alive, and on no match's.
func (i *Instance) CanFight(ctx context.Context, id kernel.CharacterID) error {
	m := fightMsg{id: id, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// IsMatch reports whether the instance runs a match.
func (i *Instance) IsMatch() bool { return i.match != nil }

// canFight answers a fightMsg.
func (i *Instance) canFight(m fightMsg) {
	p, ok := i.players[m.id]
	switch {
	case i.match != nil:
		m.done <- model.ErrInMatch
	case !ok:
		m.done <- fmt.Errorf("%w: the character is not on this map", transient.ErrUnavailable)
	case p.class.Parent == "":
		m.done <- model.ErrNotPromoted
	case p.dead:
		m.done <- model.ErrCharacterDead
	default:
		m.done <- nil
	}
}

// goToMatch hands the player to the match, keeping who they are here as their return point; the match counts one who can't go as gone.
func (i *Instance) goToMatch(m toMatchMsg) {
	p, ok := i.players[m.id]
	switch {
	case i.match != nil:
		m.done <- model.ErrInMatch
		return
	case !ok:
		m.done <- fmt.Errorf("%w: the character is not on this map", transient.ErrUnavailable)
		return
	case p.dead:
		m.done <- model.ErrCharacterDead
		return
	case p.toMatch != nil:
		m.done <- model.ErrInMatch // the first match keeps them
		return
	case p.itemCalls > 0:
		p.toMatch = &m // it goes once the call settles
		return
	}
	i.cancelTeleport(p, model.TeleportMoved)
	target, _ := i.data.Map(m.mapID)
	back := model.Return{MapID: i.m.ID, Player: p.Player}
	back.Player.X, back.Player.Y, back.Player.Z, back.Player.Yaw = p.state.X, p.state.Y, p.state.Z, facing(p.state)
	back.Player.Timers = i.timersOf(p, time.Now())
	back.Player.Quests = p.Quests.Clone()
	moved := back.Player
	moved.Back = &back
	i.leave(p, target, m.spot.At.X, 0, m.spot.At.Z, float32(m.spot.Yaw), moved, m.matchID)
	m.done <- nil
}

// pendingMatches hands off those waiting for an item call to settle.
func (i *Instance) pendingMatches() []kernel.CharacterID {
	var left []kernel.CharacterID
	for _, id := range slices.Clone(i.order) {
		if p := i.players[id]; p.toMatch != nil && p.itemCalls == 0 {
			m := *p.toMatch
			p.toMatch = nil
			i.goToMatch(m)
			if _, still := i.players[id]; !still {
				left = append(left, id)
			}
		}
	}
	return left
}

// kitted is pl as the match fights: the class's kit at its level, every skill it learns at its last stage with its traits, full HP and energy, at its side's spot, with no timers.
func (i *Instance) kitted(pl model.Player, f *fighter) model.Player {
	class, _ := i.data.Class(pl.Class)
	kit, _ := i.data.Kit(pl.Class)
	spot := i.m.Sides[f.side][f.slot]
	traits := pl.Skills.Traits
	if f.bot != nil {
		traits = f.bot.Build.Traits
	}
	pl.Level, pl.Exp = kit.Level, 0
	pl.Points = model.Points{Spent: subStats(kit.Own, i.data.StartClass().BaseStats)}
	pl.Skills = model.Skills{Points: map[string]int{}, Traits: map[string]string{}}
	for _, s := range i.data.Skills() {
		if !class.Learnable(s) || s.Granted {
			continue
		}
		pl.Skills.Points[s.ID] = max(s.Stages[len(s.Stages)-1].At, 1)
		if t, ok := traits[s.ID]; ok {
			pl.Skills.Traits[s.ID] = t
		}
	}
	pl.Worn = model.Worn{}
	for _, w := range kit.Worn {
		it, _ := i.data.Item(w.Item)
		pl.Worn.Items = append(pl.Worn.Items, model.WornItem{Template: it.ID, Stats: progression.Rolled(it.Rolls, func(r gamedata.Range) int { return (r.Min + r.Max) / 2 })})
	}
	pl.HP, pl.Energy = math.MaxInt32, i.data.Energy().Max
	pl.X, pl.Y, pl.Z, pl.Yaw = spot.At.X, 0, spot.At.Z, float32(spot.Yaw)
	pl.Timers = model.SkillTimers{}
	return pl
}

func subStats(a, b gamedata.Stats) gamedata.Stats {
	return gamedata.Stats{STR: a.STR - b.STR, DEX: a.DEX - b.DEX, INT: a.INT - b.INT, AGI: a.AGI - b.AGI, VIT: a.VIT - b.VIT}
}

// fighting is whether the match lets its fighters attack and use skills: in a round, not before or after.
func (i *Instance) fighting() bool { return i.match == nil || i.match.phase == model.PhaseFight }

// walled is whether the prep wall stands: before a round starts.
func (i *Instance) walled() bool {
	return i.match != nil && (i.match.phase == model.PhasePrep || i.match.phase == model.PhaseCountdown)
}

// geometry is the map's solids, with the prep wall while it stands.
func (i *Instance) geometry() sim.Geometry {
	if i.walled() {
		return i.m.Walled
	}
	return i.m.Geometry
}

// joinMatch is a fighter's arrival: kitted at their side's spot, told the match and their sheet; false for one the match doesn't have, or has had: arrived already, or gone.
func (i *Instance) joinMatch(j *joinMsg) bool {
	f, ok := i.match.fighters[j.player.ID]
	if !ok || f.arrived || f.gone || i.match.phase == model.PhaseResult || f.bot == nil && j.player.Back == nil {
		return false
	}
	f.arrived, f.name, f.class = true, j.player.Name, j.player.Class
	j.player = i.kitted(j.player, f)
	return true
}

// welcome tells an arrived fighter the match and replaces their sheet and timers with the kit's.
func (i *Instance) welcome(p *player) {
	p.private = append(p.private, i.matchState())
	i.retime(p)
}

// retime tells the player their sheet and timers, which replace the ones the client holds.
func (i *Instance) retime(p *player) {
	s := i.stats(p)
	p.derived = s.Derived
	t := i.timersOf(p, time.Now())
	p.private = append(p.private, model.StatsChanged{Tick: i.tick, Sheet: &s.Sheet, HP: p.HP, MaxHP: p.derived.MaxHP, Timers: &t})
}

// matchState is the match as its fighters see it now.
func (i *Instance) matchState() model.MatchState {
	mt := i.match
	st := model.MatchState{Tick: i.tick, Match: mt.Spec.ID, Mode: mt.Mode.ID, Phase: mt.phase, Round: mt.round, Wins: mt.wins}
	if mt.ends > i.tick {
		st.EndsIn = time.Duration(mt.ends-i.tick) * i.tuning.TickInterval
	}
	for s, side := range mt.order {
		for _, id := range side {
			f := mt.fighters[id]
			st.Sides[s] = append(st.Sides[s], model.MatchFighter{ID: id, Name: f.name, Class: f.class, Bot: f.bot != nil})
		}
	}
	return st
}

// enterPhase moves the match to phase for d and tells everyone on it.
func (i *Instance) enterPhase(phase model.MatchPhase, d time.Duration) {
	i.match.phase, i.match.ends = phase, i.tick+i.ticksFor(d)
	i.events = append(i.events, i.matchState())
}

// stepMatch runs the match's clock at the tick's start: prep and between into a countdown, the countdown into a round, a round out of time to its outcome, and the result out.
func (i *Instance) stepMatch() []kernel.CharacterID {
	mt := i.match
	if mt == nil || i.tick < mt.ends {
		return nil
	}
	switch mt.phase {
	case model.PhasePrep:
		for _, f := range mt.fighters {
			if !f.arrived && f.bot == nil {
				f.left, f.gone = true, true // their transfer never landed
			}
		}
		if i.settleLeavers() {
			return nil
		}
		i.enterPhase(model.PhaseCountdown, mt.rules.Rounds.Countdown)
	case model.PhaseCountdown:
		i.enterPhase(model.PhaseFight, mt.Mode.Rounds.Length)
	case model.PhaseFight:
		i.endRound(i.onTime())
	case model.PhaseBetween:
		mt.round++
		i.resetRound()
		i.enterPhase(model.PhaseCountdown, mt.rules.Rounds.Countdown)
	case model.PhaseResult:
		return i.closeMatch()
	}
	return nil
}

// judgeRound ends the round when a whole side is down or gone: the other side takes it, and an even round when both are.
func (i *Instance) judgeRound() {
	if i.match == nil || i.match.phase != model.PhaseFight {
		return
	}
	out := [2]bool{i.sideOut(0), i.sideOut(1)}
	switch {
	case out[0] && out[1]:
		i.endRound(0)
	case out[0]:
		i.endRound(2)
	case out[1]:
		i.endRound(1)
	}
}

// sideOut is whether every fighter of side s is down or gone.
func (i *Instance) sideOut(s int) bool {
	for _, id := range i.match.order[s] {
		if p, ok := i.players[id]; ok && !p.dead && !i.match.fighters[id].gone {
			return false
		}
	}
	return true
}

// onTime is a round's winner on time: the side with the larger share of its HP, 0 for equal shares.
func (i *Instance) onTime() int {
	var share [2]float64
	for s, side := range i.match.order {
		hp, most := 0, 0
		for _, id := range side {
			if p, ok := i.players[id]; ok && !i.match.fighters[id].gone {
				hp, most = hp+p.HP, most+p.derived.MaxHP
			}
		}
		if most > 0 {
			share[s] = float64(hp) / float64(most)
		}
	}
	switch {
	case share[0] > share[1]:
		return 1
	case share[1] > share[0]:
		return 2
	}
	return 0
}

// endRound gives the round to winner (0 for none), then ends the match when a side has won enough rounds or the most are played, else waits between rounds.
func (i *Instance) endRound(winner int) {
	mt := i.match
	if winner > 0 {
		mt.wins[winner-1]++
	}
	need := mt.Mode.Rounds.BestOf/2 + 1
	switch {
	case mt.wins[0] >= need:
		i.endMatch(1)
	case mt.wins[1] >= need:
		i.endMatch(2)
	case mt.round >= mt.Mode.Rounds.Most:
		i.endMatch(0)
	default:
		i.enterPhase(model.PhaseBetween, mt.rules.Rounds.Between)
	}
}

// endMatch decides the match for winner, 0 a draw, reports it, and shows the outcome.
func (i *Instance) endMatch(winner int) {
	mt := i.match
	mt.winner = winner
	r := model.MatchResult{Match: mt.Spec.ID, Mode: mt.Mode.ID, Started: mt.started, Ended: time.Now(), Winner: winner, Wins: mt.wins}
	for s, side := range mt.order {
		for _, id := range side {
			f := mt.fighters[id]
			fr := model.FighterResult{Left: f.left, Downs: f.downs}
			if f.bot != nil {
				fr.Tier = f.bot.Tier
			} else {
				fr.Character = id
			}
			r.Sides[s] = append(r.Sides[s], fr)
		}
	}
	mt.Results.Ended(r)
	i.enterPhase(model.PhaseResult, mt.rules.Result)
}

// settleLeavers ends the match for the other side once a side has no one left on the map, and closes it with no outcome when it expected players and none came; a match of bots alone runs. True when it ended.
func (i *Instance) settleLeavers() bool {
	mt := i.match
	if mt.phase == model.PhaseResult {
		return false
	}
	gone := [2]bool{true, true}
	came := false
	for _, f := range mt.fighters {
		if !f.gone {
			gone[f.side] = false
		}
		if f.bot == nil && f.arrived {
			came = true
		}
	}
	switch {
	case mt.players && !came:
		i.closeMatch() // nobody to rate: no outcome
		return true
	case gone[0]:
		i.endMatch(2)
		return true
	case gone[1]:
		i.endMatch(1)
		return true
	}
	return false
}

// resetRound puts every fighter back at their spot, up, with full HP and energy, and nothing running on them: no cooldown, buff, shield, status, dodge, block or action.
func (i *Instance) resetRound() {
	for _, id := range slices.Clone(i.order) {
		p := i.players[id]
		f := i.match.fighters[id]
		spot := i.m.Sides[f.side][f.slot]
		if w := p.waitingCast; w != nil {
			w.done <- model.ErrMatchNotStarted
			p.waitingCast = nil
		}
		p.dead, p.deadUntil = false, 0
		p.action, p.windUps, p.attackQueued = action{}, nil, false
		p.moveMark, p.dodgeUntil, p.blockUntil, p.invulnUntil = nil, 0, 0, 0
		p.statuses.Clear()
		p.skillReady, p.buffs = map[string]uint64{}, map[string]buff{}
		p.held, p.pending = sim.Input{}, p.pending[:0]
		p.state = i.standing(spot.At.X, 0, spot.At.Z, float32(spot.Yaw))
		p.aim = float32(spot.Yaw)
		p.Energy, p.spentAhead = i.data.Energy().Max, nil
		p.derived = i.derive(p)
		p.HP = p.derived.MaxHP
		p.changed, p.moved = true, true
		i.events = append(i.events, model.Respawned{Tick: i.tick, ID: p.ID, MapID: i.m.ID, X: p.state.X, Y: p.state.Y, Z: p.state.Z, HP: p.HP, Yaw: float32(spot.Yaw)})
		i.retime(p)
	}
}

// closeMatch takes everyone back to where they came from and stops the instance.
func (i *Instance) closeMatch() []kernel.CharacterID {
	left := slices.Clone(i.order)
	for _, id := range left {
		i.goBack(i.players[id])
	}
	i.match.phase = model.PhaseResult
	i.ended.Store(true)
	i.cancel()
	return left
}

// goBack hands a player in a match back to their return point as they were there, their timers counting on; a bot just goes.
func (i *Instance) goBack(p *player) {
	f := i.match.fighters[p.ID]
	f.gone = true
	if p.bot != nil {
		i.detach(p.ID)
		return
	}
	back := *p.Back
	target, _ := i.data.Map(back.MapID)
	moved := back.Player
	moved.Connection = p.Connection
	i.leave(p, target, moved.X, moved.Y, moved.Z, moved.Yaw, moved, target.ID, i.worldStats(moved))
}

// worldStats is the sheet, HP and timers of pl back on a world map, which replace the match's.
func (i *Instance) worldStats(pl model.Player) model.StatsChanged {
	class, _ := i.data.Class(pl.Class)
	now := time.Now()
	s := StatsOf(i.data, class, pl.Level, pl.Points, pl.Skills, pl.Worn, BuffsOf(i.data, pl.Timers, now), i.tuning.TickInterval)
	s.Sheet.Buffs = BuffViewsOf(pl.Timers, pl.ID, now)
	t := pl.Timers.Clone()
	return model.StatsChanged{Tick: i.tick, Sheet: &s.Sheet, HP: min(pl.HP, s.Derived.MaxHP), MaxHP: s.Derived.MaxHP, Timers: &t}
}

// forfeit takes the player out of the match: before the outcome it's a loss, which ends a match whose side has no one left.
func (i *Instance) forfeit(m forfeitMsg) []kernel.CharacterID {
	p, ok := i.players[m.id]
	switch {
	case !ok || p.Connection != m.conn:
		m.done <- model.ErrNotInMatch
		return nil
	case i.match == nil:
		m.done <- model.ErrNotInMatch
		return nil
	}
	if i.match.phase != model.PhaseResult {
		i.match.fighters[p.ID].left = true
	}
	i.goBack(p)
	i.settleLeavers()
	m.done <- nil
	return []kernel.CharacterID{m.id}
}

// leftMatch marks a fighter who left the map some other way than the outcome: logged out or dropped.
func (i *Instance) leftMatch(id kernel.CharacterID) {
	if i.match == nil {
		return
	}
	if f, ok := i.match.fighters[id]; ok && !f.gone {
		f.gone = true
		if i.match.phase != model.PhaseResult {
			f.left = true
		}
		i.settleLeavers()
	}
}

// fighterOf is id's place in the match; false outside one, nil-safe.
func (m *match) fighterOf(id kernel.CharacterID) (*fighter, bool) {
	if m == nil {
		return nil, false
	}
	f, ok := m.fighters[id]
	return f, ok
}

// fall marks p at 0 HP; they go down once every hit of the tick has landed, so two who down each other on one tick both go down, an even round.
func (i *Instance) fall(p *player) {
	if !slices.Contains(i.match.falling, p) {
		i.match.falling = append(i.match.falling, p)
	}
}

// settleDowns downs those who fell this tick and are still at 0 HP, in the order they fell.
func (i *Instance) settleDowns() {
	if i.match == nil {
		return
	}
	for _, p := range i.match.falling {
		if i.players[p.ID] == p && !p.dead && p.HP <= 0 {
			i.down(p)
		}
	}
	i.match.falling = i.match.falling[:0]
}

// scaled is a fighter's damage on another in a match, by scale.damage and their class's kit's damage factor.
func (i *Instance) scaled(by kernel.CharacterID, amount float64) int {
	factor := i.match.rules.Scale.Damage
	if f, ok := i.match.fighters[by]; ok {
		factor *= i.match.rules.Kits[f.class].Damage
	}
	return int(math.Round(amount * factor))
}

// down is a fighter at 0 HP in a match: out of the round with their action, statuses and buffs gone, as a death, but with no penalty and no respawn; the round's reset brings them back.
func (i *Instance) down(p *player) {
	p.HP, p.dead, p.deadUntil = 0, true, math.MaxUint64
	p.held, p.pending = sim.Input{}, p.pending[:0]
	if len(p.buffs) > 0 {
		clear(p.buffs)
		i.restat(p)
	}
	p.action, p.windUps = action{}, nil
	p.moveMark, p.state.Move, p.dodgeUntil, p.blockUntil = nil, sim.Move{}, 0, 0
	p.statuses.Clear()
	p.state.Held, p.state.Slow = 0, sim.Slow{}
	p.changed = true
	i.match.fighters[p.ID].downs++
	i.events = append(i.events, model.Died{Tick: i.tick, ID: p.ID, X: p.state.X, Y: p.state.Y, Z: p.state.Z})
}

// side is the player's side in the match, 1 or 2; 0 outside one.
func (i *Instance) side(id kernel.CharacterID) int {
	if i.match == nil {
		return 0
	}
	if f, ok := i.match.fighters[id]; ok {
		return f.side + 1
	}
	return 0
}

// enemies reports whether a and b fight each other: in a match, players on the other side; elsewhere never.
func (i *Instance) enemies(a, b kernel.CharacterID) bool {
	sa, sb := i.side(a), i.side(b)
	return sa != 0 && sb != 0 && sa != sb
}

// refused answers done with ErrInMatch in a match, for what a match doesn't allow: items, gear, points, traits and teleports.
func (i *Instance) refused(done chan error) bool {
	if i.match == nil {
		return false
	}
	done <- model.ErrInMatch
	return true
}
