package app_test

import (
	"context"
	"net"
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
	"finga/pkg/runner/runnertest"
	"finga/services/world/app"
	"finga/services/world/config"
	"finga/services/world/internal/application/world"
)

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

type items struct {
	itemv1.UnimplementedItemServiceServer
	mu     sync.Mutex
	grants []string
}

// Inventory has the character wearing a cap worth 30 max HP.
func (f *items) Inventory(context.Context, *itemv1.InventoryRequest) (*itemv1.InventoryResponse, error) {
	hat := &itemv1.Item{Id: kernel.NewItemID().String(), TemplateId: "leather_cap", Quantity: 1, EquipSlot: "hat", Stats: &itemv1.ItemStats{Defense: 3, MaxHp: 30}}
	return &itemv1.InventoryResponse{Items: []*itemv1.Item{hat}, Revision: 1}, nil
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

func TestATickThatSplitsAMovementStepIsRefused(t *testing.T) {
	for _, hz := range []int{25, 120} { // 2.4 steps of 1/60 s, and half of one
		cfg := config.Config{DataDir: "../../../data", TickRateHz: hz}
		if _, err := app.Build(t.Context(), cfg, runnertest.NewLog().Logger(), prometheus.NewRegistry()); err == nil || !strings.Contains(err.Error(), "tick_rate_hz") {
			t.Fatalf("%d Hz: %v", hz, err)
		}
	}
}

// The interval is cut to whole nanoseconds, so 30 Hz holds 1.99999998 steps of 1/60 s; the rate itself divides evenly.
func TestATickOfWholeStepsIsAcceptedAtAnyRate(t *testing.T) {
	for _, hz := range []int{10, 12, 15, 20, 30, 60} {
		cfg := config.Config{
			GRPC: host.GRPCSection{Listen: "127.0.0.1:0"}, Ops: host.OpsSection{Addr: "127.0.0.1:0"},
			DataDir: "../../../data", TickRateHz: hz, CheckpointInterval: 30 * time.Second, ItemCalls: config.ItemCalls{Workers: 1},
		}
		h, err := app.Build(t.Context(), cfg, runnertest.NewLog().Logger(), prometheus.NewRegistry())
		if err != nil {
			t.Fatalf("%d Hz: %v", hz, err)
		}
		h.Close()
	}
}

func TestAJoinOverTheRealListenersLoadsGrantsAndDelivers(t *testing.T) {
	ctx := t.Context()
	account, id := kernel.NewAccountID(), kernel.NewCharacterID()
	chars := &characters{c: &characterv1.Character{
		Id: id.String(), Name: "Ayla", Class: "warrior", Level: 1, Stats: &characterv1.Stats{Strength: 12, Dexterity: 5, Intelligence: 4, Agility: 5, Vitality: 10},
		Hp: 204, Mp: 28, MapId: "town", X: 416, Y: 608,
	}}
	itemSvc := &items{}
	log := runnertest.NewLog()
	cfg := config.Config{
		GRPC: host.GRPCSection{Listen: "127.0.0.1:0"}, Ops: host.OpsSection{Addr: "127.0.0.1:0"},
		ShutdownGrace:      time.Second,
		DataDir:            "../../../data",
		TickRateHz:         20,
		CheckpointInterval: 30 * time.Second,
		FlushTimeout:       5 * time.Second,
		ItemCalls:          config.ItemCalls{Workers: 2},
		Dependencies: config.Dependencies{
			Character: serve(t, func(s *grpc.Server) { characterv1.RegisterCharacterServiceServer(s, chars) }),
			Item:      serve(t, func(s *grpc.Server) { itemv1.RegisterItemServiceServer(s, itemSvc) }),
			Social:    serve(t, func(s *grpc.Server) { socialv1.RegisterSocialServiceServer(s, social{}) }),
		},
	}
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
	if res.GetCharacter().GetMaxHp() != 234 || res.GetCharacter().GetMapId() != "town" {
		t.Fatalf("view %+v; want 204 HP and the worn cap's 30", res.GetCharacter())
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
		for deadline := time.Now().Add(5 * time.Second); !strings.Contains(body, `finga_tick_duration_seconds_count{map="town"}`); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("no tick metrics in\n%s", body)
			}
			_, body = runnertest.Get(t, "http://"+p.Addr("ops")+"/metrics")
		}
	})
	t.Run("every reason a save is dropped for shows from the start, at 0", func(t *testing.T) {
		_, body := runnertest.Get(t, "http://"+p.Addr("ops")+"/metrics")
		for _, reason := range []string{world.DropRefused, world.DropUnexplained, world.DropShutdown} {
			if want := `finga_saves_dropped_total{reason="` + reason + `"} 0`; !strings.Contains(body, want) {
				t.Fatalf("no %s in\n%s", want, body)
			}
		}
	})
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
}
