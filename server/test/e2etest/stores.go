package e2etest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Left is what one column of a store holds, in order.
func (s *Stack) Left(t *testing.T, query string) []string {
	t.Helper()
	rows, err := s.DB.Query(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}
