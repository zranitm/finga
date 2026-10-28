// Command bot drives test players against a running game for the
// proof session (step 17): each bot signs up, enters the world, walks from
// the town to the forest, hunts, loots, chats, parties up with the others,
// and goes back to the town now and then. At the end it reads the world
// service's /metrics (its ops address, -metrics) and the gateway's, and
// prints what happened, including the tick p99, so a session can be put in
// the evidence log.
//
//	go run ./cmd/bot -url http://localhost:8080 -metrics http://localhost:9104 -bots 4 -duration 30m
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

	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

func main() {
	url := flag.String("url", "http://localhost:8080", "the gateway")
	n := flag.Int("bots", 4, "how many bots")
	duration := flag.Duration("duration", 30*time.Minute, "how long they play")
	prefix := flag.String("prefix", "bot", "account name prefix (letters and digits)")
	metrics := flag.String("metrics", "http://localhost:9104", "the world service's ops address, where the tick metrics are")
	flag.Parse()
	if err := run(*url, *metrics, *n, *duration, *prefix); err != nil {
		fmt.Fprintln(os.Stderr, "bot:", err)
		os.Exit(1)
	}
}

func run(url, metrics string, n int, duration time.Duration, prefix string) error {
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
		wg.Add(1)
		go func() { defer wg.Done(); b.play(ctx, bots) }()
	}
	wg.Wait()
	fmt.Printf("proof session: %d bots for %s against %s\n\n", n, time.Since(start).Round(time.Second), url)
	var total stats
	for _, b := range bots {
		s := b.snapshot()
		total.add(s)
		fmt.Printf("%-14s kills %3d  pickups %3d  deaths %2d  hops %3d  chat %3d  messages %7d  refused %3d  reconnects %d  login failures %d\n",
			b.name, s.kills, s.pickups, s.deaths, s.hops, s.chats, s.messages, s.refused, s.reconnects, s.loginFailures)
	}
	fmt.Printf("%-14s kills %3d  pickups %3d  deaths %2d  hops %3d  chat %3d  messages %7d  refused %3d  reconnects %d  login failures %d\n\n",
		"total", total.kills, total.pickups, total.deaths, total.hops, total.chats, total.messages, total.refused, total.reconnects, total.loginFailures)
	return report(metrics, url)
}

// stats is what one bot saw.
type stats struct {
	messages, kills, pickups, deaths, hops, chats, refused, reconnects, loginFailures int
}

func (s *stats) add(o stats) {
	s.messages += o.messages
	s.kills += o.kills
	s.pickups += o.pickups
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
	codec    *wsconn.JSONCodec
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
	drops   map[uint64]protocol.DropView
	seq     uint32
	held    protocol.Input
	inParty atomic.Bool
}

func newBot(url, name, charName string, index int) *bot {
	return &bot{url: url, name: name, charName: charName, index: index, codec: protocol.NewCodec(), rng: rand.New(rand.NewPCG(uint64(index), uint64(time.Now().UnixNano())))}
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

// play signs up once, then enters the world and plays until ctx ends,
// reconnecting when the connection drops.
func (b *bot) play(ctx context.Context, all []*bot) {
	token, err := b.signUp(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: sign up: %v\n", b.name, err)
		return
	}
	for ctx.Err() == nil {
		if token == "" {
			// A dropped connection: log in again (the login throttle is 5 per
			// minute per IP, so wait a little between attempts).
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

// signUp registers, logs in once, creates the character, and returns the
// login token for the first Hello.
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

// enter connects and says Hello with the token.
func (b *bot) enter(ctx context.Context, token string) error {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, "ws"+strings.TrimPrefix(b.url, "http")+"/ws", nil)
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
	b.drops = map[uint64]protocol.DropView{}
	b.dead = false
	b.held = protocol.Input{}
	if err := b.send(wsconn.Message{Data: protocol.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterID: b.id}}); err != nil {
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
	return b.conn.Write(context.Background(), websocket.MessageText, data)
}

func (b *bot) input(in protocol.Input) {
	if in == b.held {
		return
	}
	b.held = in
	b.seq++
	_ = b.send(wsconn.Message{Seq: b.seq, Data: in})
}

func (b *bot) command(d any) {
	b.seq++
	_ = b.send(wsconn.Message{Seq: b.seq, Data: d})
}

// observe updates the bot's view of the world from one message.
func (b *bot) observe(m wsconn.Message) {
	b.count(func(s *stats) { s.messages++ })
	switch d := m.Data.(type) {
	case protocol.Welcome:
		b.token = d.Token
		b.mapID = d.MapID
	case protocol.Snapshot:
		b.drops = map[uint64]protocol.DropView{}
		for _, dv := range d.Drops {
			b.drops[dv.ID] = dv
		}
		for _, v := range d.Players {
			if v.ID == b.id {
				b.x, b.y, b.dead = v.X, v.Y, v.Dead
			}
		}
	case protocol.Delta:
		for _, v := range d.Players {
			if v.ID == b.id {
				b.x, b.y, b.dead = v.X, v.Y, v.Dead
			}
		}
		for _, dv := range d.Drops {
			b.drops[dv.ID] = dv
		}
		for _, id := range d.RemovedDrops {
			delete(b.drops, id)
		}
	case protocol.MapChange:
		b.mapID, b.x, b.y = d.MapID, d.X, d.Y
		b.held = protocol.Input{}
		b.count(func(s *stats) { s.hops++ })
	case protocol.Died:
		if d.CharacterID == b.id {
			b.dead = true
			b.count(func(s *stats) { s.deaths++ })
		}
	case protocol.Respawned:
		if d.CharacterID == b.id {
			b.dead = false
			b.mapID, b.x, b.y = d.MapID, d.X, d.Y
			b.held = protocol.Input{}
		}
	case protocol.ExpGained:
		b.count(func(s *stats) { s.kills++ })
	case protocol.InventoryChanged:
		b.count(func(s *stats) { s.pickups++ })
	case protocol.Response:
		if !d.OK {
			b.count(func(s *stats) { s.refused++ })
		}
	case protocol.PartyInvited:
		b.command(protocol.PartyRespond{InviteID: d.InviteID, Accept: true})
	case protocol.PartyState:
		b.inParty.Store(d.Party != nil)
	}
}

// loop is one connection's life: 20 decisions a second until the
// connection closes or the session ends.
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
			b.input(protocol.Input{})
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
				b.command(protocol.EnterPortal{})
				leaveForestAt = time.Now().Add(time.Duration(8+b.index) * time.Minute)
			}
		case "forest":
			if time.Now().After(leaveForestAt) {
				if b.walkTo(forestPortal, jump) {
					b.command(protocol.EnterPortal{})
				}
				continue
			}
			if drop, ok := b.nearestDrop(); ok && math.Abs(drop.X-b.x) < 160 {
				if math.Abs(drop.X-b.x) < 12 {
					b.input(protocol.Input{})
					b.command(protocol.PickUp{})
				} else {
					b.walkTo(drop.X, jump)
				}
			} else {
				if b.walkTo(target, jump) {
					if target == forestRight {
						target = forestLeft
					} else {
						target = forestRight
					}
				}
				if time.Now().After(nextAttack) {
					b.command(protocol.Attack{})
					nextAttack = time.Now().Add(900 * time.Millisecond)
				}
			}
			if time.Now().After(nextChat) {
				b.command(protocol.ChatSend{Channel: protocol.ChatMap, Text: fmt.Sprintf("%s at %.0f, %d kills", b.name, b.x, b.snapshot().kills)})
				b.count(func(s *stats) { s.chats++ })
				nextChat = time.Now().Add(time.Duration(25+b.rng.IntN(20)) * time.Second)
			}
			if b.index == 0 && time.Now().After(nextInvite) {
				for _, other := range all {
					if other != b && other.id != "" && !other.inParty.Load() {
						b.command(protocol.PartyInvite{CharacterID: other.id})
						break
					}
				}
				nextInvite = time.Now().Add(15 * time.Second)
			}
		default: // the cave or anywhere else: go back where we came from
			b.command(protocol.EnterPortal{})
		}
	}
}

// walkTo holds a direction toward x and reports standing within 12 px.
func (b *bot) walkTo(x float64, jump bool) bool {
	dx := x - b.x
	if math.Abs(dx) < 12 {
		b.input(protocol.Input{})
		return true
	}
	b.input(protocol.Input{Right: dx > 0, Left: dx < 0, Jump: jump})
	return false
}

func (b *bot) nearestDrop() (protocol.DropView, bool) {
	var best protocol.DropView
	bestDist := math.Inf(1)
	for _, d := range b.drops {
		if dist := math.Abs(d.X - b.x); dist < bestDist {
			best, bestDist = d, dist
		}
	}
	return best, bestDist < math.Inf(1)
}

// scrape reads one /metrics page.
func scrape(url string) ([]byte, error) {
	res, err := http.Get(url) //nolint:gosec // the operator names the address
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}

// report reads /metrics from each address (the world's ticks, the game
// server's players) and prints the tick histogram's percentiles per map and
// overall, the overruns, and the players online.
func report(urls ...string) error {
	var raw []byte
	for _, url := range urls {
		body, err := scrape(url + "/metrics")
		if err != nil {
			return err
		}
		raw = append(append(raw, body...), '\n')
	}
	type hist struct {
		buckets map[float64]float64 // le → cumulative count
		count   float64
	}
	byMap := map[string]*hist{}
	overruns := map[string]float64{}
	online := ""
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "tick_duration_seconds_bucket"):
			labels, value := parseLine(line)
			h := byMap[labels["map"]]
			if h == nil {
				h = &hist{buckets: map[float64]float64{}}
				byMap[labels["map"]] = h
			}
			le, _ := strconv.ParseFloat(labels["le"], 64)
			h.buckets[le] = value
		case strings.HasPrefix(line, "tick_duration_seconds_count"):
			labels, value := parseLine(line)
			if h := byMap[labels["map"]]; h != nil {
				h.count = value
			}
		case strings.HasPrefix(line, "tick_overruns_total"):
			labels, value := parseLine(line)
			overruns[labels["map"]] = value
		case strings.HasPrefix(line, "players_online "):
			online = strings.TrimSpace(strings.TrimPrefix(line, "players_online")) // the last page's: the gateway's
		}
	}
	if online != "" {
		fmt.Printf("players_online %s\n", online)
	}
	all := &hist{buckets: map[float64]float64{}}
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
		fmt.Printf("%-8s ticks %8.0f  p50 %s  p99 %s  overruns %.0f\n", name, h.count, percentile(h.buckets, h.count, 0.50), percentile(h.buckets, h.count, 0.99), overruns[name])
	}
	for _, name := range names {
		line(name, byMap[name])
	}
	line("all", all)
	return nil
}

// percentile reads a Prometheus histogram the way histogram_quantile
// does: the bucket where the cumulative count crosses the rank, with
// linear interpolation inside it.
func percentile(buckets map[float64]float64, count float64, q float64) string {
	if count == 0 {
		return "n/a"
	}
	les := make([]float64, 0, len(buckets))
	for le := range buckets {
		les = append(les, le)
	}
	sort.Float64s(les)
	rank := q * count
	prevLE, prevCount := 0.0, 0.0
	for _, le := range les {
		c := buckets[le]
		if c >= rank {
			if math.IsInf(le, 1) {
				return fmt.Sprintf("> %.1f ms", prevLE*1000)
			}
			frac := 0.0
			if c > prevCount {
				frac = (rank - prevCount) / (c - prevCount)
			}
			return fmt.Sprintf("%.2f ms (bucket ≤ %.0f ms)", (prevLE+(le-prevLE)*frac)*1000, le*1000)
		}
		prevLE, prevCount = le, c
	}
	return "n/a"
}

func parseLine(line string) (map[string]string, float64) {
	labels := map[string]string{}
	rest := line
	if k := strings.Index(line, "{"); k >= 0 {
		end := strings.Index(line, "}")
		for _, kv := range strings.Split(line[k+1:end], ",") {
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
