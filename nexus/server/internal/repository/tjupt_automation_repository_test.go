package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTjuptAutomationQuotaAndLeaseFencing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo, err := NewTjuptAutomationRepository(db, "test policy", 5)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = repo.SaveControl(ctx, "test policy", true, 5, 1000, "qb", "/torrent-test"); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Claim(ctx, "test policy", "first", 1000, 1180); !got || err != nil {
		t.Fatalf("first claim: %v %v", got, err)
	}
	if got, err := repo.Claim(ctx, "test policy", "parallel", 1000, 1180); got || err != nil {
		t.Fatalf("parallel claim: %v %v", got, err)
	}
	if err := repo.SaveControl(ctx, "new rules", false, 5, 1001, "", ""); !errors.Is(err, ErrTjuptAutomationBusy) {
		t.Fatalf("settings changed the active lease: %v", err)
	}
	if err := repo.HoldUntilExpiry(ctx, "first", "unconfirmed"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveControl(ctx, "new rules", false, 5, 1140, "", ""); !errors.Is(err, ErrTjuptAutomationBusy) {
		t.Fatalf("an unconfirmed write unlocked active policy: %v", err)
	}

	type result struct {
		ok  bool
		err error
	}
	var wg sync.WaitGroup
	results := make(chan result, 2)
	for i := 1; i <= 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task := TjuptAutomationTask{Hash: fmt.Sprintf("%040x", i), SourceID: fmt.Sprint(i), SizeBytes: 6,
				State: "adding", DownloaderID: "qb", SaveRoot: "/torrent-test"}
			ok, err := repo.Reserve(ctx, &task, "first", 1001, 10, 3)
			results <- result{ok: ok, err: err}
		}(i)
	}
	wg.Wait()
	close(results)
	count := 0
	for row := range results {
		if row.err != nil {
			t.Fatal(row.err)
		}
		if row.ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("atomic reservations over 10-byte budget: %d", count)
	}
	tasks, err := repo.LiveTasks(ctx)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("unexpected quota ledger: %v %v", tasks, err)
	}
	if got, err := repo.Claim(ctx, "test policy", "second", 1300, 1480); !got || err != nil {
		t.Fatalf("expired claim did not advance: %v %v", got, err)
	}
	stale := tasks[0]
	stale.State = "deleted"
	if err := repo.UpdateTask(ctx, stale, "first", 1300); !errors.Is(err, ErrTjuptAutomationLeaseLost) {
		t.Fatalf("stale lease changed quota: %v", err)
	}
	if err := repo.Finish(ctx, "first", "late"); err != nil {
		t.Fatal(err)
	}
	if err := repo.VerifyLease(ctx, "second", 1301); err != nil {
		t.Fatal(err)
	}
	if err = repo.Finish(ctx, "second", "done"); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveControl(ctx, "different", true, 5, 1302, "other-qb", "/torrent-test"); !errors.Is(err, ErrTjuptAutomationScope) {
		t.Fatalf("switched scopes with outstanding reservation: %v", err)
	}
	if got, err := repo.Claim(ctx, "test policy", "third", 1600, 1780); !got || err != nil {
		t.Fatalf("expected recovery lease: %v %v", got, err)
	}
	if err := repo.ReleaseUnappliedAdd(ctx, tasks[0].Hash, "second", 1600); !errors.Is(err, ErrTjuptAutomationLeaseLost) {
		t.Fatalf("stale lease released reservation: %v", err)
	}
	if err := repo.ReleaseUnappliedAdd(ctx, tasks[0].Hash, "third", 1600); err != nil {
		t.Fatal(err)
	}
	if tasks, err := repo.LiveTasks(ctx); err != nil || len(tasks) != 0 {
		t.Fatalf("certified unused reservation not released: %v %v", tasks, err)
	}
}
