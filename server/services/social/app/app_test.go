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
	settings := host.Settings{GRPC: "127.0.0.1:0", Ops: "127.0.0.1:0", ShutdownGrace: time.Second, DataDir: "../../../data"}
	h, err := host.New(host.Options{Name: "social", Settings: settings, Logger: log.Logger(), Registry: prometheus.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	if err := app.Build(ctx, h, config.Config{Members: config.Members{Keep: time.Hour, PruneEvery: time.Minute, PruneBatch: 10}}); err != nil {
		t.Fatal(err)
	}
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
		if _, err := c.Online(ctx, &socialv1.OnlineRequest{GatewayId: "gw", ConnectionId: "conn-" + id.String(), JoinSeq: 1, Member: &socialv1.Member{Id: id.String(), Name: "X", MapId: "town"}}); err != nil {
			t.Fatal(err)
		}
		if m, err := stream.Recv(); err != nil || m.GetCharacterId() != id.String() || m.GetPartyState() == nil {
			t.Fatalf("at login %+v %v, want an empty party", m, err)
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
