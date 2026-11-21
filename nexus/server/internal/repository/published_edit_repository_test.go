package repository

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestPublishedEditReservationSurvivesConcurrentConfirmation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := &Store{DB: db}
	repo, err := NewPublishedEditRepository(store)
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	var group sync.WaitGroup
	for logID := uint64(1); logID <= 8; logID++ {
		group.Add(1)
		go func(id uint64) {
			defer group.Done()
			attempt := PublishedEditAttempt{PublishLogID: id, TargetID: "12", Revision: strings.Repeat("a", 64), Status: "pending"}
			ok, err := repo.Reserve(context.Background(), &attempt)
			if err != nil {
				t.Error(err)
			}
			if ok {
				accepted.Add(1)
				if err := repo.Complete(context.Background(), attempt.ID, "unknown"); err != nil {
					t.Error(err)
				}
			}
		}(logID)
	}
	group.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d requests for the same target revision", accepted.Load())
	}
	reopened, err := NewPublishedEditRepository(store)
	if err != nil {
		t.Fatal(err)
	}
	row := PublishedEditAttempt{PublishLogID: 50, TargetID: "12", Revision: strings.Repeat("a", 64)}
	if ok, err := reopened.Reserve(context.Background(), &row); ok || err != nil {
		t.Fatalf("uncertain request was not fenced on restart: ok=%t err=%v", ok, err)
	}
}
