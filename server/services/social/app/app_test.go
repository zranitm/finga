package app_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/runner/runnertest"
	"finga/services/social/app"
	"finga/services/social/config"
)

func TestAnInviteReachesTheGatewayOverTheRealListener(t *testing.T) {
	ctx := t.Context()
	log := runnertest.NewLog()
	cfg := config.Config{GRPC: host.GRPCSection{Listen: "127.0.0.1:0"}, Ops: host.OpsSection{Addr: "127.0.0.1:0"}, ShutdownGrace: time.Second}
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
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
}
