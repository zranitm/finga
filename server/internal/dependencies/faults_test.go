package dependencies

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/config"
	"finga/internal/application/item"
	"finga/internal/application/world"
	"finga/internal/shared/host"
	"finga/pkg/transient"
	"finga/pkg/uuidv7"
	"finga/test/e2etest"
)

func TestMain(m *testing.M) {
	os.Exit(e2etest.Main(m))
}

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

// build is Build, which it must keep matching, but for the admin API, with the world granting through f.
func (f *flakyItems) build(ctx context.Context, h *host.Host, cfg config.Config) error {
	data, err := h.GameData()
	if err != nil {
		return err
	}
	accounts, err := buildAccount(ctx, h, cfg)
	if err != nil {
		return err
	}
	if f.Module, err = buildItem(ctx, h, cfg, data); err != nil {
		return err
	}
	characters, err := buildCharacter(ctx, h, data, f.Module)
	if err != nil {
		return err
	}
	parties, err := buildSocial(ctx, h, cfg, data)
	if err != nil {
		return err
	}
	m, err := h.Module("world")
	if err != nil {
		return err
	}
	game, err := world.New(ctx, world.Deps{Module: m, Config: cfg.World, Data: data, ID: uuidv7.NewString(), Character: characters, Item: f, Social: parties})
	if err != nil {
		return err
	}
	matches, err := buildPvp(ctx, h, data, game, parties, characters, f.Module)
	if err != nil {
		return err
	}
	return buildGateway(ctx, h, cfg.Gateway, cfg.HTTP, gatewayModules{Account: accounts, Character: characters, Item: f.Module, World: game, Social: parties, Pvp: matches, Arena: matches})
}

// The world's grants meet an item module that fails some of them, before and after they commit: each take's items are asked again under their keys, and every item lands once.
func TestGrantsTheItemModuleFailsLandOnceEach(t *testing.T) {
	t.Parallel()
	flaky := &flakyItems{}
	flaky.before.Store(3)
	flaky.after.Store(3)
	s := e2etest.NewStackBuilt(t, e2etest.GameData(t, append(e2etest.QuickSlimes(), e2etest.LootRules(1, "60s"))...), flaky.build) // every kill a pouch, for the takes to meet the failures soon
	token, id := s.CreateCharacter(t, "ola", "Ola", "warrior")
	s.Place(t, id, "willowmere", e2etest.SlimeField, 144, 0)
	ola := e2etest.NewLooter(s.Join(t, token, id))
	deadline := time.Now().Add(30 * time.Second)
	for flaky.after.Load() >= 0 && time.Now().Before(deadline) { // until every failure was met, and a grant after them
		ola.Farm(time.Second)
	}
	if flaky.after.Load() >= 0 {
		t.Fatal("the item module still had failures to give after 30 s of farming")
	}
	if !ola.Clear(15 * time.Second) { // the last kills' pouches are taken, any item still being asked again
		t.Fatalf("Ola's pouches still down after 15 s: %v", ola.Pouches)
	}
	// Each item the world asked for lands once, asked again until it does, and its owner hears of it once: the database and the pushes agree.
	var landed, events, drops, collected int
	converged := ola.Until(15*time.Second, func() bool {
		keys := flaky.keys()
		collected = 0
		for _, l := range ola.Loots {
			if l.GetKind() == clientv1.LootKind_LOOT_KIND_TAKEN && l.GetOwnerId() == id {
				collected += len(l.GetItems())
			}
		}
		if err := s.DB.QueryRow(context.Background(), `
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
	if len(ola.Failures) != 0 {
		t.Fatalf("failures %+v", ola.Failures)
	}
	s.CheckInvariants(t)
	_, w := s.Welcome(t, "ola", id)
	held := map[string]int{}
	for _, it := range w.GetInventory() {
		held[it.GetTemplate()] += int(it.GetQuantity())
	}
	var revision int64
	if err := s.DB.QueryRow(context.Background(), `SELECT revision FROM inventory_revisions WHERE owner_id = $1`, id).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if want := s.InventoryTotals(t, id); !maps.Equal(held, want) || w.GetInventoryRevision() != uint64(revision) {
		t.Fatalf("Ola is welcomed with %v at revision %d, the database holds %v at %d", held, w.GetInventoryRevision(), want, revision)
	}
}
