package app_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/host"
	"finga/pkg/dbx"
	"finga/pkg/grpcx"
	"finga/pkg/pgtest"
	"finga/services/account/cmd/account/app"
	"finga/services/account/config"
	"finga/services/account/migrations"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, migrations.Up)
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

// A second login is the account service's hook firing for real: the
// gateway watching sessions hears the first one end.
func TestASecondLoginReachesAWatchingGateway(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := config.Config{
		GRPC: host.GRPCSection{Listen: "127.0.0.1:0"}, Ops: host.OpsSection{Addr: "127.0.0.1:0"},
		ShutdownGrace: time.Second,
		Database:      dbx.Config{URL: cluster.URL(t), MaxConns: 2},
	}
	h, err := app.Build(ctx, cfg, slog.New(slog.DiscardHandler), prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx) }()
	for deadline := time.Now().Add(5 * time.Second); !h.Ready(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("never ready")
		}
	}
	conn, err := grpcx.Dial(h.Addr(host.GRPC))
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
	cancel()
	if err := <-done; err != nil {
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
