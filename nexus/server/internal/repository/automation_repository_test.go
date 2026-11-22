package repository

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func automationTestRepo(t *testing.T) *AutomationRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "automation.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = db.AutoMigrate(&PublishQueueTask{}); err != nil {
		t.Fatal(err)
	}
	repo, err := NewAutomationRepository(&Store{DB: db, DBType: "sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	return repo
}
func automationTestTask(id string) AutomationTask {
	return AutomationTask{ID: id, Source: "source", Target: "target", SourceID: id, InfoHash: fmt.Sprintf("%040s", id), Month: "2026-10", Size: 10, State: "reserved", CreatedAt: 1, UpdatedAt: 1}
}
func TestAutomationQuotaConcurrentReservation(t *testing.T) {
	r := automationTestRepo(t)
	var success atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task := automationTestTask(fmt.Sprint(i))
			if err := r.Reserve(&task, 6, 50); err == nil {
				success.Add(1)
			} else if !errors.Is(err, ErrAutomationQuota) {
				t.Errorf("reserve: %v", err)
			}
		}(i)
	}
	wg.Wait()
	q, err := r.Quota("2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if success.Load() != 5 || q.Count != 5 || q.Bytes != 50 {
		t.Fatalf("quota overspend or lost writes: success=%d quota=%+v", success.Load(), q)
	}
}

// TestAutomationDownloadHistoryAcrossRoutesAndMonths 验证同源记录和同哈希记录跨月跨目标排除，拒绝不会扣减新月份配额。
func TestAutomationDownloadHistoryAcrossRoutesAndMonths(t *testing.T) {
	r := automationTestRepo(t)
	old := automationTestTask("original")
	old.InfoHash = strings.Repeat("a", 40)
	old.State = "failed"
	old.DownloadAttempted = true
	if err := r.Reserve(&old, 6, 60); err != nil {
		t.Fatal(err)
	}
	seen, err := r.HasDownloadHistory(old.Source, old.SourceID, old.InfoHash, old.ID)
	if err != nil || seen {
		t.Fatalf("owned task counted as previous history: %v, %v", seen, err)
	}
	for _, sameHash := range []bool{false, true} {
		next := automationTestTask(fmt.Sprintf("new-%t", sameHash))
		next.Month = "2026-11"
		next.Target = "other-target"
		if sameHash {
			next.Source = "different-source"
			next.InfoHash = strings.ToUpper(old.InfoHash)
		} else {
			next.SourceID = old.SourceID
		}
		seen, err := r.HasDownloadHistory(next.Source, next.SourceID, next.InfoHash, "")
		if err != nil || !seen {
			t.Fatalf("previous history bid=%t: %v, %v", sameHash, seen, err)
		}
		if err = r.Reserve(&next, 6, 60); !errors.Is(err, ErrAutomationDuplicate) {
			t.Fatalf("history accepted bid=%t: %v", sameHash, err)
		}
		quota, err := r.Quota(next.Month)
		if err != nil || quota.Count != 0 || quota.Bytes != 0 {
			t.Fatalf("duplicate changed quota: %+v, %v", quota, err)
		}
	}
}

// TestAutomationConcurrentRoutesReserveOneSource 确保同源同哈希并行预留不同目标时只有一项获得配额。
func TestAutomationConcurrentRoutesReserveOneSource(t *testing.T) {
	r := automationTestRepo(t)
	var selected atomic.Int32
	var duplicates atomic.Int32
	var group sync.WaitGroup
	for _, target := range []string{"target-a", "target-b"} {
		group.Add(1)
		go func(target string) {
			defer group.Done()
			task := automationTestTask(target)
			task.Target = target
			task.SourceID = "same-source-id"
			task.InfoHash = strings.Repeat("b", 40)
			switch err := r.Reserve(&task, 6, 60); {
			case err == nil:
				selected.Add(1)
			case errors.Is(err, ErrAutomationDuplicate):
				duplicates.Add(1)
			default:
				t.Errorf("reserve: %v", err)
			}
		}(target)
	}
	group.Wait()
	quota, err := r.Quota("2026-10")
	if err != nil || selected.Load() != 1 || duplicates.Load() != 1 || quota.Count != 1 {
		t.Fatalf("same source selected=%d duplicates=%d quota=%+v err=%v", selected.Load(), duplicates.Load(), quota, err)
	}
}
func TestAutomationLeaseFencingAndOnlyUntouchedRefund(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Unix(1000, 0)
	task := automationTestTask("lease")
	if err := r.Reserve(&task, 6, 60); err != nil {
		t.Fatal(err)
	}
	claimed, err := r.Claim(task.ID, "old", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Claim(task.ID, "other", now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("double claim: %v", err)
	}
	future := now.Add(16 * time.Minute)
	if _, err = r.Claim(task.ID, "new", future); err != nil {
		t.Fatal(err)
	}
	if err = r.Advance(task.ID, claimed.LeaseToken, "reserved", map[string]any{"state": "downloading"}, future); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("stale lease accepted: %v", err)
	}
	if err = r.Cancel(task.ID, future); err != nil {
		t.Fatal(err)
	}
	q, _ := r.Quota(task.Month)
	if q.Count != 0 || q.Bytes != 0 {
		t.Fatalf("untouched cancellation not refunded: %+v", q)
	}
	task = automationTestTask("touched")
	task.DownloadAttempted = true
	if err = r.Reserve(&task, 6, 60); err != nil {
		t.Fatal(err)
	}
	if err = r.Cancel(task.ID, future); err != nil {
		t.Fatal(err)
	}
	q, _ = r.Quota(task.Month)
	if q.Count != 1 || q.Bytes != 10 {
		t.Fatalf("touched reservation released: %+v", q)
	}
}
func TestAutomationHandoffRollbackAndAttemptRecovery(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Now()
	if err := r.SaveSettings(`{"enabled":true,"auto_publish":true}`, false, now); err != nil {
		t.Fatal(err)
	}
	task := automationTestTask("handoff")
	task.State = "ready"
	if err := r.Reserve(&task, 6, 60); err != nil {
		t.Fatal(err)
	}
	claimed, err := r.Claim(task.ID, "owner", now)
	if err != nil {
		t.Fatal(err)
	}
	bad := claimed
	bad.LeaseToken = "stale"
	queue := PublishQueueTask{Trigger: "automation", Status: "queued"}
	if err = r.Enqueue(bad, &queue, now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("stale handoff accepted: %v", err)
	}
	var count int64
	r.db.Model(&PublishQueueTask{}).Count(&count)
	if count != 0 {
		t.Fatalf("orphan queue rows=%d", count)
	}
	queue = PublishQueueTask{Trigger: "automation", Status: "queued"}
	if err = r.Enqueue(claimed, &queue, now); err != nil {
		t.Fatal(err)
	}
	if err = r.Enqueue(claimed, &PublishQueueTask{}, now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("duplicate handoff accepted: %v", err)
	}
	if err = r.BeginPublish(queue.ID, now); err != nil {
		t.Fatal(err)
	}
	if err = r.BeginPublish(queue.ID, now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("duplicate upload marker accepted: %v", err)
	}
	if err = r.RecoverPublishing(now); err != nil {
		t.Fatal(err)
	}
	row, _ := r.Task(task.ID)
	if row.State != "needs_review" || !row.PublishAttempted {
		t.Fatalf("restart lost attempt evidence: %+v", row)
	}
	if err = r.Retry(task.ID, now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("uncertain upload retry accepted: %v", err)
	}
	next := automationTestTask("next")
	next.SourceID = task.SourceID
	next.InfoHash = task.InfoHash
	next.Month = "2026-11"
	if err = r.Reserve(&next, 6, 60); !errors.Is(err, ErrAutomationDuplicate) {
		t.Fatalf("cross month uncertainty ignored: %v", err)
	}
}
func TestAutomationDisabledCannotBeginPublish(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Now()
	task := automationTestTask("disabled")
	task.State = "queued_publish"
	id := int64(5)
	task.QueueID = &id
	if err := r.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.SaveSettings(`{"enabled":false,"auto_publish":true}`, true, now); err != nil {
		t.Fatal(err)
	}
	if err := r.BeginPublish(id, now); err == nil {
		t.Fatal("disabled module began upload")
	}
	row, _ := r.Task(task.ID)
	if row.PublishAttempted {
		t.Fatal("attempt changed while disabled")
	}
}
func TestAutomationEvidenceSurvivesNativeCleanup(t *testing.T) {
	r := automationTestRepo(t)
	old := time.Now().Add(-48 * time.Hour).Format(PublishQueueTimeLayout)
	q := PublishQueueTask{Trigger: "automation", Status: "success", FinishedAt: &old}
	if err := r.db.Create(&q).Error; err != nil {
		t.Fatal(err)
	}
	task := automationTestTask("audit")
	task.State = "published"
	task.PublishAttempted = true
	task.QueueID = &q.ID
	if err := r.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	native := NewPublishQueueRepository(&Store{DB: r.db})
	if _, err := native.CleanupFinishedTasks(time.Now()); err != nil {
		t.Fatal(err)
	}
	row, _ := r.Task(task.ID)
	if !row.PublishAttempted || row.QueueID == nil {
		t.Fatal("publish evidence lost")
	}
	var n int64
	r.db.Model(&PublishQueueTask{}).Where("id = ?", q.ID).Count(&n)
	if n != 1 {
		t.Fatal("automation native evidence prematurely cleaned")
	}
}
func TestAutomationQueueToggleFiltersManualAndAutomatic(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Now()
	for _, trigger := range []string{"queue", "automation"} {
		q := PublishQueueTask{Trigger: trigger, Status: "queued", CreatedAt: now.Format(PublishQueueTimeLayout)}
		if err := r.db.Create(&q).Error; err != nil {
			t.Fatal(err)
		}
	}
	native := NewPublishQueueRepository(&Store{DB: r.db})
	q, ok, err := native.ClaimNextRunnableTaskFiltered(now, false, true)
	if err != nil || !ok || q.Trigger != "automation" {
		t.Fatalf("automation filter: %+v %v %v", q, ok, err)
	}
	q, ok, err = native.ClaimNextRunnableTaskFiltered(now, true, false)
	if err != nil || !ok || q.Trigger != "queue" {
		t.Fatalf("manual filter: %+v %v %v", q, ok, err)
	}
}

// TestAutomationFairPollingReachesBeyondFiftyWaitingTasks 验证长期等待或授权失效的前50条任务不会永久阻塞后续任务。
func TestAutomationFairPollingReachesBeyondFiftyWaitingTasks(t *testing.T) {
	r := automationTestRepo(t)
	for i := 0; i < 61; i++ {
		task := automationTestTask(fmt.Sprintf("fair-%03d", i))
		task.State = "downloading"
		task.CreatedAt = int64(i + 1)
		if err := r.db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for tick := 0; tick < 2; tick++ {
		rows, err := r.WorkTasks()
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			seen[row.ID] = true
			if err = r.TouchPoll(row.ID, time.Unix(int64(tick+1), 0)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(seen) != 61 {
		t.Fatalf("tasks beyond first 50 starved: observed=%d", len(seen))
	}
}
