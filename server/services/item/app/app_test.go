package app_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

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
	cluster, err = pgtest.Start(ctx, migrations.Up)
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
		GRPC: host.GRPCSection{Listen: runnertest.Addr(t)}, Ops: host.OpsSection{Addr: runnertest.Addr(t)},
		ShutdownGrace: time.Second,
		DataDir:       "../../../data",
		Database:      dbx.Config{URL: cluster.URL(t), MaxConns: 2},
	}
}

func TestTheServiceGrantsStarterItemsOverGRPC(t *testing.T) {
	ctx := t.Context()
	cfg := cfgFor(t)
	h, err := app.Build(ctx, cfg, slog.New(slog.DiscardHandler), prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	stop := runnertest.Start(t, "http://"+cfg.Ops.Addr+"/readyz", h.Run)
	conn, err := grpcx.Dial(cfg.GRPC.Listen)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := itemv1.NewItemServiceClient(conn)
	actor := &itemv1.Actor{AccountId: kernel.NewAccountID().String(), CharacterId: kernel.NewCharacterID().String()}
	if _, err := c.GrantStarter(ctx, &itemv1.GrantStarterRequest{Actor: actor, Class: "warrior"}); err != nil {
		t.Fatal(err)
	}
	inv, err := c.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: actor.GetCharacterId()})
	if err != nil || len(inv.GetItems()) != 2 {
		t.Fatalf("inventory %+v, err %v", inv.GetItems(), err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildFailsFastOnBadGameData(t *testing.T) {
	cfg := cfgFor(t)
	cfg.DataDir = t.TempDir()
	if _, err := app.Build(context.Background(), cfg, slog.New(slog.DiscardHandler), prometheus.NewRegistry()); err == nil {
		t.Fatal("built with no game data")
	}
}
