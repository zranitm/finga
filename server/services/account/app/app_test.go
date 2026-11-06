package app_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/host"
	"finga/pkg/dbx"
	"finga/pkg/grpcx"
	"finga/pkg/pgtest"
	"finga/pkg/runner/runnertest"
	"finga/pkg/uuidv7"
	"finga/services/account/app"
	"finga/services/account/config"
	"finga/services/account/migrations"
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

func cfgFor(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		GRPC: host.GRPCSection{Listen: "127.0.0.1:0"}, Ops: host.OpsSection{Addr: "127.0.0.1:0"},
		ShutdownGrace: time.Second,
		Database:      dbx.Config{URL: cluster.URL(t), MaxConns: 2},
		Password:      config.Password{Time: 1, MemoryKiB: 8 * 1024, Threads: 1, Concurrent: 2, Wait: time.Second},
		Sessions:      config.Sessions{Keep: 30 * 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 100},
	}
}

// A session that ended longer ago than it is kept is deleted by the running service, and counted.
func TestTheServiceDeletesLongEndedSessions(t *testing.T) {
	ctx := t.Context()
	cfg := cfgFor(t)
	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	account, ended, active := uuidv7.New(), uuidv7.New(), uuidv7.New()
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, username, password_hash, created_at, updated_at) VALUES ($1, 'alice', 'x', now(), now())`, account); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id, account_id, token_hash, created_at, updated_at, expires_at, revoked_at) VALUES
		($2, $1, '\x01', now() - interval '40 days', now() - interval '31 days', now() - interval '20 days', now() - interval '31 days'),
		($3, $1, '\x02', now(), now(), now() + interval '7 days', NULL)`, account, ended, active); err != nil {
		t.Fatal(err)
	}
	log := runnertest.NewLog()
	h, err := app.Build(ctx, cfg, log.Logger(), prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	p := runnertest.Start(t, log, h.Run, "grpc", "ops")
	runnertest.GetUntil(t, "http://"+p.Addr("ops")+"/metrics", `finga_db_expired_rows_deleted_total{table="sessions"} 1`)
	rows, err := pool.Query(ctx, `SELECT id FROM sessions`)
	if err != nil {
		t.Fatal(err)
	}
	left, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil || len(left) != 1 || left[0] != active {
		t.Fatalf("sessions left %v (%v), want only the active one", left, err)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestASecondLoginReachesAWatchingGateway(t *testing.T) {
	ctx := t.Context()
	log := runnertest.NewLog()
	cfg := cfgFor(t)
	h, err := app.Build(ctx, cfg, log.Logger(), prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	p := runnertest.Start(t, log, h.Run, "grpc", "ops")
	conn, err := grpcx.Dial(p.Addr("grpc"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := accountv1.NewAccountServiceClient(conn)
	reg, err := c.Register(ctx, &accountv1.RegisterRequest{Username: "alice", Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Login(ctx, &accountv1.LoginRequest{Username: "alice", Password: "correct horse"}); err != nil {
		t.Fatal(err)
	}
	stream, err := c.WatchSessions(ctx, &accountv1.WatchSessionsRequest{GatewayId: "gw"})
	if err != nil {
		t.Fatal(err)
	}
	if m, err := stream.Recv(); err != nil || m.GetSubscribed().GetBootId() == "" {
		t.Fatalf("first %+v %v", m, err)
	}
	if _, err := c.Login(ctx, &accountv1.LoginRequest{Username: "alice", Password: "correct horse"}); err != nil {
		t.Fatal(err)
	}
	m, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev := m.GetAccountRevoked(); ev.GetAccountId() != reg.GetAccount().GetId() || ev.GetReason() != accountv1.RevokeReason_REVOKE_REASON_NEW_LOGIN {
		t.Fatalf("event %+v", m)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildFailsFastWithoutADatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := config.Config{Database: dbx.Config{URL: "postgres://nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=1", MaxConns: 1}}
	if _, err := app.Build(ctx, cfg, slog.New(slog.DiscardHandler), prometheus.NewRegistry()); err == nil {
		t.Fatal("built without a database")
	}
}
