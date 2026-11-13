package app_test

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/dbx"
	"finga/pkg/grpcx"
	"finga/pkg/pgtest"
	"finga/pkg/runner/runnertest"
	"finga/services/item/app"
	"finga/services/item/config"
	"finga/services/item/migrations"
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

func cfgFor() config.Config {
	return config.Config{
		IdempotencyKeys: config.IdempotencyKeys{Keep: 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 100},
		Outbox:          config.Outbox{PublishedKeep: 24 * time.Hour, UnpublishedKeep: 7 * 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 100},
	}
}

// settingsFor is the item's own process on loopback ports, over the real game data and a fresh database.
func settingsFor(t *testing.T) host.Settings {
	t.Helper()
	return host.Settings{GRPC: "127.0.0.1:0", Ops: "127.0.0.1:0", ShutdownGrace: time.Second, DataDir: "../../../data", Database: dbx.Config{URL: cluster.URL(t), MaxConns: 2}}
}

// build adds the item module from cfg to a host with settings, which the test closes.
func build(t *testing.T, settings host.Settings, cfg config.Config, log *slog.Logger) (*host.Host, error) {
	t.Helper()
	h, err := host.New(host.Options{Name: "item", Settings: settings, Logger: log, Registry: prometheus.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h, app.Build(t.Context(), h, cfg)
}

// A key past the retry window is deleted by the running service, and counted; a fresh one stays.
func TestTheServiceDeletesExpiredIdempotencyKeys(t *testing.T) {
	ctx := t.Context()
	settings := settingsFor(t)
	pool, err := pgxpool.New(ctx, settings.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `INSERT INTO idempotency_keys (owner, scope, key, request_hash, response, created_at, updated_at)
		VALUES ($1, 'item.add', 'old', '\x00', '\x00', now() - interval '25 hours', now() - interval '25 hours'), ($1, 'item.add', 'new', '\x00', '\x00', now(), now())`, kernel.NewAccountID().String()); err != nil {
		t.Fatal(err)
	}
	log := runnertest.NewLog()
	h, err := build(t, settings, cfgFor(), log.Logger())
	if err != nil {
		t.Fatal(err)
	}
	p := runnertest.Start(t, log, h.Run, "grpc", "ops")
	runnertest.GetUntil(t, "http://"+p.Addr("ops")+"/metrics", `finga_retention_deleted_total{module="item",store="idempotency_keys"} 1`)
	rows, err := pool.Query(ctx, `SELECT key FROM idempotency_keys`)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(keys) != 1 || keys[0] != "new" {
		t.Fatalf("keys left %v (%v), want only the fresh one", keys, err)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
}

// Published rows go a day after publishing; unsent ones go after the Stage 1 TTL, and stay when it is off, as Stage 2's relay needs.
func TestTheServiceDeletesOldOutboxRows(t *testing.T) {
	for _, tt := range []struct {
		name    string
		ttl     time.Duration
		deleted string
		left    []string
	}{
		{"with the unpublished TTL", 7 * 24 * time.Hour, "2", []string{"published-new", "unpublished-new"}},
		{"with it off", 0, "1", []string{"published-new", "unpublished-new", "unpublished-old"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			settings, cfg := settingsFor(t), cfgFor()
			cfg.Outbox.UnpublishedKeep = tt.ttl
			pool, err := pgxpool.New(ctx, settings.Database.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			if _, err := pool.Exec(ctx, `INSERT INTO outbox (event_id, topic, key, type, content_type, payload, occurred_at, published_at, created_at, updated_at)
				SELECT gen_random_uuid(), 'item', key, 'T', 'application/json', '{}', now() - created, now() - published, now() - created, now() - created
				FROM (VALUES ('published-old', interval '30 days', interval '25 hours'), ('published-new', interval '30 days', interval '23 hours'),
					('unpublished-old', interval '8 days', NULL), ('unpublished-new', interval '6 days', NULL)) AS r(key, created, published)`); err != nil {
				t.Fatal(err)
			}
			log := runnertest.NewLog()
			h, err := build(t, settings, cfg, log.Logger())
			if err != nil {
				t.Fatal(err)
			}
			p := runnertest.Start(t, log, h.Run, "grpc", "ops")
			runnertest.GetUntil(t, "http://"+p.Addr("ops")+"/metrics", `finga_retention_deleted_total{module="item",store="outbox"} `+tt.deleted+"\n")
			rows, err := pool.Query(ctx, `SELECT key FROM outbox ORDER BY key`)
			if err != nil {
				t.Fatal(err)
			}
			left, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil || !slices.Equal(left, tt.left) {
				t.Fatalf("rows left %v (%v), want %v", left, err, tt.left)
			}
			if err := p.Stop(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTheServiceGrantsStarterItemsOverGRPC(t *testing.T) {
	ctx := t.Context()
	log := runnertest.NewLog()
	h, err := build(t, settingsFor(t), cfgFor(), log.Logger())
	if err != nil {
		t.Fatal(err)
	}
	p := runnertest.Start(t, log, h.Run, "grpc", "ops")
	conn, err := grpcx.Dial(p.Addr("grpc"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := itemv1.NewItemServiceClient(conn)
	actor := &itemv1.Actor{AccountId: kernel.NewAccountID().String(), CharacterId: kernel.NewCharacterID().String()}
	if _, err := c.GrantStarter(ctx, &itemv1.GrantStarterRequest{Actor: actor}); err != nil {
		t.Fatal(err)
	}
	inv, err := c.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: actor.GetCharacterId()})
	if err != nil || len(inv.GetItems()) != 2 {
		t.Fatalf("inventory %+v, err %v", inv.GetItems(), err)
	}
	_, metrics := runnertest.Get(t, "http://"+p.Addr("ops")+"/metrics")
	for _, want := range []string{`finga_db_tx_duration_seconds_count{module="item",operation="GrantStarter"} 1`, `finga_outbox_unpublished{module="item"} `} {
		if !strings.Contains(metrics, want) {
			t.Fatalf("metrics lack %s:\n%s", want, metrics)
		}
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildFailsFastOnBadGameData(t *testing.T) {
	settings := settingsFor(t)
	settings.DataDir = t.TempDir()
	if _, err := build(t, settings, cfgFor(), slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("built with no game data")
	}
}
