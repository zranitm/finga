package e2e_test

import (
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// smallCells cuts the 72 m forest in 6 m cells, so a block of 7 by 7 is 42 m wide: three rows behind still cover the 17 m of the longest reach.
var smallCells = e2etest.DataEdit{File: "world.yaml", Old: "interest:\n  cell: 32\n  behind: 1\n", New: "interest:\n  cell: 6\n  behind: 3\n"}

// cellEdge is the line between the forest's 6 m columns 3 and 4: Ayla's block, round column 0, ends at it.
const cellEdge = 24.0

// Region step 1a through the whole stack: on a map cut by cells, a player comes into another's frames whole as they cross into their block and goes out of them, as unseen, as they leave it, and a swing three cells off is heard only from inside it.
func TestPlayersOnAMapCutByCellsComeAndGoAtTheBlocksEdge(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, append(stillSlimes(), smallCells)...))
	robinToken, robinID := s.CreateCharacter(t, "robin", "Robin", "beginner")
	s.Place(t, robinID, "forest", gamedata.Point{X: cellEdge + 1, Z: 26}, 135, 0) // column 4, clear of props and mobs
	aylaToken, aylaID := s.CreateCharacter(t, "ayla", "Ayla", "beginner")
	s.Place(t, aylaID, "forest", gamedata.Point{X: 4, Z: 26}, 135, 0) // column 0
	robin := clientOf(t, s, robinToken, robinID)
	ayla := clientOf(t, s, aylaToken, aylaID)
	if ayla.held[robinID] != nil || robin.held[robinID] == nil {
		t.Fatalf("Ayla's snapshot holds Robin %v, out of her block; Robin's holds him %v", ayla.held[robinID] != nil, robin.held[robinID] != nil)
	}

	robin.walk(&clientv1.Input{Left: true}, func(x float64) bool { return x < cellEdge })
	ayla.until("Robin to come", func() bool { return ayla.held[robinID] != nil })
	if v := ayla.came[robinID]; v.GetX() >= cellEdge || v.GetName() != "Robin" {
		t.Fatalf("Ayla was first sent Robin as %+v; want him whole, once in her block", v)
	}
	robin.until("Ayla to come", func() bool { return robin.held[aylaID].GetName() == "Ayla" })
	if tick := robin.swing(); !ayla.heard(robinID, tick) {
		t.Fatal("Ayla didn't hear Robin's swing from inside her block, three cells off")
	}

	robin.walk(&clientv1.Input{Right: true}, func(x float64) bool { return x >= cellEdge })
	ayla.until("Robin to go out of sight", func() bool { return ayla.unseen[robinID] && ayla.held[robinID] == nil })
	robin.until("Ayla to go out of sight", func() bool { return robin.unseen[aylaID] && robin.held[aylaID] == nil })
	if tick := robin.swing(); ayla.heard(robinID, tick) {
		t.Fatal("Ayla heard Robin's swing from outside her block")
	}
}

// client is what a player's client holds of the players round it, folded from its snapshot and every message after it as they're read.
type client struct {
	t      *testing.T
	p      *e2etest.Player
	id     string
	held   map[string]*clientv1.PlayerView
	came   map[string]*clientv1.PlayerView // each player as they first came after the snapshot
	unseen map[string]bool                 // those a frame said went out of sight
	swings map[string][]uint64             // each attacker's swings heard, by tick
	tick   uint64                          // the latest frame's
}

func clientOf(t *testing.T, s *e2etest.Stack, token, id string) *client {
	t.Helper()
	p, snap := s.Join(t, token, id)
	c := &client{t: t, p: p, id: id, held: map[string]*clientv1.PlayerView{}, came: map[string]*clientv1.PlayerView{}, unseen: map[string]bool{}, swings: map[string][]uint64{}}
	for _, v := range snap.GetPlayers() {
		c.held[v.GetId()] = v
	}
	return c
}

// read folds the next message in, if one comes within a second.
func (c *client) read() {
	m, ok := c.p.Read(time.Second)
	if !ok {
		return
	}
	switch d := m.Data.(type) {
	case *clientv1.Delta:
		c.tick = m.Tick
		for _, v := range d.GetPlayers() {
			if c.held[v.GetId()] == nil {
				c.came[v.GetId()] = v
			}
			c.held[v.GetId()] = v
		}
		for _, id := range d.GetRemovedPlayers() {
			delete(c.held, id)
		}
		for _, id := range d.GetUnseen().GetPlayers() {
			delete(c.held, id)
			c.unseen[id] = true
		}
	case *clientv1.AttackUsed:
		c.swings[d.GetAttacker()] = append(c.swings[d.GetAttacker()], m.Tick)
	}
}

// until reads until done, for up to 15 s of the world's ticks.
func (c *client) until(what string, done func() bool) {
	c.t.Helper()
	for game := c.p.Game(15 * time.Second); !done(); c.read() {
		if !game.Left() {
			c.t.Fatalf("%s: no %s", c.id, what)
		}
	}
}

// walk holds key until the player's own view is where done says, then lets go.
func (c *client) walk(key *clientv1.Input, done func(x float64) bool) {
	c.t.Helper()
	for game := c.p.Game(15 * time.Second); !done(c.held[c.id].GetX()); c.read() {
		if !game.Left() {
			c.t.Fatalf("%s never got there, at %v", c.id, c.held[c.id].GetX())
		}
		c.p.Send(wsconn.Message{Seq: c.p.NextSeq(), Data: key})
	}
	c.p.Send(wsconn.Message{Seq: c.p.NextSeq(), Data: &clientv1.Input{}})
}

// swing attacks and returns the tick its own client heard it at.
func (c *client) swing() uint64 {
	c.t.Helper()
	before := len(c.swings[c.id])
	c.p.Send(wsconn.Message{Seq: c.p.NextSeq(), Data: &clientv1.Attack{Aim: 1.5}})
	c.until("swing of its own", func() bool { return len(c.swings[c.id]) > before })
	return c.swings[c.id][len(c.swings[c.id])-1]
}

// heard is whether c heard attacker's swing of that tick by the first frame past it, when the tick's events have all been sent.
func (c *client) heard(attacker string, tick uint64) bool {
	c.t.Helper()
	c.until("frame past the swing", func() bool { return c.tick > tick })
	return slices.Contains(c.swings[attacker], tick)
}
