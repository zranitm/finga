package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"finga/internal/gamedata/gamedatatest"
	"finga/internal/kernel"
	"finga/modules/character/internal/application"
	"finga/modules/character/internal/model"
)

// Concurrent creates for one account count one by one, so the cap holds under a race.
func TestConcurrentCreatesKeepTheCharacterCap(t *testing.T) {
	ctx := context.Background()
	data := gamedatatest.Shipped(t)
	store := NewPostgres(cluster.DB(t), nil)
	svc := application.NewService(application.Deps{Store: store, Content: data, Starter: granted{}, Logger: slog.New(slog.DiscardHandler)})
	account := kernel.NewAccountID()
	const n = 6
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			_, errs[i] = svc.Create(ctx, account, application.CreateInput{Name: fmt.Sprintf("Hero%d", i)})
		})
	}
	wg.Wait()
	created, full := 0, 0
	for i, err := range errs {
		switch {
		case err == nil:
			created++
		case errors.Is(err, model.ErrSlotsFull):
			full++
		default:
			t.Fatalf("create %d: %v", i, err)
		}
	}
	list, err := store.List(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	if created != 3 || full != n-3 || len(list) != 3 {
		t.Fatalf("%d created, %d refused as full, %d stored; want 3, %d, 3", created, full, len(list), n-3)
	}
}

// granted is an item module that grants every starter set.
type granted struct{}

func (granted) GrantStarter(context.Context, kernel.AccountID, kernel.CharacterID) error {
	return nil
}
func (granted) Retryable(error) bool { return false }
