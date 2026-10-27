package app_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/config"
	"finga/internal/host"
	"finga/internal/model"
	"finga/pkg/grpcx"
	"finga/pkg/pgtest"
	"finga/services/item/cmd/item/app"
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
		Server:    config.Server{ShutdownGrace: time.Second},
		Database:  config.Database{URL: cluster.URL(t), MaxConns: 2},
		World:     config.World{DataDir: "../../../../../data"},
		Telemetry: config.Telemetry{LogLevel: "error"},
		Services:  config.Services{Item: config.Endpoint{Listen: "127.0.0.1:0", Ops: "127.0.0.1:0"}},
	}
}

func TestTheServiceGrantsStarterItemsOverGRPC(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := app.Build(ctx, cfgFor(t), slog.New(slog.DiscardHandler), prometheus.NewRegistry())
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
	c := itemv1.NewItemServiceClient(conn)
	actor := &itemv1.Actor{AccountId: model.NewAccountID().String(), CharacterId: model.NewCharacterID().String()}
	if _, err := c.GrantStarter(ctx, &itemv1.GrantStarterRequest{Actor: actor, Class: "warrior"}); err != nil {
		t.Fatal(err)
	}
	inv, err := c.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: actor.GetCharacterId()})
	if err != nil || len(inv.GetItems()) != 2 {
		t.Fatalf("inventory %+v, err %v", inv.GetItems(), err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestBuildFailsFastOnBadGameData(t *testing.T) {
	cfg := cfgFor(t)
	cfg.World.DataDir = t.TempDir()
	if _, err := app.Build(context.Background(), cfg, slog.New(slog.DiscardHandler), prometheus.NewRegistry()); err == nil {
		t.Fatal("built with no game data")
	}
}
