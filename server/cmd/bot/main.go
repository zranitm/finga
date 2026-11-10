// Command bot drives test players against a running game, then prints what happened and the tick p99.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

func main() {
	url := flag.String("url", "http://localhost:8080", "the gateway")
	n := flag.Int("bots", 4, "how many bots")
	duration := flag.Duration("duration", 30*time.Minute, "how long they play")
	prefix := flag.String("prefix", "bot", "account name prefix (letters and digits)")
	metrics := flag.String("metrics", "http://localhost:9104", "the world service's ops address, where the tick and grant batch metrics are")
	itemMetrics := flag.String("item-metrics", "http://localhost:9103", "the item service's ops address, where the transaction times are")
	flag.Parse()
	if err := run(*url, *metrics, *itemMetrics, *n, *duration, *prefix); err != nil {
		fmt.Fprintln(os.Stderr, "bot:", err)
		os.Exit(1)
	}
}

func run(url, metrics, itemMetrics string, n int, duration time.Duration, prefix string) error {
	start := time.Now()
	stamp := start.Format("150405")
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	bots := make([]*bot, 0, n)
	var wg sync.WaitGroup
	for k := range n {
		short := prefix
		if len(short) > 3 {
			short = short[:3]
		}
		b := newBot(url, fmt.Sprintf("%s%s%d", prefix, stamp, k), strings.ToUpper(short[:1])+short[1:]+stamp+strconv.Itoa(k), k)
		bots = append(bots, b)
		wg.Go(func() { b.play(ctx, bots) })
	}
	wg.Wait()
	fmt.Printf("proof session: %d bots for %s against %s\n\n", n, time.Since(start).Round(time.Second), url)
	var total stats
	for _, b := range bots {
		s := b.snapshot()
		total.add(s)
		fmt.Printf("%-14s kills %3d  items collected %3d  deaths %2d  hops %3d  chat %3d  messages %7d  refused %3d  reconnects %d  login failures %d\n",
			b.name, s.kills, s.items, s.deaths, s.hops, s.chats, s.messages, s.refused, s.reconnects, s.loginFailures)
	}
	fmt.Printf("%-14s kills %3d  items collected %3d  deaths %2d  hops %3d  chat %3d  messages %7d  refused %3d  reconnects %d  login failures %d\n\n",
		"total", total.kills, total.items, total.deaths, total.hops, total.chats, total.messages, total.refused, total.reconnects, total.loginFailures)
	return report(metrics, itemMetrics, url)
}

type stats struct {
	messages, kills, items, deaths, hops, chats, refused, reconnects, loginFailures int
}

func (s *stats) add(o stats) {
	s.messages += o.messages
	s.kills += o.kills
	s.items += o.items
	s.deaths += o.deaths
	s.hops += o.hops
	s.chats += o.chats
	s.refused += o.refused
	s.reconnects += o.reconnects
	s.loginFailures += o.loginFailures
}

type bot struct {
	url      string
	name     string
	charName string // 3 to 12 letters and digits
	index    int
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
	return &bot{url: url, name: name, charName: charName, index: index, codec: protocol.ClientCodec(wsconn.Binary), rng: rand.New(rand.NewPCG(uint64(index), uint64(time.Now().UnixNano()))), kills: newKills()}
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
	created, err := b.rest(ctx, "POST", "/api/characters", token, map[string]string{"name": b.charName, "class": []string{"warrior", "bowman"}[b.index%2]})
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
	b.seq++
	_ = b.send(wsconn.Message{Seq: b.seq, Data: d})
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
	case *clientv1.Snapshot:
		for _, v := range d.GetPlayers() {
			if v.GetId() == b.id {
				b.x, b.y, b.dead = v.GetX(), v.GetY(), v.GetDead()
			}
		}
	case *clientv1.Delta:
		for _, v := range d.GetPlayers() {
			if v.GetId() == b.id {
				b.x, b.y, b.dead = v.GetX(), v.GetY(), v.GetDead()
			}
		}
		b.killed(b.kills.removedMobs(m.Tick, d.GetRemovedMobs(), b.id))
	case *clientv1.Damage:
		b.killed(b.kills.blow(m.Tick, d, b.id))
	case *clientv1.MapChange:
		b.mapID, b.x, b.y = d.GetMapId(), d.GetX(), d.GetY()
		b.held = &clientv1.Input{}
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
		}
	case *clientv1.Loot:
		if d.GetKind() == clientv1.LootKind_LOOT_KIND_COLLECTED && d.GetOwnerId() == b.id {
			b.count(func(s *stats) { s.items++ })
		}
	case *clientv1.Response:
		if !d.GetOk() {
			b.count(func(s *stats) { s.refused++ })
		}
	case *clientv1.PartyInvited:
		b.command(&clientv1.PartyRespond{InviteId: d.GetInviteId(), Accept: true})
	case *clientv1.PartyState:
		b.inParty.Store(d.GetParty() != nil)
	}
}

// loop makes 20 decisions a second until the connection closes or the session ends.
func (b *bot) loop(ctx context.Context, all []*bot) {
	const (
		townPortal   = 1216.0 // the town's to_forest arrival point
		forestPortal = 64.0   // the forest's from_town arrival point
		forestLeft   = 260.0
		forestRight  = 720.0
	)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	target := forestRight
	nextAttack, nextChat, nextInvite := time.Now(), time.Now().Add(time.Duration(10+b.index*7)*time.Second), time.Now().Add(20*time.Second)
	leaveForestAt := time.Now().Add(time.Duration(8+b.index) * time.Minute)
	lastX, stuck := b.x, 0
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
		if math.Abs(b.x-lastX) < 1 {
			stuck++
		} else {
			stuck = 0
		}
		lastX = b.x
		jump := stuck >= 4
		switch b.mapID {
		case "town":
			if b.walkTo(townPortal, jump) {
				b.command(&clientv1.EnterPortal{})
				leaveForestAt = time.Now().Add(time.Duration(8+b.index) * time.Minute)
			}
		case "forest":
			if time.Now().After(leaveForestAt) {
				if b.walkTo(forestPortal, jump) {
					b.command(&clientv1.EnterPortal{})
				}
				continue
			}
			if b.walkTo(target, jump) { // drops fly to their owners: the bot only hunts
				if target == forestRight {
					target = forestLeft
				} else {
					target = forestRight
				}
			}
			if time.Now().After(nextAttack) {
				b.command(&clientv1.Attack{})
				nextAttack = time.Now().Add(900 * time.Millisecond)
			}
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
		default: // the cave or anywhere else: go back where we came from
			b.command(&clientv1.EnterPortal{})
		}
	}
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
