// Command bot drives test players against a running game, then prints what happened and the tick p99.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/gamedata"
	"finga/internal/gamedata/load"
	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/pkg/uuidv7"
	"finga/pkg/wsconn"
)

func main() {
	url := flag.String("url", "http://localhost:8080", "the gateway")
	n := flag.Int("bots", 4, "how many bots")
	duration := flag.Duration("duration", 30*time.Minute, "how long they play")
	prefix := flag.String("prefix", "bot", "account name prefix (letters and digits)")
	metrics := flag.String("metrics", "http://localhost:9104", "the world service's ops address, where the tick and grant batch metrics are")
	itemMetrics := flag.String("item-metrics", "http://localhost:9103", "the item service's ops address, where the transaction times are")
	dataDir := flag.String("data", "data", "the game data, for the classes' auto-assign splits")
	flag.Parse()
	if err := run(*url, *metrics, *itemMetrics, *dataDir, *n, *duration, *prefix); err != nil {
		fmt.Fprintln(os.Stderr, "bot:", err)
		os.Exit(1)
	}
}

func run(url, metrics, itemMetrics, dataDir string, n int, duration time.Duration, prefix string) error {
	data, err := load.Dir(dataDir)
	if err != nil {
		return err
	}
	climbs, err := loadClimbs(dataDir, data)
	if err != nil {
		return err
	}
	w := newWorld(data, climbs)
	start := time.Now()
	stamp := start.Format("150405")
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	// Every bot starts a Beginner, works toward a class a Beginner is promoted to, a different one per bot, and spends by its split.
	var future []gamedata.Class
	for _, c := range data.Classes() {
		if c.Parent == data.StartClass().ID {
			future = append(future, c)
		}
	}
	bots := make([]*bot, 0, n)
	var wg sync.WaitGroup
	for k := range n {
		short := prefix
		if len(short) > 3 {
			short = short[:3]
		}
		b := newBot(url, fmt.Sprintf("%s%s%d", prefix, stamp, k), strings.ToUpper(short[:1])+short[1:]+stamp+strconv.Itoa(k), k)
		b.future, b.data, b.world, b.start = future[k%len(future)], data, w, start
		bots = append(bots, b)
		wg.Go(func() { b.play(ctx, bots) })
	}
	wg.Wait()
	fmt.Printf("proof session: %d bots for %s against %s\n\n", n, time.Since(start).Round(time.Second), url)
	var total stats
	for _, b := range bots {
		s := b.snapshot()
		total.add(s)
		fmt.Printf("%-14s kills %3d  items collected %3d  deaths %2d  hops %3d  chat %3d  spends %2d  skills %4d  quests taken %2d  handed in %2d  messages %7d  refused %3d  reconnects %d  login failures %d\n",
			b.name, s.kills, s.items, s.deaths, s.hops, s.chats, s.spends, s.skills, s.accepted, s.handedIn, s.messages, s.refused, s.reconnects, s.loginFailures)
	}
	fmt.Printf("%-14s kills %3d  items collected %3d  deaths %2d  hops %3d  chat %3d  spends %2d  skills %4d  quests taken %2d  handed in %2d  messages %7d  refused %3d  reconnects %d  login failures %d\n\n",
		"total", total.kills, total.items, total.deaths, total.hops, total.chats, total.spends, total.skills, total.accepted, total.handedIn, total.messages, total.refused, total.reconnects, total.loginFailures)
	promoted := 0
	for _, b := range bots {
		s := b.snapshot()
		at := "not promoted"
		if s.promotedAt > 0 {
			promoted++
			at = "promoted after " + s.promotedAt.Round(time.Second).String()
		}
		fmt.Printf("%-14s %-9s level %2d  %s  (Promote sent %d)\n", b.name, s.class, s.level, at, s.promotes)
	}
	fmt.Printf("promoted %d of %d\n\n", promoted, len(bots))
	return report(metrics, itemMetrics, url)
}

type stats struct {
	messages, kills, items, deaths, hops, chats, spends, skills, accepted, handedIn, refused, reconnects, loginFailures, promotes int
	// where the bot stands at the end, and when it promoted; 0 if it didn't
	class      string
	level      int
	promotedAt time.Duration
}

func (s *stats) add(o stats) {
	s.messages += o.messages
	s.kills += o.kills
	s.items += o.items
	s.deaths += o.deaths
	s.hops += o.hops
	s.chats += o.chats
	s.spends += o.spends
	s.skills += o.skills
	s.accepted += o.accepted
	s.handedIn += o.handedIn
	s.refused += o.refused
	s.reconnects += o.reconnects
	s.loginFailures += o.loginFailures
	s.promotes += o.promotes
}

type bot struct {
	url      string
	name     string
	charName string // 3 to 12 letters and digits
	index    int
	future   gamedata.Class // the class it works toward, whose split it spends by
	data     *gamedata.Data
	world    *world
	start    time.Time // the session's
	codec    *wsconn.ProtoCodec
	rng      *rand.Rand

	mu    sync.Mutex
	st    stats
	id    string
	token string

	// world state, owned by the play loop
	conn    *websocket.Conn
	msgs    chan wsconn.Message
	closed  chan struct{}
	mapID   string
	x, y    float64
	dead    bool
	seq     uint32
	held    *clientv1.Input
	kills   *kills
	inParty atomic.Bool
	items   map[string]*clientv1.ItemView // by tab/slot
	stop    int                           // the next town NPC to talk to on this visit
	talked  time.Time                     // when it talked to that NPC; zero before

	// what it knows of itself, for its plan
	class       kernel.Class
	level       int
	own         gamedata.Stats // stats without gear, as a promotion counts them
	invested    map[string]int32
	quests      map[string]*clientv1.QuestState
	attackEvery time.Duration
	tick        time.Duration
	climbing    bool
	why         string               // the goal it last logged
	lastTalk    map[string]time.Time // by NPC
	townDue     time.Time            // the next trip to the town to talk to everyone, before promotion
	huntTo      float64              // the end of its spot it walks to
	climbSince  time.Time            // when the climb under way started; zero when none is
	climbFailed time.Time            // when a climb last ran past climbLimit: its spot is out of reach for climbRetry
	heldAt      int                  // the level the trainer last said no at: no promotion is tried again before the next
	mp          int
	skillReady  time.Time // when its damage skill may be used again, as far as it knows
	facing      int32
	onGround    bool
	mobs        map[uint32]*clientv1.MobView // on its map, by ID
	portalAt    time.Time
}

// kills finds a bot's kills. A blow that empties a mob's HP removes it, so no later blow lands on it: the last landed blow on a mob removed in a tick is its kill, known once the next tick starts.
type kills struct {
	tick    uint64
	removed map[uint32]bool   // the mobs removed at tick
	last    map[uint32]string // each mob's last landed blow at tick, by source
}

func newKills() *kills { return &kills{removed: map[uint32]bool{}, last: map[uint32]string{}} }

// at moves to tick and returns the bot's kills in the tick before.
func (k *kills) at(tick uint64, me string) int {
	if tick == k.tick {
		return 0
	}
	n := 0
	for id := range k.removed {
		if k.last[id] == "p:"+me {
			n++
		}
	}
	k.tick = tick
	clear(k.removed)
	clear(k.last)
	return n
}

// removedMobs takes a Delta's removed mobs.
func (k *kills) removedMobs(tick uint64, ids []uint32, me string) int {
	n := k.at(tick, me)
	for _, id := range ids {
		k.removed[id] = true
	}
	return n
}

// blow takes a Damage; a tick's Delta comes before its Damage, but either order counts.
func (k *kills) blow(tick uint64, d *clientv1.Damage, me string) int {
	n := k.at(tick, me)
	mob, ok := strings.CutPrefix(d.GetTarget(), "m:")
	if !ok || d.GetMiss() {
		return n
	}
	if id, err := strconv.ParseUint(mob, 10, 32); err == nil {
		k.last[uint32(id)] = d.GetSource()
	}
	return n
}

func newBot(url, name, charName string, index int) *bot {
	return &bot{
		url: url, name: name, charName: charName, index: index, codec: protocol.ClientCodec(wsconn.Binary),
		rng: rand.New(rand.NewPCG(uint64(index), uint64(time.Now().UnixNano()))), kills: newKills(),
		quests: map[string]*clientv1.QuestState{}, lastTalk: map[string]time.Time{}, attackEvery: 900 * time.Millisecond, tick: 50 * time.Millisecond,
	}
}

func (b *bot) snapshot() stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.st
}

func (b *bot) count(f func(*stats)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	f(&b.st)
}

func (b *bot) play(ctx context.Context, all []*bot) {
	token, err := b.signUp(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: sign up: %v\n", b.name, err)
		return
	}
	for ctx.Err() == nil {
		if token == "" {
			// Wait between logins to stay under the per-IP login throttle.
			time.Sleep(5 * time.Second)
			token, err = b.login(ctx)
			if err != nil {
				b.count(func(s *stats) { s.loginFailures++ })
				continue
			}
			b.count(func(s *stats) { s.reconnects++ })
		}
		if err := b.enter(ctx, token); err != nil {
			fmt.Fprintf(os.Stderr, "%s: enter: %v\n", b.name, err)
		} else {
			b.loop(ctx, all)
			_ = b.conn.CloseNow()
		}
		token = ""
	}
}

func (b *bot) signUp(ctx context.Context) (string, error) {
	if _, err := b.rest(ctx, "POST", "/api/accounts", "", map[string]string{"username": b.name, "password": "correct horse"}); err != nil {
		return "", err
	}
	token, err := b.login(ctx)
	if err != nil {
		return "", err
	}
	created, err := b.rest(ctx, "POST", "/api/characters", token, map[string]string{"name": b.charName})
	if err != nil {
		return "", err
	}
	b.id, _ = created["id"].(string)
	return token, nil
}

func (b *bot) login(ctx context.Context) (string, error) {
	res, err := b.rest(ctx, "POST", "/api/sessions", "", map[string]string{"username": b.name, "password": "correct horse"})
	if err != nil {
		return "", err
	}
	token, _ := res["token"].(string)
	return token, nil
}

func (b *bot) rest(ctx context.Context, method, path, token string, body any) (map[string]any, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, b.url+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	if res.StatusCode >= 300 {
		return out, fmt.Errorf("%s %s: %d %s", method, path, res.StatusCode, raw)
	}
	return out, nil
}

func (b *bot) enter(ctx context.Context, token string) error {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, "ws"+strings.TrimPrefix(b.url, "http")+"/ws", &websocket.DialOptions{Subprotocols: []string{protocol.SubprotocolProto}})
	if err != nil {
		return err
	}
	b.conn = conn
	b.msgs = make(chan wsconn.Message, 4096)
	b.closed = make(chan struct{})
	go func() {
		defer close(b.closed)
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			m, err := b.codec.Decode(data)
			if err != nil {
				continue
			}
			b.msgs <- m
		}
	}()
	b.dead = false
	b.held = &clientv1.Input{}
	b.stop, b.talked = 0, time.Time{}
	if err := b.send(wsconn.Message{Data: &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: b.id}}); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case m := <-b.msgs:
			b.observe(m)
			if m.Type == "Snapshot" {
				return nil
			}
		case <-b.closed:
			return fmt.Errorf("closed before Welcome")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("no Snapshot after Hello")
}

func (b *bot) send(m wsconn.Message) error {
	data, err := b.codec.Encode(m)
	if err != nil {
		return err
	}
	return b.conn.Write(context.Background(), websocket.MessageBinary, data)
}

func (b *bot) input(in *clientv1.Input) {
	if proto.Equal(in, b.held) {
		return
	}
	b.held = in
	b.seq++
	_ = b.send(wsconn.Message{Seq: b.seq, Data: in})
}

func (b *bot) command(d proto.Message) {
	b.request("", d)
}

// request sends with a request ID, which the Response carries back.
func (b *bot) request(reqID string, d proto.Message) {
	b.seq++
	_ = b.send(wsconn.Message{Seq: b.seq, ReqID: reqID, Data: d})
}

// The request IDs of a Promote and a UseSkill, so their refusals can be told apart.
const (
	promoteReq = "promote"
	skillReq   = "skill"
)

// skillBackoff is how long a bot leaves its skill after the world refuses a use.
const skillBackoff = 5 * time.Second

// promoted reports whether the bot is its future class already.
func (b *bot) promoted() bool { return b.class == b.future.ID }

// know takes what a character view says of the bot.
func (b *bot) know(c *clientv1.CharacterView) {
	b.class, b.level, b.mp = kernel.Class(c.GetClass()), int(c.GetLevel()), int(c.GetMp())
	b.sheet(c.GetSheet())
	b.count(func(s *stats) { s.class, s.level = c.GetClass(), int(c.GetLevel()) })
}

// sheet takes a stat sheet: own stats are the start class's plus the points spent, as a promotion counts them.
func (b *bot) sheet(sh *clientv1.StatSheet) {
	base, spent := b.data.StartClass().BaseStats, sh.GetPoints().GetSpent()
	b.own = gamedata.Stats{
		STR: base.STR + int(spent.GetStr()), DEX: base.DEX + int(spent.GetDex()), INT: base.INT + int(spent.GetInt()),
		AGI: base.AGI + int(spent.GetAgi()), VIT: base.VIT + int(spent.GetVit()),
	}
	b.invested = sh.GetSkills().GetInvested()
	if t := sh.GetAttackLengthTicks(); t > 0 {
		b.attackEvery = time.Duration(t)*b.tick + 50*time.Millisecond
	}
}

// spend puts a view's unspent points where its plan wants them: stat points into the promotion's own stats, then the class's split, as a player planning that class does; skill points into the promotion's skills, and once promoted into the class's first.
func (b *bot) spend(c *clientv1.CharacterView) {
	if c.GetId() != b.id {
		return
	}
	need, skills := b.future.Promotion.Stats, b.future.Promotion.Skills
	if b.promoted() {
		need, skills = gamedata.Stats{}, nil
	}
	rest := b.mainSkill()
	if u := c.GetSheet().GetPoints().GetUnspent(); u > 0 {
		if p := statPoints(int(u), b.own, need, b.future.AutoAssign); p.Total() > 0 {
			b.command(&clientv1.SpendPoints{ExpectedUnspent: u, Points: &clientv1.Stats{
				Str: int32(p.STR), Dex: int32(p.DEX), Int: int32(p.INT), Agi: int32(p.AGI), Vit: int32(p.VIT),
			}})
			b.count(func(s *stats) { s.spends++ })
		}
	}
	if u := c.GetSheet().GetSkills().GetUnspent(); u > 0 {
		if p := skillPoints(u, b.invested, skills, rest); len(p) > 0 {
			b.command(&clientv1.SpendSkillPoints{ExpectedUnspent: u, Points: p})
		}
	}
}

// mainSkill is the skill a bot puts its spare skill points in and fights with: the promotion's first before it, the class's first after.
func (b *bot) mainSkill() string {
	if b.promoted() {
		if len(b.future.Skills) > 0 {
			return b.future.Skills[0]
		}
		return ""
	}
	if keys := slices.Sorted(maps.Keys(b.future.Promotion.Skills)); len(keys) > 0 {
		return keys[0]
	}
	return ""
}

// useSkill uses the main skill when it is a damage skill with points, off its cooldown, with the MP for it; true if it did.
func (b *bot) useSkill() bool {
	id := b.mainSkill()
	points := int(b.invested[id])
	sk, ok := b.data.Skill(id)
	if !ok || points == 0 || sk.Kind != gamedata.Active || sk.Effect != gamedata.DamageEffect || time.Now().Before(b.skillReady) {
		return false
	}
	st := sk.Stages[sk.StageAt(points)]
	if b.mp < st.MPAt(points) {
		return false
	}
	b.request(skillReq, &clientv1.UseSkill{Skill: id})
	b.skillReady = time.Now().Add(st.Cooldown + 2*b.tick)
	b.count(func(s *stats) { s.skills++ })
	return true
}

// plan is what the bot knows of itself, for its next goal.
func (b *bot) plan() plan {
	return plan{class: b.class, level: b.level, own: b.own, invested: b.invested, quests: b.quests, holding: b.holding}
}

func (b *bot) killed(n int) {
	if n > 0 {
		b.count(func(s *stats) { s.kills += n })
	}
}

func (b *bot) observe(m wsconn.Message) {
	b.count(func(s *stats) { s.messages++ })
	switch d := m.Data.(type) {
	case *clientv1.Welcome:
		b.token = d.GetToken()
		b.mapID = d.GetMapId()
		if hz := d.GetTickRateHz(); hz > 0 {
			b.tick = time.Second / time.Duration(hz)
		}
		b.items = map[string]*clientv1.ItemView{}
		for _, it := range d.GetInventory() {
			b.items[it.GetTab()+"/"+strconv.Itoa(int(it.GetSlot()))] = it
		}
		b.quests = map[string]*clientv1.QuestState{}
		for _, q := range d.GetQuests() {
			b.quests[q.GetQuestId()] = q
		}
		b.know(d.GetCharacter())
		b.spend(d.GetCharacter())
	case *clientv1.Snapshot:
		for _, v := range d.GetPlayers() {
			if v.GetId() == b.id {
				b.x, b.y, b.dead, b.climbing, b.facing, b.onGround = v.GetX(), v.GetY(), v.GetDead(), v.GetClimbing(), v.GetFacing(), v.GetOnGround()
			}
		}
		b.mobs = map[uint32]*clientv1.MobView{}
		for _, mob := range d.GetMobs() {
			b.mobs[mob.GetId()] = mob
		}
	case *clientv1.Delta:
		for _, v := range d.GetPlayers() {
			if v.GetId() == b.id {
				b.x, b.y, b.dead, b.climbing, b.facing, b.onGround = v.GetX(), v.GetY(), v.GetDead(), v.GetClimbing(), v.GetFacing(), v.GetOnGround()
			}
		}
		if b.mobs == nil {
			b.mobs = map[uint32]*clientv1.MobView{}
		}
		for _, mob := range d.GetMobs() {
			b.mobs[mob.GetId()] = mob
		}
		for _, id := range d.GetRemovedMobs() {
			delete(b.mobs, id)
		}
		b.killed(b.kills.removedMobs(m.Tick, d.GetRemovedMobs(), b.id))
	case *clientv1.Damage:
		b.killed(b.kills.blow(m.Tick, d, b.id))
	case *clientv1.MapChange:
		b.mapID, b.x, b.y = d.GetMapId(), d.GetX(), d.GetY()
		clear(b.mobs)
		b.held = &clientv1.Input{}
		b.stop, b.talked = 0, time.Time{}
		b.count(func(s *stats) { s.hops++ })
	case *clientv1.Died:
		if d.GetCharacterId() == b.id {
			b.dead = true
			b.count(func(s *stats) { s.deaths++ })
		}
	case *clientv1.Respawned:
		if d.GetCharacterId() == b.id {
			b.dead = false
			b.mapID, b.x, b.y = d.GetMapId(), d.GetX(), d.GetY()
			b.held = &clientv1.Input{}
			b.stop, b.talked = 0, time.Time{}
		}
	case *clientv1.Loot:
		if d.GetKind() == clientv1.LootKind_LOOT_KIND_COLLECTED && d.GetOwnerId() == b.id {
			b.count(func(s *stats) { s.items++ })
		}
	case *clientv1.Response:
		if !d.GetOk() {
			b.count(func(s *stats) { s.refused++ })
			if m.ReqID == skillReq {
				b.skillReady = time.Now().Add(skillBackoff)
			}
			if m.ReqID == promoteReq && !b.makeRoom(d.GetCode()) {
				b.heldAt = b.level
				fmt.Fprintf(os.Stderr, "%s: Promote to %s refused with %d; trying again after the next level\n", b.name, b.future.ID, d.GetCode())
			}
		}
	case *clientv1.LevelUp:
		if d.GetCharacter().GetId() == b.id {
			b.know(d.GetCharacter())
			b.spend(d.GetCharacter())
		}
	case *clientv1.StatsChanged:
		if d.GetSheet() != nil { // regen and a skill's MP come without one
			b.sheet(d.GetSheet())
		}
		b.mp = int(d.GetMp())
	case *clientv1.Promoted:
		if d.GetCharacter().GetId() == b.id {
			b.know(d.GetCharacter())
			at := time.Since(b.start)
			b.count(func(s *stats) { s.promotedAt = at })
			fmt.Fprintf(os.Stderr, "%s: promoted to %s at level %d, %s into the session\n", b.name, b.class, b.level, at.Round(time.Second))
			b.spend(d.GetCharacter())
		}
	case *clientv1.PartyInvited:
		b.command(&clientv1.PartyRespond{InviteId: d.GetInviteId(), Accept: true})
	case *clientv1.PartyState:
		b.inParty.Store(d.GetParty() != nil)
	case *clientv1.InventoryChanged:
		for _, sl := range d.GetSlots() {
			key := sl.GetTab() + "/" + strconv.Itoa(int(sl.GetSlot()))
			if sl.GetItem() == nil {
				delete(b.items, key)
			} else {
				b.items[key] = sl.GetItem()
			}
		}
	case *clientv1.NpcDialog:
		// The bot counted every condition met, but the trainer says otherwise.
		if o := d.GetPromotion(); o.GetClass() == string(b.future.ID) && !promotable(o, b.future.ID) && meets(b.future.Promotion, b.plan()) && b.heldAt != b.level {
			b.heldAt = b.level
			fmt.Fprintf(os.Stderr, "%s: %s's trainer shows a condition unmet: %v; trying again after the next level\n", b.name, b.future.ID, o.GetConditions())
		}
		for _, c := range b.answer(d) {
			if _, ok := c.(*clientv1.Promote); ok {
				b.request(promoteReq, c)
				b.count(func(s *stats) { s.promotes++ })
				continue
			}
			b.command(c)
		}
	case *clientv1.QuestChanged:
		b.quests[d.GetQuest().GetQuestId()] = d.GetQuest()
		clear(b.lastTalk) // the next talk may have something new to say
		switch d.GetChange() {
		case clientv1.QuestChange_QUEST_CHANGE_ACCEPTED:
			b.count(func(s *stats) { s.accepted++ })
		case clientv1.QuestChange_QUEST_CHANGE_HANDED_IN:
			b.count(func(s *stats) { s.handedIn++ })
		}
	}
}

// answer is what the bot does with a dialog: hand in each quest that is ready, the items it asks for held, then take every quest open to it, and promote once the trainer shows every condition met.
func (b *bot) answer(d *clientv1.NpcDialog) []proto.Message {
	var out []proto.Message
	for _, t := range d.GetTakes() {
		if t.GetReady() && b.holdsWhatItAsks(t.GetQuestId()) {
			out = append(out, &clientv1.HandInQuest{NpcId: d.GetNpcId(), QuestId: t.GetQuestId()})
		}
	}
	for _, o := range d.GetGives() {
		if o.GetState() == clientv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE {
			out = append(out, &clientv1.AcceptQuest{NpcId: d.GetNpcId(), QuestId: o.GetQuestId()})
		}
	}
	if promotable(d.GetPromotion(), b.future.ID) {
		out = append(out, &clientv1.Promote{NpcId: d.GetNpcId(), Class: string(b.future.ID)})
	}
	return out
}

func (b *bot) holdsWhatItAsks(quest string) bool {
	q, ok := b.data.Quest(quest)
	if !ok {
		return false
	}
	for _, o := range q.Objectives {
		if o.Kind == gamedata.Collect && b.holding(o.Target) < o.Count {
			return false
		}
	}
	return true
}

// tabFull are the gateway's codes for a full inventory tab, as testdata/errors/codes.json lists them, by the tab.
var tabFull = map[int32]string{220005: "equip", 220006: "use", 220007: "etc"}

// makeRoom answers a Promote refused for a full tab by discarding an item there, so the trainer's gift fits when the bot next asks; false for any other refusal.
func (b *bot) makeRoom(code int32) bool {
	tab, ok := tabFull[code]
	if !ok {
		return false
	}
	it := spare(b.items, tab, b.collects)
	if it == nil {
		return false
	}
	b.command(&clientv1.InventoryDiscard{IdempotencyKey: "k-" + uuidv7.NewString(), ItemId: it.GetItemId(), Quantity: it.GetQuantity()})
	fmt.Fprintf(os.Stderr, "%s: Promote to %s refused, the %s tab full; discarded %s to make room\n", b.name, b.future.ID, tab, it.GetTemplate())
	return true
}

// spare is what a full tab gives up: its last item that no quest of the bot still collects.
func spare(items map[string]*clientv1.ItemView, tab string, collects func(template string) bool) *clientv1.ItemView {
	var out *clientv1.ItemView
	for _, it := range items {
		if it.GetTab() == tab && !collects(it.GetTemplate()) && (out == nil || it.GetSlot() > out.GetSlot()) {
			out = it
		}
	}
	return out
}

// collects reports whether a quest the bot has taken asks for an item.
func (b *bot) collects(template string) bool {
	for id, st := range b.quests {
		q, ok := b.data.Quest(id)
		if !ok || st.GetStatus() != clientv1.QuestStatus_QUEST_STATUS_ACTIVE {
			continue
		}
		for _, o := range q.Objectives {
			if o.Kind == gamedata.Collect && o.Target == template {
				return true
			}
		}
	}
	return false
}

// holding is how many of an item the bot's inventory holds.
func (b *bot) holding(template string) int {
	n := 0
	for _, it := range b.items {
		if it.GetTemplate() == template {
			n += int(it.GetQuantity())
		}
	}
	return n
}

// townNPCs are the NPCs of a map in the order a visit talks to them: right to left, as it arrives from the portal on the right.
func townNPCs(data *gamedata.Data, mapID string) []gamedata.NPC {
	var out []gamedata.NPC
	for _, n := range data.NPCs() {
		if n.MapID == mapID {
			out = append(out, n)
		}
	}
	slices.SortFunc(out, func(a, c gamedata.NPC) int { return int(c.At.X - a.At.X) })
	return out
}

// talkGap is how long a bot waits before talking to the same NPC again, unless a quest changed since.
const talkGap = 20 * time.Second

// stall finds a bot stuck against something a jump clears: four decisions pressing a way on the ground without moving. Frames in the air, on a rope or standing to attack never count, so updates that arrive in bursts don't set a bot hopping.
type stall struct {
	x     float64
	count int
}

// jump takes a decision's position and the input held into it, and reports whether to jump.
func (s *stall) jump(x float64, onGround, climbing bool, held *clientv1.Input) bool {
	if onGround && !climbing && (held.GetLeft() || held.GetRight()) && math.Abs(x-s.x) < 1 {
		s.count++
	} else {
		s.count = 0
	}
	s.x = x
	return s.count >= 4
}

// loop makes 20 decisions a second until the connection closes or the session ends: in the start town it talks to everyone first, then it works toward its goal.
func (b *bot) loop(ctx context.Context, all []*bot) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	nextAttack, nextChat, nextInvite := time.Now(), time.Now().Add(time.Duration(10+b.index*7)*time.Second), time.Now().Add(20*time.Second)
	b.townDue = time.Now().Add(b.townEvery())
	stuck := stall{x: b.x}
	town := b.data.StartTown().ID
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.closed:
			return
		case <-ticker.C:
		}
		for drained := false; !drained; {
			select {
			case m := <-b.msgs:
				b.observe(m)
			default:
				drained = true
			}
		}
		if b.dead {
			b.input(&clientv1.Input{})
			continue
		}
		jump := stuck.jump(b.x, b.onGround, b.climbing, b.held)
		if time.Now().After(nextChat) {
			b.command(&clientv1.ChatSend{Channel: clientv1.ChatChannel_CHAT_CHANNEL_MAP, Text: fmt.Sprintf("%s at %.0f, %d kills", b.name, b.x, b.snapshot().kills)})
			b.count(func(s *stats) { s.chats++ })
			nextChat = time.Now().Add(time.Duration(25+b.rng.IntN(20)) * time.Second)
		}
		if b.index == 0 && time.Now().After(nextInvite) {
			for _, other := range all {
				if other != b && other.id != "" && !other.inParty.Load() {
					b.command(&clientv1.PartyInvite{CharacterId: other.id})
					break
				}
			}
			nextInvite = time.Now().Add(15 * time.Second)
		}
		if b.mapID == town && b.visitNPCs(jump) {
			continue
		}
		g := b.goal()
		if g.why != b.why {
			fmt.Fprintf(os.Stderr, "%s: level %d %s, now %s\n", b.name, b.level, b.class, g.why)
			b.why = g.why
		}
		if to := b.mapOf(g); to != b.mapID {
			b.travel(to, jump)
			continue
		}
		switch g.kind {
		case tourGoal:
			b.townDue = time.Now().Add(b.townEvery())
		case talkGoal:
			b.talkTo(g.npc, jump)
		case huntGoal:
			// Drops fly to their owners: the bot only hunts. The world plays a skill and an attack one a turn, so a skill puts the next attack off a cooldown.
			if b.hunt(g.spot, g.prey, jump) && time.Now().After(nextAttack) {
				if !b.useSkill() {
					b.command(&clientv1.Attack{})
				}
				nextAttack = time.Now().Add(b.attackEvery)
			}
		}
	}
}

// townEvery is how often a bot not yet promoted goes back to the start town to talk to everyone, staggered by bot.
func (b *bot) townEvery() time.Duration { return time.Duration(8+b.index) * time.Minute }

// goal is a trip to the start town when one is due before promotion, else what the bot's plan says.
func (b *bot) goal() goal {
	if !b.promoted() && b.level < b.future.Promotion.Level && time.Now().After(b.townDue) {
		return goal{kind: tourGoal, why: "a visit to the town"}
	}
	g := b.world.next(b.future, b.plan(), !b.climbFailed.IsZero() && time.Since(b.climbFailed) < climbRetry)
	if g.promote && b.heldAt == b.level {
		s, _ := b.world.levelSpot(b.level)
		return goal{kind: huntGoal, spot: s, why: "for the next level, the trainer having said no"}
	}
	return g
}

// mapOf is the map a goal is on; a hunt with no spot stays where it is.
func (b *bot) mapOf(g goal) string {
	switch {
	case g.kind == tourGoal:
		return b.data.StartTown().ID
	case g.kind == talkGoal:
		return g.npc.MapID
	case g.spot.mapID != "":
		return g.spot.mapID
	}
	return b.mapID
}

// travel walks to the portal that starts the way to a map and goes through; off the bots' maps it takes any portal back.
func (b *bot) travel(to string, jump bool) {
	p, ok := b.world.portalTo(b.mapID, to)
	if ok && !b.walkTo(p.Area.X+p.Area.W/2, jump) {
		return
	}
	if time.Since(b.portalAt) > time.Second {
		b.command(&clientv1.EnterPortal{})
		b.portalAt = time.Now()
	}
}

// approach walks to within reach of a mob at x and faces it, a step its way turning the bot.
func (b *bot) approach(x float64, jump bool) {
	dx := x - b.x
	switch {
	case math.Abs(dx) > b.reach():
		in := toward(b.x, x)
		in.Jump = jump
		b.input(in)
	case (dx < 0) != (b.facing < 0):
		b.input(toward(b.x, x))
	default:
		b.input(&clientv1.Input{})
	}
}

// reach is how near a bot goes to its prey: well inside its basic attack, a melee box or a shot.
func (b *bot) reach() float64 {
	c, ok := b.data.Class(b.class)
	switch {
	case !ok:
		return 32
	case c.Attack.Kind == gamedata.Ranged:
		return min(c.Attack.Range, 300) * 0.6
	}
	return c.Attack.Width * 0.6
}

// talkTo walks to an NPC and talks to it from its ground, dropping from a platform above it, at most once a talkGap.
func (b *bot) talkTo(n gamedata.NPC, jump bool) {
	if !b.walkTo(n.At.X, jump) {
		return
	}
	if b.y < n.At.Y-1 {
		b.input(&clientv1.Input{Down: true, Jump: true})
		return
	}
	if level(b.y, n.At.Y) && time.Since(b.lastTalk[n.ID]) > talkGap {
		b.command(&clientv1.Talk{NpcId: n.ID})
		b.lastTalk[n.ID] = time.Now()
	}
}

// clearing is how near a mob on its level a climbing bot fights it first, since a mob's touch knocks it off a rope or a ledge.
const clearing = 150.0

// hunt climbs to a spot off the ground, fighting off what stands near on the way and giving the climb up past climbLimit, then goes after the nearest prey on it, as a player does, or walks from one end of it to the other while none is there; true while it should attack.
func (b *bot) hunt(s spot, prey []string, jump bool) bool {
	if s.climb != nil && !s.climb.on(b.x, b.y) {
		if m := nearest(b.mobs, spot{from: b.x - clearing, to: b.x + clearing, y: b.y}, nil, b.x); m != nil && b.onGround && !b.climbing {
			b.approach(m.GetX(), jump)
			return true
		}
		if b.climbSince.IsZero() {
			b.climbSince = time.Now()
		} else if time.Since(b.climbSince) > climbLimit {
			b.climbFailed, b.climbSince = time.Now(), time.Time{}
			fmt.Fprintf(os.Stderr, "%s: can't climb to the spot on %s at (%.0f, %.0f) in %s; hunting the ground for %s\n", b.name, s.mapID, b.x, b.y, climbLimit, climbRetry)
		}
		in := s.climb.step(b.x, b.y, b.climbing, b.onGround, b.held)
		in.Jump = jump && (in.GetLeft() || in.GetRight())
		b.input(in)
		return false
	}
	b.climbSince = time.Time{}
	if m := nearest(b.mobs, s, prey, b.x); m != nil {
		b.approach(m.GetX(), jump)
		return true
	}
	if b.huntTo != s.from && b.huntTo != s.to {
		b.huntTo = s.to
	}
	if b.walkTo(b.huntTo, jump) {
		if b.huntTo == s.to {
			b.huntTo = s.from
		} else {
			b.huntTo = s.to
		}
	}
	return true
}

// visitNPCs walks to each NPC on the map in turn and talks to it from its ground, never mid-jump, then stands a second for the dialog and its answers; false once every one has been visited.
func (b *bot) visitNPCs(jump bool) bool {
	npcs := townNPCs(b.data, b.mapID)
	if b.stop >= len(npcs) {
		return false
	}
	if !b.talked.IsZero() {
		if time.Since(b.talked) > time.Second {
			b.stop, b.talked = b.stop+1, time.Time{}
		}
		return true
	}
	if n := npcs[b.stop]; b.walkTo(n.At.X, jump) && math.Abs(b.y-n.At.Y) < 1 {
		b.command(&clientv1.Talk{NpcId: n.ID})
		b.talked = time.Now()
	}
	return true
}

// walkTo holds a direction toward x and reports standing within 12 px.
func (b *bot) walkTo(x float64, jump bool) bool {
	dx := x - b.x
	if math.Abs(dx) < 12 {
		b.input(&clientv1.Input{})
		return true
	}
	b.input(&clientv1.Input{Right: dx > 0, Left: dx < 0, Jump: jump})
	return false
}

func scrape(url string) ([]byte, error) {
	res, err := http.Get(url) //nolint:gosec // the operator names the address
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}

func report(urls ...string) error {
	var raw []byte
	for _, url := range urls {
		body, err := scrape(url + "/metrics")
		if err != nil {
			return err
		}
		raw = append(append(raw, body...), '\n')
	}
	byMap := map[string]*hist{}
	batches, grants := newHist(), newHist() // drops per grant call; the item service's AddMany transactions
	overruns := map[string]float64{}
	online := ""
	for line := range strings.SplitSeq(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "finga_tick_duration_seconds_bucket"):
			labels, value := parseLine(line)
			h := byMap[labels["map"]]
			if h == nil {
				h = newHist()
				byMap[labels["map"]] = h
			}
			h.bucket(labels, value)
		case strings.HasPrefix(line, "finga_tick_duration_seconds_count"):
			labels, value := parseLine(line)
			if h := byMap[labels["map"]]; h != nil {
				h.count = value
			}
		case strings.HasPrefix(line, "finga_tick_overruns_total"):
			labels, value := parseLine(line)
			overruns[labels["map"]] = value
		case strings.HasPrefix(line, "finga_item_calls_batch_size_bucket"):
			batches.bucket(parseLine(line))
		case strings.HasPrefix(line, "finga_item_calls_batch_size_count"):
			_, batches.count = parseLine(line)
		case strings.HasPrefix(line, "finga_item_calls_batch_size_sum"):
			_, batches.sum = parseLine(line)
		case strings.HasPrefix(line, `finga_db_tx_duration_seconds_bucket{operation="AddMany"`):
			grants.bucket(parseLine(line))
		case strings.HasPrefix(line, `finga_db_tx_duration_seconds_count{operation="AddMany"`):
			_, grants.count = parseLine(line)
		case strings.HasPrefix(line, "finga_players_online "):
			online = strings.TrimSpace(strings.TrimPrefix(line, "finga_players_online")) // the last page's: the gateway's
		}
	}
	if online != "" {
		fmt.Printf("finga_players_online %s\n", online)
	}
	all := newHist()
	names := make([]string, 0, len(byMap))
	for name, h := range byMap {
		names = append(names, name)
		all.count += h.count
		for le, c := range h.buckets {
			all.buckets[le] += c
		}
	}
	sort.Strings(names)
	line := func(name string, h *hist) {
		fmt.Printf("%-8s ticks %8.0f  p50 %s  p99 %s  overruns %.0f\n", name, h.count, percentile(h, 0.50), percentile(h, 0.99), overruns[name])
	}
	for _, name := range names {
		line(name, byMap[name])
	}
	line("all", all)
	if batches.count > 0 {
		fmt.Printf("\ngrant calls %.0f  drops per call: mean %.2f  p50 %s  p95 %s  max %s\n", batches.count, batches.sum/batches.count,
			drops(batches.quantile(0.50)), drops(batches.quantile(0.95)), drops(batches.quantile(1)))
	}
	if grants.count > 0 {
		fmt.Printf("item AddMany transactions %.0f  p50 %s  p99 %s\n", grants.count, percentile(grants, 0.50), percentile(grants, 0.99))
	}
	return nil
}

// hist is a Prometheus histogram as /metrics shows it.
type hist struct {
	buckets map[float64]float64 // le → cumulative count
	count   float64
	sum     float64
}

func newHist() *hist { return &hist{buckets: map[float64]float64{}} }

func (h *hist) bucket(labels map[string]string, value float64) {
	le, _ := strconv.ParseFloat(labels["le"], 64)
	h.buckets[le] += value
}

// quantile interpolates within the bucket that crosses the rank, as histogram_quantile does; over is a rank past the last finite bound, which v then is.
func (h *hist) quantile(q float64) (v, bound float64, over, ok bool) {
	if h.count == 0 {
		return 0, 0, false, false
	}
	les := make([]float64, 0, len(h.buckets))
	for le := range h.buckets {
		les = append(les, le)
	}
	sort.Float64s(les)
	rank := q * h.count
	prevLE, prevCount := 0.0, 0.0
	for _, le := range les {
		c := h.buckets[le]
		if c >= rank {
			if math.IsInf(le, 1) {
				return prevLE, prevLE, true, true
			}
			frac := 0.0
			if c > prevCount {
				frac = (rank - prevCount) / (c - prevCount)
			}
			return prevLE + (le-prevLE)*frac, le, false, true
		}
		prevLE, prevCount = le, c
	}
	return 0, 0, false, false
}

// percentile is a quantile of seconds, in milliseconds.
func percentile(h *hist, q float64) string {
	v, bound, over, ok := h.quantile(q)
	switch {
	case !ok:
		return "n/a"
	case over:
		return fmt.Sprintf("> %.1f ms", v*1000)
	}
	return fmt.Sprintf("%.2f ms (bucket ≤ %s ms)", v*1000, strconv.FormatFloat(math.Round(bound*1e6)/1e3, 'f', -1, 64))
}

// drops is a quantile of drops per call as its bucket: a count interpolated within a bucket, as from 0 in the first, could read as a call with no drop.
func drops(v, bound float64, over, ok bool) string {
	switch {
	case !ok:
		return "n/a"
	case over:
		return fmt.Sprintf("> %.0f", v)
	}
	return fmt.Sprintf("≤ %.0f", bound)
}

func parseLine(line string) (map[string]string, float64) {
	labels := map[string]string{}
	rest := line
	if k := strings.Index(line, "{"); k >= 0 {
		end := strings.Index(line, "}")
		for kv := range strings.SplitSeq(line[k+1:end], ",") {
			name, val, _ := strings.Cut(kv, "=")
			labels[name] = strings.Trim(val, `"`)
		}
		rest = line[end+1:]
	} else if k := strings.Index(line, " "); k >= 0 {
		rest = line[k:]
	}
	value, _ := strconv.ParseFloat(strings.TrimSpace(rest), 64)
	return labels, value
}
