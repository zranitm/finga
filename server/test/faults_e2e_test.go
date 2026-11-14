package e2e_test

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/config"
	"finga/internal/host"
	"finga/pkg/transient"
	"finga/pkg/uuidv7"
	"finga/services/account"
	"finga/services/character"
	gatewayapp "finga/services/gateway/app"
	"finga/services/item"
	"finga/services/social"
	"finga/services/world"
)

// flakyItems is the item module failing the world's next grants: before them as a module down would, after them as a lost reply would.
type flakyItems struct {
	*item.Module
	before, after atomic.Int32
	mu            sync.Mutex
	offered       []string // every drop's key the world asked to grant, each once
}

func (f *flakyItems) AddMany(ctx context.Context, actor item.Actor, drops []item.AddRequest) (item.Batch, error) {
	f.mu.Lock()
	for _, d := range drops {
		if !slices.Contains(f.offered, d.Key) {
			f.offered = append(f.offered, d.Key)
		}
	}
	f.mu.Unlock()
	if f.before.Add(-1) >= 0 {
		return item.Batch{}, fmt.Errorf("%w: the item module is down", transient.ErrUnavailable)
	}
	b, err := f.Module.AddMany(ctx, actor, drops)
	if err == nil && f.after.Add(-1) >= 0 {
		return item.Batch{}, context.DeadlineExceeded // granted, but the answer came too late
	}
	return b, err
}

func (f *flakyItems) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.offered)
}

// build is cmd/finga's composition, which it must keep matching (cmd/finga/app.Build), with the world granting through f.
func (f *flakyItems) build(ctx context.Context, h *host.Host, cfg config.Config) error {
	data, err := h.GameData()
	if err != nil {
		return err
	}
	m, err := h.Module("account")
	if err != nil {
		return err
	}
	accounts, err := account.New(ctx, account.Deps{Module: m, Config: cfg.Account})
	if err != nil {
		return err
	}
	if m, err = h.Module("item"); err != nil {
		return err
	}
	if f.Module, err = item.New(ctx, item.Deps{Module: m, Config: cfg.Item, Data: data}); err != nil {
		return err
	}
	if m, err = h.Module("character"); err != nil {
		return err
	}
	characters, err := character.New(ctx, character.Deps{Module: m, Data: data, Item: f.Module})
	if err != nil {
		return err
	}
	if m, err = h.Module("social"); err != nil {
		return err
	}
	parties, err := social.New(ctx, social.Deps{Module: m, Config: cfg.Social, Data: data})
	if err != nil {
		return err
	}
	if m, err = h.Module("world"); err != nil {
		return err
	}
	game, err := world.New(ctx, world.Deps{Module: m, Config: cfg.World, Data: data, ID: uuidv7.NewString(), Local: world.Local{Character: characters, Item: f, Social: parties}})
	if err != nil {
		return err
	}
	return gatewayapp.BuildLocal(ctx, h, cfg.Gateway, cfg.HTTP, gatewayapp.Modules{Account: accounts, Character: characters, Item: f.Module, World: game, Social: parties})
}

// The world's grants meet an item module that fails some of them, before and after they commit: the batches retry each under its keys, and every drop lands once.
func TestGrantsTheItemModuleFailsLandOnceEach(t *testing.T) {
	t.Parallel()
	flaky := &flakyItems{}
	flaky.before.Store(3)
	flaky.after.Store(3)
	s := newStackBuilt(t, gameData(t, quickSlimes()...), flaky.build)
	token, id := s.createCharacter(t, "ola", "Ola", "warrior")
	s.place(t, id, "forest", 300, 608, 144, 0)
	p, snap := s.join(t, token, id)
	p.face(true)
	ola := newLooter(p, snap)
	deadline := time.Now().Add(30 * time.Second)
	for flaky.after.Load() >= 0 && time.Now().Before(deadline) { // until every failure was met, and a grant after them
		ola.farm(time.Second)
	}
	if flaky.after.Load() >= 0 {
		t.Fatal("the item module still had failures to give after 30 s of farming")
	}
	// Each drop the world offered lands once, retried until it does, and its owner hears of it once: the database and the pushes agree.
	var landed, events, drops, collected int
	converged := ola.until(15*time.Second, func() bool {
		keys := flaky.keys()
		collected = 0
		for _, l := range ola.loots {
			if l.GetKind() == clientv1.LootKind_LOOT_KIND_COLLECTED && l.GetOwnerId() == id {
				collected++
			}
		}
		if err := s.db.QueryRow(context.Background(), `
			SELECT count(DISTINCT e.cause_id) FILTER (WHERE e.cause_id = ANY($2::uuid[])), count(*), count(DISTINCT e.cause_id)
			FROM item_events e JOIN items i ON i.id = e.item_id
			WHERE i.owner_id = $1 AND e.cause_type = 'drop'`, id, keys).Scan(&landed, &events, &drops); err != nil {
			t.Fatal(err)
		}
		return landed == len(keys) && events == drops && drops == len(keys) && collected == drops
	})
	if !converged || drops == 0 {
		t.Fatalf("of %d drops offered, %d landed; %d grant events under %d drops, %d told collected; want each offered drop granted once and told once",
			len(flaky.keys()), landed, events, drops, collected)
	}
	if len(ola.failures) != 0 {
		t.Fatalf("failures %+v", ola.failures)
	}
	s.checkInvariants(t)
	_, w := s.welcome(t, "ola", id)
	held := map[string]int{}
	for _, it := range w.GetInventory() {
		held[it.GetTemplate()] += int(it.GetQuantity())
	}
	if want := s.inventoryTotals(t, id); !maps.Equal(held, want) {
		t.Fatalf("Ola is welcomed with %v, the database holds %v", held, want)
	}
}
