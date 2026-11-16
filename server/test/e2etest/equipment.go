package e2etest

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/item"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/pkg/uuidv7"
	"finga/pkg/wsconn"
)

// Actor is the character as the item module names who acts.
func (s *Stack) Actor(t *testing.T, id string) item.Actor {
	t.Helper()
	var account string
	if err := s.DB.QueryRow(context.Background(), `SELECT account_id FROM characters WHERE id = $1`, id).Scan(&account); err != nil {
		t.Fatal(err)
	}
	return item.Actor{Account: MustID(t, accountID, account), Character: MustID(t, kernel.ParseCharacterID, id)}
}

// accountID is s as an account's ID, as the database holds it.
func accountID(s string) (kernel.AccountID, error) {
	var id kernel.AccountID
	err := id.UnmarshalText([]byte(s))
	return id, err
}

// MustID is s parsed by parse.
func MustID[ID any](t *testing.T, parse func(string) (ID, error), s string) ID {
	t.Helper()
	id, err := parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Give adds an item through the item module, as a drop would, and returns its ID.
func (s *Stack) Give(t *testing.T, id, template string, stats *progression.ItemStats) string {
	t.Helper()
	key := uuidv7.New()
	res, err := s.Items.Add(context.Background(), s.Actor(t, id), item.AddRequest{Key: key.String(), TemplateID: template, Quantity: 1, Cause: item.Cause{Type: item.CauseDrop, ID: key}, Stats: stats})
	if err != nil {
		t.Fatal(err)
	}
	return res.Changed[len(res.Changed)-1].Item.ID.String()
}

// Worn is what the database says the character wears, item ID by equip slot.
func (s *Stack) Worn(t *testing.T, id string) map[string]string {
	t.Helper()
	rows, err := s.DB.Query(context.Background(), `SELECT equip_slot, id::text FROM items WHERE owner_id = $1 AND location = 'equipped'`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var slot, item string
		if err := rows.Scan(&slot, &item); err != nil {
			t.Fatal(err)
		}
		out[slot] = item
	}
	return out
}

// Hello joins over a new connection and returns the Welcome with a looter on the Snapshot.
func (s *Stack) Hello(t *testing.T, token, id string) (*Looter, *clientv1.Welcome) {
	t.Helper()
	p := s.Connect(t, id)
	w, snap := p.Hello(token)
	return NewLooter(p, snap), w
}

// Gather reads until one message of each type has arrived, in any order: a Response and what the world's stream sends race.
func (l *Looter) Gather(types ...string) map[string]wsconn.Message {
	l.t.Helper()
	got := map[string]wsconn.Message{}
	l.Pump(5*time.Second, func(m wsconn.Message) bool {
		for _, typ := range types {
			if _, seen := got[typ]; m.Type == typ && !seen {
				got[typ] = m
			}
		}
		return len(got) == len(types)
	})
	if len(got) != len(types) {
		l.t.Fatalf("got %v of %v within 5 s", got, types)
	}
	return got
}

// Key is a fresh idempotency key.
func Key() string { return "k-" + uuidv7.NewString() }
