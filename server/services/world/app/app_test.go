package app_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/world/app"
	"finga/services/world/config"
)

// characters is the character service: one character to load.
type characters struct {
	characterv1.UnimplementedCharacterServiceServer
	c *characterv1.Character
}

func (f *characters) Load(_ context.Context, req *characterv1.LoadRequest) (*characterv1.LoadResponse, error) {
	return &characterv1.LoadResponse{Character: f.c}, nil
}

func (f *characters) SaveCheckpoint(context.Context, *characterv1.SaveCheckpointRequest) (*characterv1.SaveCheckpointResponse, error) {
	return &characterv1.SaveCheckpointResponse{}, nil
}

// items is the item service: it records starter grants.
type items struct {
	itemv1.UnimplementedItemServiceServer
	mu     sync.Mutex
	grants []string
}

func (f *items) GrantStarter(_ context.Context, req *itemv1.GrantStarterRequest) (*itemv1.GrantStarterResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants = append(f.grants, req.GetActor().GetCharacterId())
	return &itemv1.GrantStarterResponse{}, nil
}

// social is the social service: no parties, and a stream that stays open.
type social struct {
	socialv1.UnimplementedSocialServiceServer
}

func (social) WatchParties(_ *socialv1.WatchPartiesRequest, stream grpc.ServerStreamingServer[socialv1.WatchPartiesResponse]) error {
	if err := stream.Send(&socialv1.WatchPartiesResponse{Event: &socialv1.WatchPartiesResponse_Subscribed{Subscribed: &socialv1.Subscribed{BootId: "s"}}}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return nil
}

func serve(t *testing.T, register func(*grpc.Server)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	register(srv)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(srv.Stop)
	return ln.Addr().String()
}

func TestAJoinOverTheRealListenersLoadsGrantsAndDelivers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	account, id := kernel.NewAccountID(), kernel.NewCharacterID()
	chars := &characters{c: &characterv1.Character{
		Id: id.String(), Name: "Ayla", Class: "warrior", Level: 1, Stats: &characterv1.Stats{Strength: 12, Dexterity: 5, Intelligence: 4, Agility: 5, Vitality: 10},
		Hp: 204, Mp: 28, MapId: "town", X: 416, Y: 608,
	}}
	itemSvc := &items{}
	cfg := config.Config{
		GRPC: host.GRPCSection{Listen: "127.0.0.1:0"}, Ops: host.OpsSection{Addr: "127.0.0.1:0"},
		ShutdownGrace:      time.Second,
		DataDir:            "../../../data",
		TickRateHz:         20,
		CheckpointInterval: 30 * time.Second,
		Economy:            config.Economy{Workers: 2},
		Dependencies: config.Dependencies{
			Character: serve(t, func(s *grpc.Server) { characterv1.RegisterCharacterServiceServer(s, chars) }),
			Item:      serve(t, func(s *grpc.Server) { itemv1.RegisterItemServiceServer(s, itemSvc) }),
			Social:    serve(t, func(s *grpc.Server) { socialv1.RegisterSocialServiceServer(s, social{}) }),
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
	c := worldv1.NewWorldServiceClient(conn)
	stream, err := c.Deliver(ctx, &worldv1.DeliverRequest{GatewayId: "gw-1"})
	if err != nil {
		t.Fatal(err)
	}
	if m, err := stream.Recv(); err != nil || m.GetSubscribed().GetBootId() == "" {
		t.Fatalf("first %+v %v", m, err)
	}
	res, err := c.Join(ctx, &worldv1.JoinRequest{GatewayId: "gw-1", ConnectionId: "conn-1", AccountId: account.String(), CharacterId: id.String()})
	if err != nil {
		t.Fatal(err)
	}
	if res.GetCharacter().GetMaxHp() != 204 || res.GetCharacter().GetMapId() != "town" {
		t.Fatalf("view %+v", res.GetCharacter())
	}
	if m, err := stream.Recv(); err != nil || m.GetSnapshot().GetMapId() != "town" || m.GetCharacterId() != id.String() {
		t.Fatalf("snapshot %+v %v", m, err)
	}
	itemSvc.mu.Lock()
	grants := append([]string(nil), itemSvc.grants...)
	itemSvc.mu.Unlock()
	if len(grants) != 1 || grants[0] != id.String() {
		t.Fatalf("grants %v", grants)
	}

	t.Run("tick metrics on the ops listener", func(t *testing.T) {
		var body string
		for deadline := time.Now().Add(5 * time.Second); !strings.Contains(body, `tick_duration_seconds_count{map="town"}`); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("no tick metrics in\n%s", body)
			}
			resp, err := http.Get("http://" + h.Addr(host.Ops) + "/metrics")
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			body = string(b)
		}
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
