// Package outbox writes domain events in the same transaction as the change.
package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"finga/pkg/dbx"
	"finga/pkg/uuidv7"
)

// Event is one domain event; Add fills a zero ID, OccurredAt and ContentType.
type Event struct {
	ID          uuid.UUID
	Topic       string
	Key         string
	Type        string
	ContentType string
	Payload     []byte
	OccurredAt  time.Time
}

// DefaultContentType is used for an Event without one.
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

// Unpublished counts the rows a relay has not published yet.
func Unpublished(ctx context.Context, db dbx.DBTX) (int64, error) {
	var n int64
	err := db.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE published_at IS NULL`).Scan(&n)
	return n, err
}

const insertSQL = `INSERT INTO outbox (event_id, topic, key, type, content_type, payload, occurred_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`

// Add implements Writer.
func (w *writer) Add(ctx context.Context, events ...Event) error {
	for _, e := range events {
		if e.ID == uuid.Nil {
			e.ID = uuidv7.New()
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
