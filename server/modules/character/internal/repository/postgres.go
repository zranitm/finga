// Package repository is the character service's Postgres store.
package repository

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/modules/character/internal/application"
	"finga/modules/character/internal/model"
	"finga/modules/character/internal/repository/internal/db"
	"finga/pkg/dbx"
)

// Postgres is the service's store.
type Postgres struct {
	pool *pgxpool.Pool
	q    *db.Queries
	obs  dbx.Observer // hears each transaction; nil hears none
}

// NewPostgres builds the store over a pool, telling obs about each transaction.
func NewPostgres(pool *pgxpool.Pool, obs dbx.Observer) *Postgres {
	return &Postgres{pool: pool, q: db.New(pool), obs: obs}
}

// InTx runs fn in one transaction; fn may run more than once, so it must have no outside effects.
func (p *Postgres) InTx(ctx context.Context, fn func(uow application.UnitOfWork) error) error {
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		return fn(unitOfWork{q: db.New(tx), now: dbx.Now()})
	}, dbx.Observed(p.obs))
	return failures.Classify(err)
}

// List is the account's characters, oldest first.
func (p *Postgres) List(ctx context.Context, account kernel.AccountID) ([]model.Character, error) {
	rows, err := p.q.ListCharactersByAccount(ctx, uuid.UUID(account))
	if err != nil {
		return nil, failures.Classify(err)
	}
	out := make([]model.Character, 0, len(rows))
	for _, row := range rows {
		out = append(out, toCharacter(row))
	}
	return out, nil
}

// Load is one character, only if the account owns it; otherwise ErrNotFound.
func (p *Postgres) Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	row, err := p.q.GetOwnedCharacter(ctx, db.GetOwnedCharacterParams{ID: uuid.UUID(id), AccountID: uuid.UUID(account)})
	if err != nil {
		return model.Character{}, failures.Classify(err)
	}
	return toCharacter(row), nil
}

// Quests is the character's quests, by ID.
func (p *Postgres) Quests(ctx context.Context, id kernel.CharacterID) ([]model.Quest, error) {
	rows, err := p.q.ListQuests(ctx, uuid.UUID(id))
	if err != nil {
		return nil, failures.Classify(err)
	}
	out := make([]model.Quest, 0, len(rows))
	for _, q := range rows {
		progress := make([]int, len(q.Progress))
		for k, n := range q.Progress {
			progress[k] = int(n)
		}
		out = append(out, model.Quest{ID: q.QuestID, Status: model.QuestStatus(q.Status), Progress: progress, TimesFinished: int(q.TimesFinished)})
	}
	return out, nil
}

// Skills is the character's skills with points, by ID.
func (p *Postgres) Skills(ctx context.Context, id kernel.CharacterID) ([]model.SkillPoints, error) {
	rows, err := p.q.ListSkills(ctx, uuid.UUID(id))
	if err != nil {
		return nil, failures.Classify(err)
	}
	out := make([]model.SkillPoints, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.SkillPoints{ID: r.SkillID, Points: int(r.Points)})
	}
	return out, nil
}

// Timers is the character's skill timers, by skill and kind.
func (p *Postgres) Timers(ctx context.Context, id kernel.CharacterID) ([]model.SkillTimer, error) {
	rows, err := p.q.ListSkillTimers(ctx, uuid.UUID(id))
	if err != nil {
		return nil, failures.Classify(err)
	}
	out := make([]model.SkillTimer, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.SkillTimer{Skill: r.SkillID, Kind: model.TimerKind(r.Kind), Until: r.Until, Points: int(r.Points)})
	}
	return out, nil
}

func toCharacter(r db.Character) model.Character {
	return model.Character{
		ID: kernel.CharacterID(r.ID), AccountID: kernel.AccountID(r.AccountID), Name: r.Name, Class: kernel.Class(r.Class),
		Level: int(r.Level), Exp: r.Exp,
		Points:        gamedata.Stats{STR: int(r.StrPoints), DEX: int(r.DexPoints), INT: int(r.IntPoints), AGI: int(r.AgiPoints), VIT: int(r.VitPoints)},
		UnspentPoints: int(r.UnspentPoints), FreeResetUsed: r.FreeResetUsed, HP: int(r.Hp), Energy: int(r.Energy),
		MapID: r.MapID, X: float64(r.PosX), Y: float64(r.PosY), UpdatedAt: r.UpdatedAt.Time(),
		UnspentSkillPoints: int(r.UnspentSkillPoints), SkillBar: r.SkillBar,
	}
}

// unitOfWork stamps every row it writes with now, the transaction's time on the app's clock.
type unitOfWork struct {
	q   *db.Queries
	now dbx.Stamp
}

// Characters is the character repository bound to the transaction.
func (u unitOfWork) Characters() application.Repository { return repo(u) }

type repo struct {
	q   *db.Queries
	now dbx.Stamp
}

// CountLocked takes the account's advisory lock, then counts its characters.
func (r repo) CountLocked(ctx context.Context, account kernel.AccountID) (int, error) {
	if err := r.q.LockAccountCharacters(ctx, account.String()); err != nil {
		return 0, failures.Classify(err)
	}
	n, err := r.q.CountCharactersByAccount(ctx, uuid.UUID(account))
	return int(n), failures.Classify(err)
}

// Insert adds a character row and returns it as stored.
func (r repo) Insert(ctx context.Context, c model.Character) (model.Character, error) {
	row, err := r.q.InsertCharacter(ctx, db.InsertCharacterParams{
		ID: uuid.UUID(c.ID), AccountID: uuid.UUID(c.AccountID), Name: c.Name, Class: string(c.Class),
		Level: int32(c.Level), Exp: c.Exp, //nolint:gosec // levels are small
		StrPoints: int32(c.Points.STR), DexPoints: int32(c.Points.DEX), IntPoints: int32(c.Points.INT), //nolint:gosec // points are small
		AgiPoints: int32(c.Points.AGI), VitPoints: int32(c.Points.VIT), UnspentPoints: int32(c.UnspentPoints), FreeResetUsed: c.FreeResetUsed, //nolint:gosec // points are small
		Hp: int32(c.HP), MapID: c.MapID, PosX: float32(c.X), PosY: float32(c.Y), //nolint:gosec // HP fits
		Now: r.now,
	})
	if err != nil {
		return model.Character{}, err
	}
	return toCharacter(row), nil
}

// SaveCheckpoint updates the class, position, HP, energy, level, EXP and points; no row is pgx.ErrNoRows.
func (r repo) SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error {
	n, err := r.q.SaveCheckpoint(ctx, db.SaveCheckpointParams{
		ID: uuid.UUID(cp.ID), Class: cp.Class.String(), MapID: cp.MapID, PosX: float32(cp.X), PosY: float32(cp.Y),
		Hp: int32(cp.HP), Energy: int16(cp.Energy), Level: int32(cp.Level), Exp: cp.Exp, //nolint:gosec // HP, energy and levels fit
		StrPoints: int32(cp.Points.STR), DexPoints: int32(cp.Points.DEX), IntPoints: int32(cp.Points.INT), //nolint:gosec // points are small
		AgiPoints: int32(cp.Points.AGI), VitPoints: int32(cp.Points.VIT), UnspentPoints: int32(cp.UnspentPoints), FreeResetUsed: cp.FreeResetUsed, //nolint:gosec // points are small
		UnspentSkillPoints: int32(cp.UnspentSkillPoints), //nolint:gosec // points are small
		Now:                r.now,
	})
	if err := dbx.RowsOrNotFound(n, err); err != nil {
		return err
	}
	sk := db.SaveSkillsParams{CharacterID: uuid.UUID(cp.ID), SkillIds: []string{}, Points: []int32{}, Now: r.now}
	for _, s := range cp.Skills {
		sk.SkillIds = append(sk.SkillIds, s.ID)
		sk.Points = append(sk.Points, int32(s.Points)) //nolint:gosec // points are small
	}
	if err := r.q.SaveSkills(ctx, sk); err != nil {
		return err
	}
	q := db.SaveQuestsParams{CharacterID: uuid.UUID(cp.ID), QuestIds: []string{}, Now: r.now}
	for _, quest := range cp.Quests {
		counts := make([]string, len(quest.Progress))
		for k, n := range quest.Progress {
			counts[k] = strconv.Itoa(n)
		}
		q.QuestIds = append(q.QuestIds, quest.ID)
		q.Statuses = append(q.Statuses, string(quest.Status))
		q.Progress = append(q.Progress, strings.Join(counts, ","))
		q.TimesFinished = append(q.TimesFinished, int32(quest.TimesFinished)) //nolint:gosec // counts are small
	}
	if err := r.q.SaveQuests(ctx, q); err != nil {
		return err
	}
	if err := r.q.DropOtherQuests(ctx, db.DropOtherQuestsParams{CharacterID: uuid.UUID(cp.ID), QuestIds: q.QuestIds}); err != nil {
		return err
	}
	t := db.SaveSkillTimersParams{CharacterID: uuid.UUID(cp.ID), SkillIds: []string{}, Kinds: []string{}, Untils: []time.Time{}, Points: []int32{}, Now: r.now}
	for _, timer := range cp.Timers {
		t.SkillIds = append(t.SkillIds, timer.Skill)
		t.Kinds = append(t.Kinds, string(timer.Kind))
		t.Untils = append(t.Untils, timer.Until)
		t.Points = append(t.Points, int32(timer.Points)) //nolint:gosec // points are small
	}
	if err := r.q.SaveSkillTimers(ctx, t); err != nil {
		return err
	}
	return r.q.DropOtherSkillTimers(ctx, db.DropOtherSkillTimersParams{CharacterID: uuid.UUID(cp.ID), SkillIds: t.SkillIds, Kinds: t.Kinds})
}

// SaveLevelUp updates level, EXP, points and HP; no row is pgx.ErrNoRows.
func (r repo) SaveLevelUp(ctx context.Context, lu model.LevelUp) error {
	n, err := r.q.SaveLevelUp(ctx, db.SaveLevelUpParams{
		ID: uuid.UUID(lu.ID), Level: int32(lu.Level), Exp: lu.Exp, //nolint:gosec // levels are small
		StrPoints: int32(lu.Points.STR), DexPoints: int32(lu.Points.DEX), IntPoints: int32(lu.Points.INT), //nolint:gosec // points are small
		AgiPoints: int32(lu.Points.AGI), VitPoints: int32(lu.Points.VIT), //nolint:gosec // points are small
		Hp:  int32(lu.HP), //nolint:gosec // HP fits
		Now: r.now,
	})
	return dbx.RowsOrNotFound(n, err)
}

// SetSkillBar replaces the character's skill bar, if the account owns it; otherwise ErrNotFound.
func (r repo) SetSkillBar(ctx context.Context, id kernel.CharacterID, account kernel.AccountID, bar []string) error {
	n, err := r.q.SetSkillBar(ctx, db.SetSkillBarParams{ID: uuid.UUID(id), AccountID: uuid.UUID(account), SkillBar: bar, Now: r.now})
	return dbx.RowsOrNotFound(n, err)
}

const constraintName = "characters_name_key"

var failures = dbx.Errors{NotFound: model.ErrNotFound, Constraints: map[string]error{constraintName: model.ErrNameTaken}}
