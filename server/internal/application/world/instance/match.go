package instance

import (
	"cmp"
	"context"
	"encoding/binary"
	"fmt"
	"maps"
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
	teams    [2]*bots.Team           // what each side's bots share in a team mode; nil in 1v1
}

// fighter is a place in the match and how its fighter did.
type fighter struct {
	side  int // 0 or 1
	slot  int // the side's spot
	name  string
	class kernel.Class
	bot   *model.Bot
	// standsFor is the team fighter whose place a stand-in bot took when they left.
	standsFor kernel.CharacterID
	arrived   bool
	// bar is the arena bar a skill's key must be on; nil for a player whose build hasn't come, of whom no key is refused, and for a bot, which keeps to its own.
	bar []string
	// next is the newest build a player was sent, put on them at the next countdown; sent and revision say which they have.
	next     *model.Build
	sent     bool
	revision int
	left     bool // logged out, dropped or forfeited before the outcome
	gone     bool // off the match's map
	downs    int
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
	// buildMsg hands the match a player's arena build for its next round.
	buildMsg struct {
		id    kernel.CharacterID
		build model.Build
		done  chan error
	}
)

func (forfeitMsg) message() {}
func (toMatchMsg) message() {}
func (fightMsg) message()   {}
func (buildMsg) message()   {}

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
				mt.ring = bots.NewRing(tick(gamedata.BotMemory))
			}
		}
	}
	if m.Mode.Team > 1 {
		mt.teams = [2]*bots.Team{{}, {}}
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

// SetBuild hands the match a player's arena build, which they fight with from its next round's countdown; one older than a revision it has changes nothing, nor one of a player it doesn't have.
func (i *Instance) SetBuild(ctx context.Context, id kernel.CharacterID, b model.Build) error {
	m := buildMsg{id: id, build: b, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// setBuild answers a buildMsg.
func (i *Instance) setBuild(m buildMsg) {
	if f := i.match.fighters[m.id]; f != nil && (!f.sent || m.build.Revision > f.revision) {
		f.next, f.sent, f.revision = &m.build, true, m.build.Revision
	}
	m.done <- nil
}

// wearBuilds puts on each player the build they were sent since the last countdown: its bar and its traits, from this round.
func (i *Instance) wearBuilds() {
	for id, f := range i.match.fighters {
		if f.next == nil {
			continue
		}
		f.bar = f.next.Bar
		if p, ok := i.players[id]; ok {
			p.Skills.Traits = maps.Clone(f.next.Traits)
		}
		f.next = nil
	}
}

// onArenaBar refuses, in a match, a skill pressed on a key its fighter's arena bar doesn't hold: the key of a chain's root, or the skill's own.
func (i *Instance) onArenaBar(m skillMsg) error {
	if i.match == nil {
		return nil
	}
	if bar := i.match.fighters[m.id].bar; bar != nil && !slices.Contains(bar, cmp.Or(m.in.Root, m.in.Skill)) {
		return model.ErrNotOnArenaBar
	}
	return nil
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
	back := model.Return{MapID: i.m.ID, Channel: i.channel, Player: p.Player}
	back.Player.X, back.Player.Y, back.Player.Z, back.Player.Yaw = p.state.X, p.state.Y, p.state.Z, facing(p.state)
	back.Player.Timers = i.timersOf(p, time.Now())
	back.Player.Quests = p.Quests.Clone()
	moved := back.Player
	moved.Back = &back
	i.leave(p, target, m.spot.At.X, 0, m.spot.At.Z, float32(m.spot.Yaw), moved, Dest{ID: m.matchID})
	m.done <- nil
}

// pendingMatches hands off those waiting for an item call to settle.
func (i *Instance) pendingMatches() {
	for _, id := range slices.Clone(i.order) {
		if p := i.players[id]; p.toMatch != nil && p.itemCalls == 0 {
			m := *p.toMatch
			p.toMatch = nil
			i.goToMatch(m)
		}
	}
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

// welcome tells an arrived fighter the match and replaces their sheet and timers with the kit's; those already here hear the match again, as only an arrival names its fighter.
func (i *Instance) welcome(p *player) {
	st := i.matchState()
	for _, q := range i.players {
		q.private = append(q.private, st)
	}
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

// enterPhase moves the match to phase for d and tells everyone on it; a countdown puts on the builds sent since the last.
func (i *Instance) enterPhase(phase model.MatchPhase, d time.Duration) {
	if phase == model.PhaseCountdown {
		i.wearBuilds()
	}
	i.match.phase, i.match.ends = phase, i.tick+i.ticksFor(d)
	i.events = append(i.events, i.matchState())
}

// stepMatch runs the match's clock at the tick's start: prep and between into a countdown, the countdown into a round, a round out of time to its outcome, and the result out.
func (i *Instance) stepMatch() {
	mt := i.match
	if mt == nil || i.tick < mt.ends {
		return
	}
	switch mt.phase {
	case model.PhasePrep:
		var missing []kernel.CharacterID
		for _, side := range mt.order {
			for _, id := range side {
				if f := mt.fighters[id]; !f.arrived && f.bot == nil {
					f.left, f.gone = true, true // their transfer never landed
					missing = append(missing, id)
				}
			}
		}
		if i.someoneCame() {
			for _, id := range missing {
				i.standIn(id, nil)
			}
		}
		if i.settleLeavers() {
			return
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
		i.closeMatch()
	}
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
			if left := f.standsFor; !left.IsZero() {
				id, f = left, mt.fighters[left] // the slot is the leaver's: pvp rates them
			}
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

// settleLeavers ends the match for the other side once a side has no one left on the map or every player of it has left, and closes it with no outcome when it expected players and none came; a match of bots alone runs. True when it ended.
func (i *Instance) settleLeavers() bool {
	mt := i.match
	if mt.phase == model.PhaseResult {
		return false
	}
	gone := [2]bool{true, true}
	for _, f := range mt.fighters {
		if !f.gone {
			gone[f.side] = false
		}
	}
	switch {
	case mt.players && !i.someoneCame():
		i.closeMatch() // nobody to rate: no outcome
		return true
	case gone[0] || i.allLeft(0):
		i.endMatch(2)
		return true
	case gone[1] || i.allLeft(1):
		i.endMatch(1)
		return true
	}
	return false
}

// allLeft reports whether side s had players and every one has left: it concedes, as a 1v1 forfeit does, its stand-ins playing for nobody.
func (i *Instance) allLeft(s int) bool {
	had := false
	for _, f := range i.match.fighters {
		if f.side == s && f.bot == nil {
			if !f.left {
				return false
			}
			had = true
		}
	}
	return had
}

// someoneCame reports whether any player came to the match.
func (i *Instance) someoneCame() bool {
	for _, f := range i.match.fighters {
		if f.bot == nil && f.arrived {
			return true
		}
	}
	return false
}

// standIn has a bot of the spec's stand-in tier take a team fighter's place the tick they leave before the outcome: their body as it stands, with their class, name, HP and energy, or a fresh one at their spot for one who never came, down if they were at 0 HP; the tier's build for the class, as a bot plays from its order. Its arrival tells everyone the match with the slot now the bot's. Nothing outside a team mode, once decided, or for a side's last player, whose side concedes.
func (i *Instance) standIn(id kernel.CharacterID, body *player) {
	mt := i.match
	f := mt.fighters[id]
	if mt.Mode.Team <= 1 || mt.phase == model.PhaseResult || f.bot != nil || i.allLeft(f.side) {
		return
	}
	name, class := f.name, f.class
	if !f.arrived {
		name, class = i.freeName(), i.fewestOn(f.side)
	}
	builds := i.data.Pvp().Bots.Builds[class]
	build := builds[int(binary.BigEndian.Uint32(id[12:])%uint32(len(builds)))] //nolint:gosec // a handful of builds
	b := &model.Bot{ID: kernel.NewCharacterID(), Name: name, Class: class, Tier: i.tierOf(mt.Spec.StandIn).ID, Build: model.Build{Bar: build.Bar}}
	mt.fighters[b.ID] = &fighter{side: f.side, slot: f.slot, bot: b, standsFor: id}
	mt.order[f.side][f.slot] = b.ID
	if mt.ring == nil {
		mt.ring = bots.NewRing(i.ticksFor(gamedata.BotMemory))
	}
	look := model.BotLook(i.data.Pieces(), i.classOf(class).Weapon, b.ID)
	i.add(&joinMsg{player: model.Player{ID: b.ID, Name: name, Class: class, Look: look}, sink: botSink{}, done: make(chan error, 1)})
	p, ok := i.players[b.ID]
	if !ok {
		return
	}
	if body != nil {
		st := body.state
		st.VX, st.VY, st.VZ, st.Move, st.Held, st.Slow = 0, 0, 0, sim.Move{}, 0, sim.Slow{}
		p.state, p.aim = st, body.aim
		p.HP, p.Energy = min(body.HP, p.derived.MaxHP), body.Energy
		if body.dead || body.HP <= 0 {
			if !body.dead {
				f.downs++ // they fell this tick: the down is theirs
			}
			p.HP, p.dead, p.deadUntil = 0, true, math.MaxUint64
		}
		p.changed, p.moved = true, true
	}
}

// freeName is the first bot name no fighter in the match has.
func (i *Instance) freeName() string {
	names := i.data.Pvp().Bots.Names
	for _, n := range names {
		if !slices.ContainsFunc(slices.Collect(maps.Values(i.match.fighters)), func(f *fighter) bool { return f.name == n }) {
			return n
		}
	}
	return names[0]
}

// fewestOn is the bot class side s has the fewest of, the first in order on a tie.
func (i *Instance) fewestOn(s int) kernel.Class {
	classes := slices.Sorted(maps.Keys(i.data.Pvp().Bots.Builds))
	count := func(c kernel.Class) int {
		return len(slices.DeleteFunc(slices.Clone(i.match.order[s]), func(id kernel.CharacterID) bool { return i.match.fighters[id].class != c }))
	}
	return slices.MinFunc(classes, func(a, b kernel.Class) int { return cmp.Compare(count(a), count(b)) })
}

// classOf is a class by id.
func (i *Instance) classOf(id kernel.Class) gamedata.Class {
	c, _ := i.data.Class(id)
	return c
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
func (i *Instance) closeMatch() {
	for _, id := range slices.Clone(i.order) {
		i.goBack(i.players[id])
	}
	i.match.phase = model.PhaseResult
	i.ended.Store(true)
	i.cancel()
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
	i.leave(p, target, moved.X, moved.Y, moved.Z, moved.Yaw, moved, Dest{ID: target.ID, Channel: back.Channel}, i.worldStats(moved))
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
func (i *Instance) forfeit(m forfeitMsg) {
	p, ok := i.players[m.id]
	switch {
	case !ok || p.Connection != m.conn:
		m.done <- model.ErrNotInMatch
		return
	case i.match == nil:
		m.done <- model.ErrNotInMatch
		return
	}
	if i.match.phase != model.PhaseResult {
		i.match.fighters[p.ID].left = true
		i.standIn(p.ID, p)
	}
	i.goBack(p)
	i.settleLeavers()
	m.done <- nil
}

// leftMatch marks a fighter who left the map some other way than the outcome, logged out or dropped, body as they were; in a team mode a stand-in takes it.
func (i *Instance) leftMatch(id kernel.CharacterID, body *player) {
	if i.match == nil {
		return
	}
	if f, ok := i.match.fighters[id]; ok && !f.gone {
		f.gone = true
		if i.match.phase != model.PhaseResult {
			f.left = true
			i.standIn(id, body)
		}
		i.settleLeavers()
	}
}

// allies are who p's heals and party buffs reach: in a match, the live fighters on p's side; elsewhere, their party's members alive on the map.
func (i *Instance) allies(p *player) []*player {
	if i.match == nil {
		return i.present(p, i.deps.Parties.Party(p.ID))[1:]
	}
	var out []*player
	for _, id := range i.order {
		if a := i.players[id]; id != p.ID && !a.dead && i.side(id) == i.side(p.ID) {
			out = append(out, a)
		}
	}
	return out
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
