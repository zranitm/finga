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
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/dbx"
	"finga/pkg/grpcx"
	"finga/pkg/pgtest"
	"finga/services/character/cmd/character/app"
	"finga/services/character/config"
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
		GRPC: host.GRPCSection{Listen: "127.0.0.1:0"}, Ops: host.OpsSection{Addr: "127.0.0.1:0"},
		ShutdownGrace: time.Second,
		DataDir:       "../../../../../data",
		Database:      dbx.Config{URL: cluster.URL(t), MaxConns: 2},
		Dependencies:  config.Dependencies{Item: ln.Addr().String()},
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
	owner := kernel.NewAccountID()
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
