package world

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/quests"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
)

// Join takes the character off any map it is on first, so a login elsewhere or a rejoin never starts at an older save; it holds the character's lock throughout.
func (w *World) Join(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink instance.Sink) (model.View, error) {
	unlock, err := w.locks.Lock(ctx, id)
	if err != nil {
		return model.View{}, err
	}
	defer unlock()
	if v, err := w.rejoinMatch(ctx, account, id, conn, sink); !errors.Is(err, model.ErrNotInMatch) {
		return v, err
	}
	older, took, err := w.takeOff(ctx, id, func(p presence) bool { return p.account == account })
	var v model.View
	if err == nil {
		v, err = w.join(ctx, account, id, conn, sink)
	}
	if err != nil && took {
		older.sink.Send(model.Detached{}) // the gateway joins the older connection again, on a map or mid-transfer
	}
	return v, err
}

// rejoinMatch puts conn on the character's body in a match, its place held through a drop or still connected, as a page reload's Hello can come before its old connection's close (pvp.md section 4); ErrNotInMatch when no match holds it, for a plain join.
func (w *World) rejoinMatch(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink instance.Sink) (model.View, error) {
	older, ok := w.presences.get(id)
	if _, fighting := w.fighting.of(id); !ok || !fighting || older.account != account { // as the join loads no character, only its account's presence is taken
		return model.View{}, model.ErrNotInMatch
	}
	at := w.presences.place(id, presence{inst: older.inst, conn: conn, account: account, sink: sink})
	answerCtx, cancel := answerOf(ctx)
	defer cancel()
	var v model.View
	err := whileBusy(answerCtx, func() (err error) {
		v, err = older.inst.Rejoin(answerCtx, id, conn, sink)
		return err
	})
	switch {
	case errors.Is(err, instance.ErrSinkClosed):
		return model.View{}, model.ErrBusy // held again through conn, as the presence says; the gateway joins on its next stream
	case errors.Is(err, model.ErrStopped):
		w.presences.forget(id, at.gen) // the match stopped since the read and holds nobody, so the plain join lands them where they came from
		return model.View{}, model.ErrNotInMatch
	case err != nil:
		w.presences.place(id, older) // for the plain join to take off
		return model.View{}, err
	}
	v.JoinSeq = at.gen
	return v, nil
}

func (w *World) join(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink instance.Sink) (model.View, error) {
	unsaved, pending := w.saves.Get(id) // before the load, so a save that lands between them is not missed
	c, err := w.load(ctx, id, account)
	if err != nil {
		return model.View{}, err
	}
	if pending {
		c = withCheckpoint(c, unsaved)
	}
	handedIn, err := w.handedIn(ctx, id, c.Quests)
	if err != nil {
		return model.View{}, err
	}
	w.grantStarter(ctx, account, id)
	worn, err := w.worn(ctx, id) // after the grant, so starter equipment counts
	if err != nil {
		return model.View{}, err
	}
	look, err := w.look(ctx, id)
	if err != nil {
		return model.View{}, err
	}
	class, ok := w.data.Class(c.Class)
	if !ok {
		return model.View{}, model.ErrClassUnavailable
	}
	now := time.Now()
	stats := instance.StatsOf(w.data, class, c.Level, c.Points, c.Skills, worn, instance.BuffsOf(w.data, c.Timers, now), w.tick)
	stats.Sheet.Buffs = instance.BuffViewsOf(c.Timers, c.ID, now)
	derived := stats.Derived
	if c.HP <= 0 {
		if c, err = w.respawned(c, derived.MaxHP); err != nil {
			return model.View{}, err
		}
	}
	inst, landed, err := w.mapNamed(id, c.MapID)
	if err != nil {
		return model.View{}, err
	}
	defer landed()
	c = w.placed(ctx, c)
	log := c.Quests.Clone() // the view's: from the join on, the map's tick changes the player's
	p := model.Player{ID: c.ID, Account: account, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp, Points: c.Points, Skills: c.Skills, HP: c.HP, Energy: c.Energy, X: c.X, Y: c.Y, Z: c.Z, Yaw: c.Yaw, Connection: conn, Worn: worn, Look: look, Quests: c.Quests, Timers: running(c.Timers, now), Waypoints: c.Waypoints.Clone(), HandedIn: handedIn}
	at := w.presences.place(id, presence{inst: inst, conn: conn, account: account, sink: sink})
	answerCtx, cancel := answerOf(ctx)
	defer cancel()
	err = inst.Join(answerCtx, p, sink)
	switch {
	case err == nil && ctx.Err() != nil:
		w.undo(ctx, id, at) // the caller counts the join failed, so the map must not keep it
		return model.View{}, ctx.Err()
	case err == nil:
	case answerCtx.Err() != nil:
		w.leaveBehind(inst, id, conn) // posted, and the map has not answered: it may still land
		w.presences.forget(id, at.gen)
		return model.View{}, cmp.Or(ctx.Err(), model.ErrBusy)
	default:
		w.presences.forget(id, at.gen)
		if errors.Is(err, instance.ErrSinkClosed) {
			err = model.ErrBusy // the gateway's stream is gone; it joins again on the next one
		}
		return model.View{}, err
	}
	return model.View{
		ID: c.ID, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp, Stats: stats.Own,
		HP: min(c.HP, derived.MaxHP), MaxHP: derived.MaxHP, Energy: min(max(c.Energy, 0), w.data.Energy().Max),
		MapID: c.MapID, Sheet: stats.Sheet, JoinSeq: at.gen, Quests: log, Timers: running(c.Timers, now), Waypoints: c.Waypoints.Clone(),
	}, nil
}

// running is the timers in t still running at now.
func running(t model.SkillTimers, now time.Time) model.SkillTimers {
	out := t.Clone()
	maps.DeleteFunc(out.Cooldowns, func(_ string, until time.Time) bool { return !until.After(now) })
	maps.DeleteFunc(out.Buffs, func(_ string, b model.BuffTimer) bool { return !b.Until.After(now) })
	return out
}

func (w *World) worn(ctx context.Context, id kernel.CharacterID) (model.Worn, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return w.deps.Wardrobe.Worn(ctx, id)
}

func (w *World) look(ctx context.Context, id kernel.CharacterID) (model.Look, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return w.deps.Wardrobe.Look(ctx, id)
}

// handedIn asks the item module about the next completion of each active quest with items to take or give, so one handed in while the world missed the reply still finishes.
func (w *World) handedIn(ctx context.Context, id kernel.CharacterID, log model.Quests) ([]model.HandedIn, error) {
	var asked []model.Completion
	for _, quest := range slices.Sorted(maps.Keys(log)) {
		q, ok := w.data.Quest(quest)
		if entry := log[quest]; ok && entry.Status == model.QuestActive && quests.HandsItems(q) {
			asked = append(asked, model.Completion{Quest: quest, Completion: entry.TimesFinished + 1})
		}
	}
	if len(asked) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return w.deps.HandedIns.HandedIn(ctx, id, asked)
}

func (w *World) load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return w.deps.Characters.Load(ctx, id, account)
}

// grantStarter is a safety net for a creation whose grant failed: a failure is logged, and the next join tries again.
func (w *World) grantStarter(ctx context.Context, account kernel.AccountID, id kernel.CharacterID) {
	ctx, cancel := context.WithTimeout(ctx, starterTimeout)
	defer cancel()
	if err := w.deps.Starter.GrantStarter(ctx, account, id); err != nil {
		w.deps.Logger.WarnContext(ctx, "starter items not granted; the next join tries again", "character_id", id, "err", err)
	}
}

// answerOf is for waiting on a map's answer past ctx's end, up to answerTimeout, so the lock's holder knows what the map did before it lets go.
func answerOf(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), answerTimeout)
}

// takeOff takes the character off its map, if want accepts its presence, and forgets the presence; it returns the one it took. The caller holds the character's lock.
func (w *World) takeOff(ctx context.Context, id kernel.CharacterID, want func(presence) bool) (presence, bool, error) {
	p, ok := w.presences.get(id)
	if !ok || !want(p) {
		return presence{}, false, nil
	}
	if !p.transit { // in transit no map holds it, and its landing waits for this lock
		if err := w.leaveMap(ctx, id, p); err != nil {
			return presence{}, false, err // the presence stays, for the next holder to take off
		}
	}
	w.presences.forgetThrough(id, p.conn) // the tick may have handed it off since
	return p, true, ctx.Err()
}

// leaveMap has p's map take the character off, and waits until it has and its checkpoint is queued.
func (w *World) leaveMap(ctx context.Context, id kernel.CharacterID, p presence) error {
	left := make(chan struct{})
	if err := whileBusy(ctx, func() error { return p.inst.Leave(id, p.conn, left) }); err != nil {
		return err
	}
	answerCtx, cancel := answerOf(ctx)
	defer cancel()
	return p.inst.Wait(answerCtx, left)
}

// undo takes off a join that landed though its caller gave up; if the map does not answer, the presence stays for the next holder.
func (w *World) undo(ctx context.Context, id kernel.CharacterID, at presence) {
	answerCtx, cancel := answerOf(ctx)
	defer cancel()
	if w.leaveMap(answerCtx, id, at) == nil {
		w.presences.forgetThrough(id, at.conn)
	}
}

// leaveBehind posts a leave behind a join the map has not answered, which may still land; the inbox keeps them in order.
func (w *World) leaveBehind(inst *instance.Instance, id kernel.CharacterID, conn string) {
	postCtx, cancel := context.WithTimeout(w.jobsCtx, answerTimeout)
	defer cancel()
	_ = whileBusy(postCtx, func() error { return inst.Leave(id, conn, nil) })
}

// whileBusy runs try until it answers anything but ErrBusy, backing off between tries, or until ctx ends.
func whileBusy(ctx context.Context, try func() error) error {
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		err := try()
		if err != nil && !errors.Is(err, model.ErrBusy) {
			return struct{}{}, backoff.Permanent(err)
		}
		return struct{}{}, err
	}, backoff.WithBackOff(busyBackOff()), backoff.WithMaxElapsedTime(0))
	return err
}

// respawned is c, saved dead before dead players were saved as respawned, as the respawn it was owed, where it would have got up.
func (w *World) respawned(c model.Character, maxHP int) (model.Character, error) {
	m, ok := w.data.Map(c.MapID)
	if !ok {
		return c, model.ErrNoMap
	}
	town, up, yaw := w.data.GetUp(m, c.X, c.Z)
	c.MapID, c.X, c.Y, c.Z, c.Yaw, c.HP = town.ID, up.X, 0, up.Z, float32(yaw), instance.RespawnHP(maxHP)
	return c, nil
}

// placed starts c at its map's spawn when its saved position is off the map, in a solid or NaN; the map stands it on the ground.
func (w *World) placed(ctx context.Context, c model.Character) model.Character {
	m, _ := w.data.Map(c.MapID)
	if m.Holds(c.X, c.Y, c.Z, w.data.Movement().Step) {
		return c
	}
	w.deps.Logger.WarnContext(ctx, "a saved position the map can't hold; the character starts at its spawn", "character_id", c.ID, "map_id", c.MapID, "x", c.X, "y", c.Y, "z", c.Z)
	c.X, c.Y, c.Z, c.Yaw = m.PlayerSpawn.X, 0, m.PlayerSpawn.Z, float32(m.SpawnYaw)
	return c
}

// withCheckpoint is c as cp left it; the skills, quests and waypoints are copies, as the saver may still read cp's.
func withCheckpoint(c model.Character, cp model.Checkpoint) model.Character {
	c.Class, c.MapID, c.X, c.Y, c.Z, c.Yaw, c.HP, c.Energy, c.Level, c.Exp, c.Points, c.Skills, c.Quests, c.Timers = cp.Class, cp.MapID, cp.X, cp.Y, cp.Z, cp.Yaw, cp.HP, cp.Energy, cp.Level, cp.Exp, cp.Points, cp.Skills.Clone(), cp.Quests.Clone(), cp.Timers.Clone()
	c.Waypoints = cp.Waypoints.Clone()
	return c
}

func busyBackOff() *backoff.ExponentialBackOff {
	return &backoff.ExponentialBackOff{InitialInterval: busyRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: busyRetryMax}
}

// Offline runs do if the character is on no map and no save of it is pending or under way, holding its lock so no join or leave starts before do returns; ran is false, and do not run, while the world holds the character.
func (w *World) Offline(ctx context.Context, id kernel.CharacterID, do func(ctx context.Context) error) (ran bool, err error) {
	unlock, err := w.locks.Lock(ctx, id)
	if err != nil {
		return false, err
	}
	defer unlock()
	if _, on := w.presences.get(id); on {
		return false, nil
	}
	if _, pending := w.saves.Get(id); pending { // a map queues the checkpoint before it forgets the presence, so the two leave no gap
		return false, nil
	}
	return true, do(ctx)
}
