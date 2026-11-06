package world

import (
	"context"
	"errors"

	"github.com/cenkalti/backoff/v5"

	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
)

// Join takes the character off any map it is on first, so a login elsewhere or a rejoin never starts at an older save.
func (w *World) Join(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink instance.Sink) (model.View, error) {
	older, took, err := w.depart(ctx, id, func(p presence) bool { return p.account == account })
	if err != nil {
		return model.View{}, err
	}
	v, err := w.join(ctx, account, id, conn, sink)
	if err != nil && took {
		older.sink.Send(model.Detached{}) // the gateway joins the older connection again, on a map or mid-transfer
	}
	return v, err
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
	w.grantStarter(ctx, account, id, c.Class)
	formulas, ok := w.data.Formulas(c.Class)
	if !ok {
		return model.View{}, model.ErrClassUnavailable
	}
	derived := progression.DeriveStats(formulas, c.Stats, c.Level, progression.Equipment{})
	if c.HP <= 0 {
		if c, err = w.respawned(c, derived.MaxHP); err != nil {
			return model.View{}, err
		}
	}
	inst, err := w.mapNamed(c.MapID)
	if err != nil {
		return model.View{}, err
	}
	p := model.Player{ID: c.ID, Account: account, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp, Stats: c.Stats, HP: c.HP, MP: c.MP, X: c.X, Y: c.Y, Connection: conn}
	at := presence{inst: inst, conn: conn, account: account, sink: sink}
	w.presences.place(id, at)
	if err := inst.Join(ctx, p, sink); err != nil {
		w.presences.forget(id, at)
		if ctx.Err() != nil {
			w.leaveLater(inst, id, conn) // posted, and may still land, though the caller counts it failed
		}
		if errors.Is(err, instance.ErrSinkClosed) {
			err = model.ErrBusy // the gateway's stream is gone; it joins again on the next one
		}
		return model.View{}, err
	}
	return model.View{
		ID: c.ID, Name: c.Name, Class: c.Class, Level: c.Level, Exp: c.Exp, Stats: c.Stats,
		HP: min(c.HP, derived.MaxHP), MaxHP: derived.MaxHP, MP: min(c.MP, derived.MaxMP), MaxMP: derived.MaxMP,
		MapID: c.MapID, X: c.X, Y: c.Y,
	}, nil
}

func (w *World) load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return w.deps.Characters.Load(ctx, id, account)
}

// grantStarter is a safety net for a creation whose grant failed: a failure is logged, and the next join tries again.
func (w *World) grantStarter(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, class kernel.Class) {
	ctx, cancel := context.WithTimeout(ctx, starterTimeout)
	defer cancel()
	if err := w.deps.Starter.GrantStarter(ctx, account, id, class); err != nil {
		w.deps.Logger.WarnContext(ctx, "starter items not granted; the next join tries again", "character_id", id, "err", err)
	}
}

// depart takes the character off its map if want accepts it, behind a tombstone that holds off transfers and departs; it returns the presence it took.
func (w *World) depart(ctx context.Context, id kernel.CharacterID, want func(presence) bool) (presence, bool, error) {
	for {
		left := make(chan struct{})
		p, ok := w.presences.depart(id, want, left)
		if !ok {
			return presence{}, false, nil
		}
		if p.left != nil {
			if err := p.inst.Wait(ctx, p.left); err != nil {
				return presence{}, false, err
			}
			w.presences.forget(id, p)
			continue
		}
		tomb := p
		tomb.left = left
		if err := whileBusy(ctx, func() error { return p.inst.Leave(id, p.conn, left) }); err != nil {
			w.presences.restore(id, tomb, p)
			close(left) // never posted; whoever waits on the tombstone looks again
			return presence{}, false, err
		}
		if err := p.inst.Wait(ctx, left); err != nil {
			w.forgetLater(id, tomb) // the leave lands later, and its tombstone goes then
			return presence{}, false, err
		}
		w.presences.forget(id, tomb)
		return p, true, nil
	}
}

// leaveLater posts a leave for a join that may still land, retrying while the map is busy, until the world stops.
func (w *World) leaveLater(inst *instance.Instance, id kernel.CharacterID, conn string) {
	w.background.Go(func() { _ = whileBusy(w.jobs, func() error { return inst.Leave(id, conn, nil) }) })
}

// forgetLater drops a tombstone once the map has handled its leave, for a departure whose caller stopped waiting.
func (w *World) forgetLater(id kernel.CharacterID, tomb presence) {
	w.background.Go(func() {
		if tomb.inst.Wait(w.jobs, tomb.left) == nil {
			w.presences.forget(id, tomb)
		}
	})
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

// respawned is c, saved dead before dead players were saved as respawned, as the respawn it was owed.
func (w *World) respawned(c model.Character, maxHP int) (model.Character, error) {
	m, ok := w.data.Map(c.MapID)
	if !ok {
		return c, model.ErrNoMap
	}
	town, _ := w.data.Map(m.Town)
	c.MapID, c.X, c.Y, c.HP = town.ID, town.PlayerSpawn.X, town.PlayerSpawn.Y, instance.RespawnHP(maxHP)
	return c, nil
}

// withCheckpoint is c as cp left it.
func withCheckpoint(c model.Character, cp model.Checkpoint) model.Character {
	c.MapID, c.X, c.Y, c.HP, c.MP, c.Level, c.Exp, c.Stats = cp.MapID, cp.X, cp.Y, cp.HP, cp.MP, cp.Level, cp.Exp, cp.Stats
	return c
}

func busyBackOff() *backoff.ExponentialBackOff {
	return &backoff.ExponentialBackOff{InitialInterval: busyRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: busyRetryMax}
}
