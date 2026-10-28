package app_test

import (
	"context"
	"log/slog"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/config"
	"finga/internal/host"
	"finga/internal/model"
	"finga/pkg/grpcx"
	"finga/pkg/pgtest"
	"finga/services/character/cmd/character/app"
	"finga/services/character/migrations"
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

// items is the item service, faked on a loopback port.
type items struct {
	itemv1.UnimplementedItemServiceServer
	mu     sync.Mutex
	grants []*itemv1.GrantStarterRequest
}

func (f *items) GrantStarter(_ context.Context, req *itemv1.GrantStarterRequest) (*itemv1.GrantStarterResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants = append(f.grants, req)
	return &itemv1.GrantStarterResponse{}, nil
}

func TestCreationCommitsThenGrantsOverTheRealListeners(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &items{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	itemSrv := grpcx.NewServer(grpcx.ServerOptions{})
	itemv1.RegisterItemServiceServer(itemSrv, fake)
	go func() { _ = itemSrv.Serve(ln) }()
	defer itemSrv.Stop()

	cfg := config.Config{
		Server:   config.Server{ShutdownGrace: time.Second},
		Database: config.Database{URL: cluster.URL(t), MaxConns: 2},
		World:    config.World{DataDir: "../../../../../data"},
		Services: config.Services{
			Character: config.Endpoint{Listen: "127.0.0.1:0", Ops: "127.0.0.1:0"},
			Item:      config.Endpoint{Addr: ln.Addr().String()},
		},
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
	owner := model.NewAccountID()
	res, err := characterv1.NewCharacterServiceClient(conn).Create(ctx, &characterv1.CreateRequest{AccountId: owner.String(), Name: "Ayla", Class: "warrior"})
	if err != nil {
		t.Fatal(err)
	}
	if c := res.GetCharacter(); c.GetName() != "Ayla" || c.GetMaxHp() == 0 || c.GetMapId() != "town" {
		t.Fatalf("character %+v", c)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.grants) != 1 || fake.grants[0].GetActor().GetCharacterId() != res.GetCharacter().GetId() || fake.grants[0].GetClass() != "warrior" {
		t.Fatalf("grants %+v", fake.grants)
	}
}
