// Package outbox is the domain events a change writes in its own transaction; outboxpg stores them in Postgres.
package outbox

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Event is one domain event; a Writer fills a zero ID, OccurredAt and ContentType.
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
