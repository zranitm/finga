package app_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/social/app"
	"finga/services/social/config"
)

// The service as production builds it: an invite between two characters
// online through a gateway reaches that gateway's stream.
func TestAnInviteReachesTheGatewayOverTheRealListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := config.Config{GRPC: host.GRPCSection{Listen: "127.0.0.1:0"}, Ops: host.OpsSection{Addr: "127.0.0.1:0"}, ShutdownGrace: time.Second}
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
	c := socialv1.NewSocialServiceClient(conn)
	stream, err := c.Deliver(ctx, &socialv1.DeliverRequest{GatewayId: "gw"})
	if err != nil {
		t.Fatal(err)
	}
	if m, err := stream.Recv(); err != nil || m.GetSubscribed().GetBootId() == "" {
		t.Fatalf("first %+v %v", m, err)
	}
	ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
	for _, id := range []kernel.CharacterID{ann, bo} {
		if _, err := c.Online(ctx, &socialv1.OnlineRequest{GatewayId: "gw", Member: &socialv1.Member{Id: id.String(), Name: "X", MapId: "town"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Invite(ctx, &socialv1.InviteRequest{CharacterId: ann.String(), TargetId: bo.String()}); err != nil {
		t.Fatal(err)
	}
	m, err := stream.Recv()
	if err != nil || m.GetCharacterId() != bo.String() || m.GetPartyInvited() == nil {
		t.Fatalf("got %+v %v", m, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
