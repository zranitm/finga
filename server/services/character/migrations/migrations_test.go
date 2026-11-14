package migrations_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"finga/pkg/pgtest"
	"finga/pkg/uuidv7"
	"finga/services/character/migrations"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, pgtest.Migrations(migrations.FS, migrations.Table))
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

// A checkpoint moves updated_at every 30 s, so no index may hold it, or no save could be a HOT update.
func TestTheAccountIndexOrdersByCreation(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	rows, err := pool.Query(ctx, `SELECT indexname, indexdef FROM pg_indexes WHERE tablename = 'characters'`)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([2]string, error) {
		var d [2]string
		return d, row.Scan(&d[0], &d[1])
	})
	if err != nil {
		t.Fatal(err)
	}
	byAccount := false
	for _, d := range defs {
		if strings.Contains(d[1], "updated_at") {
			t.Errorf("%s holds updated_at: %s", d[0], d[1])
		}
		byAccount = byAccount || d[0] == "characters_by_account" && strings.HasSuffix(d[1], "(account_id, created_at)")
	}
	if !byAccount {
		t.Fatalf("no characters_by_account on (account_id, created_at) among %v", defs)
	}
}

// A character's stat columns hold the points spent, never negative, and it starts with its free reset.
func TestPointsAreNeverNegative(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	insert := func(name string, str, unspent int) error {
		_, err := pool.Exec(ctx, `INSERT INTO characters (id, account_id, name, class, level, exp, str_points, dex_points, int_points, agi_points, vit_points, unspent_points, hp, map_id, pos_x, pos_y, created_at, updated_at)
			VALUES ($1, $1, $2, 'warrior', 3, 0, $3, 0, 0, 0, 0, $4, 184, 'town', 416, 608, now(), now())`, uuidv7.New(), name, str, unspent)
		return err
	}
	if err := insert("Ayla", 5, 5); err != nil {
		t.Fatal(err)
	}
	var used bool
	if err := pool.QueryRow(ctx, `SELECT free_reset_used FROM characters WHERE name = 'Ayla'`).Scan(&used); err != nil || used {
		t.Fatalf("free_reset_used %v, %v; want false", used, err)
	}
	for _, c := range []struct{ str, unspent int }{{-1, 0}, {0, -1}} {
		if err := insert("Robin", c.str, c.unspent); err == nil || !strings.Contains(err.Error(), "points_not_negative") {
			t.Fatalf("points %+v stored: %v", c, err)
		}
	}
}

func TestSkillPointsAreNeverNegative(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	id := uuidv7.New()
	if _, err := pool.Exec(ctx, `INSERT INTO characters (id, account_id, name, class, level, exp, str_points, dex_points, int_points, agi_points, vit_points, unspent_points, hp, map_id, pos_x, pos_y, created_at, updated_at)
		VALUES ($1, $1, 'Ayla', 'beginner', 3, 0, 0, 0, 0, 0, 0, 0, 135, 'town', 416, 608, now(), now())`, id); err != nil {
		t.Fatal(err)
	}
	var unspent int
	if err := pool.QueryRow(ctx, `SELECT unspent_skill_points FROM characters WHERE id = $1`, id).Scan(&unspent); err != nil || unspent != 0 {
		t.Fatalf("unspent_skill_points %d, %v; want 0", unspent, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE characters SET unspent_skill_points = -1 WHERE id = $1`, id); err == nil || !strings.Contains(err.Error(), "skill_points_not_negative") {
		t.Fatalf("unspent skill points below 0 stored: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 0, now(), now())`, id); err == nil {
		t.Fatal("a skill with no points stored")
	}
}

func TestASkillBarHasSixKeys(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	id := uuidv7.New()
	if _, err := pool.Exec(ctx, `INSERT INTO characters (id, account_id, name, class, level, exp, str_points, dex_points, int_points, agi_points, vit_points, unspent_points, hp, map_id, pos_x, pos_y, created_at, updated_at)
		VALUES ($1, $1, 'Ayla', 'beginner', 3, 0, 0, 0, 0, 0, 0, 0, 135, 'town', 416, 608, now(), now())`, id); err != nil {
		t.Fatal(err)
	}
	var bar []string
	if err := pool.QueryRow(ctx, `SELECT skill_bar FROM characters WHERE id = $1`, id).Scan(&bar); err != nil || len(bar) != 6 || strings.Join(bar, "") != "" {
		t.Fatalf("skill_bar %q, %v; want six empty keys", bar, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE characters SET skill_bar = '{strike}' WHERE id = $1`, id); err == nil || !strings.Contains(err.Error(), "skill_bar_six_keys") {
		t.Fatalf("a skill bar of one key stored: %v", err)
	}
}

// A character starts with no energy and holds from 0 to 10.
func TestEnergyStaysInItsRange(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	id := uuidv7.New()
	if _, err := pool.Exec(ctx, `INSERT INTO characters (id, account_id, name, class, level, exp, str_points, dex_points, int_points, agi_points, vit_points, unspent_points, hp, map_id, pos_x, pos_y, created_at, updated_at)
		VALUES ($1, $1, 'Ayla', 'beginner', 3, 0, 0, 0, 0, 0, 0, 0, 135, 'town', 416, 608, now(), now())`, id); err != nil {
		t.Fatal(err)
	}
	var energy int
	if err := pool.QueryRow(ctx, `SELECT energy FROM characters WHERE id = $1`, id).Scan(&energy); err != nil || energy != 0 {
		t.Fatalf("energy %d, %v; want none at the start", energy, err)
	}
	for _, e := range []int{-1, 11} {
		if _, err := pool.Exec(ctx, `UPDATE characters SET energy = $2 WHERE id = $1`, id, e); err == nil || !strings.Contains(err.Error(), "energy_range") {
			t.Errorf("energy %d stored: %v", e, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE characters SET energy = 10 WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
}
