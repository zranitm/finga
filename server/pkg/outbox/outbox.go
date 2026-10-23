// Package outbox is the transactional outbox: a module writes its domain
// events to the outbox table inside the same transaction as the change, so
// a committed change always has its event and an uncommitted one never
// does. Stage 2 adds the relay that publishes unsent rows.
package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"finga/pkg/dbx"
)

// Event is one domain event to publish. A zero ID gets a new one; a zero
// OccurredAt is now. ContentType defaults to application/json.
type Event struct {
	ID          uuid.UUID
	Topic       string
	Key         string
	Type        string
	ContentType string
	Payload     []byte
	OccurredAt  time.Time
}

// DefaultContentType is Stage 1's encoding.
const DefaultContentType = "application/json"

// Writer appends events inside the caller's transaction.
type Writer interface {
	Add(ctx context.Context, events ...Event) error
}

// NewWriter binds a writer to db, usually a transaction.
func NewWriter(db dbx.DBTX) Writer {
	return &writer{db: db, now: time.Now}
}

type writer struct {
	db  dbx.DBTX
	now func() time.Time
}

const insertSQL = `INSERT INTO outbox (event_id, topic, key, type, content_type, payload, occurred_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`

// Add inserts the events, filling in IDs, times and the content type.
func (w *writer) Add(ctx context.Context, events ...Event) error {
	for _, e := range events {
		if e.ID == uuid.Nil {
			id, err := uuid.NewV7()
			if err != nil {
				return fmt.Errorf("outbox: event id: %w", err)
			}
			e.ID = id
		}
		if e.OccurredAt.IsZero() {
			e.OccurredAt = w.now()
		}
		if e.ContentType == "" {
			e.ContentType = DefaultContentType
		}
		if e.Topic == "" || e.Type == "" {
			return fmt.Errorf("outbox: event %s needs a topic and a type", e.ID)
		}
		if e.Payload == nil {
			e.Payload = []byte("{}")
		}
		if _, err := w.db.Exec(ctx, insertSQL, e.ID, e.Topic, e.Key, e.Type, e.ContentType, e.Payload, e.OccurredAt); err != nil {
			return fmt.Errorf("outbox: add %s: %w", e.Type, err)
		}
	}
	return nil
}
