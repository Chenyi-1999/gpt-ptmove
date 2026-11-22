package repository

import (
	"errors"
	"testing"
	"time"
)

func TestAutomationBatchWaitsForDownloadsAndFencesVerdicts(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC)
	run := AutomationRun{ID: "run", Month: "2026-10", Status: "batch_active", Execute: true}
	if err := r.CreateRun(&run); err != nil {
		t.Fatal(err)
	}
	first := automationTestTask("first")
	first.RunID, first.BatchCheck, first.DownloadAttempted, first.State = run.ID, true, true, AutomationStateWaitingBatch
	second := automationTestTask("second")
	second.RunID, second.BatchCheck, second.DownloadAttempted, second.State = run.ID, true, true, "downloading"
	for _, task := range []*AutomationTask{&first, &second} {
		if err := r.Reserve(task, 2, 1); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := r.ReservedForCurrentBatch(run); err != nil || count != 2 {
		t.Fatalf("interrupted scan could exceed batch size: %d, %v", count, err)
	}
	previous := run
	previous.Selected = 1
	if count, err := r.ReservedForCurrentBatch(previous); err != nil || count != 1 {
		t.Fatalf("previous batch was counted again: %d, %v", count, err)
	}
	if rows, err := r.WaitingBatchTasks(run.ID, now); err != nil || len(rows) != 0 {
		t.Fatalf("batch opened before all downloads ended: %v, %v", rows, err)
	}
	if _, err := r.Claim(first.ID, "single", now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("individual worker acquired a batch-only task: %v", err)
	}
	worker, err := r.Claim(second.ID, "download-worker", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Advance(worker.ID, worker.LeaseToken, worker.State, map[string]any{"state": AutomationStateWaitingBatch}, now); err != nil {
		t.Fatal(err)
	}
	if err = r.Release(worker.ID, worker.LeaseToken); err != nil {
		t.Fatal(err)
	}

	rows, err := r.WaitingBatchTasks(run.ID, now)
	if err != nil || len(rows) != 2 {
		t.Fatalf("complete batch unavailable: %v, %v", rows, err)
	}
	if _, err = r.ClaimBatchTasks(rows[:1], "short-batch", now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("partial batch was claimed: %v", err)
	}
	stale, err := r.ClaimBatchTasks(rows, "first-batch-worker", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ClaimBatchTasks(rows, "second-batch-worker", now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("live batch lease overwritten: %v", err)
	}
	later := now.Add(16 * time.Minute)
	claimed, err := r.ClaimBatchTasks(rows, "current-worker", later)
	if err != nil {
		t.Fatal(err)
	}
	absent, duplicate := false, true
	if err = r.ResolveBatchTask(stale[0], &absent, later); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("expired worker wrote a verdict: %v", err)
	}
	if err = r.ResolveBatchTask(claimed[0], nil, later); err != nil {
		t.Fatal(err)
	}
	if err = r.ResolveBatchTask(claimed[1], &duplicate, later); err != nil {
		t.Fatal(err)
	}
	if rows, err = r.WaitingBatchTasks(run.ID, later); err != nil || len(rows) != 0 {
		t.Fatalf("unknown verdict skipped retry backoff: %v, %v", rows, err)
	}
	q, err := r.Quota(run.Month)
	if err != nil || q.Published != 0 || q.Pending != 1 || q.Bytes != first.Size+second.Size {
		t.Fatalf("duplicate still occupies the month's publish limit or altered bytes: %+v, %v", q, err)
	}
	if resumed, err := r.ResumeBatchRun(run, later); err != nil || resumed {
		t.Fatalf("unfinished batch resumed: %v, %v", resumed, err)
	}

	cleanup, err := r.Claim(claimed[1].ID, "cleanup", later)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Advance(cleanup.ID, cleanup.LeaseToken, cleanup.State, map[string]any{"duplicate_cleanup_attempted": true}, later); err != nil {
		t.Fatal(err)
	}
	if err = r.Cancel(cleanup.ID, later); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("cancellation raced an irreversible cleanup: %v", err)
	}
	later = later.Add(3 * time.Minute)
	rows, err = r.WaitingBatchTasks(run.ID, later)
	if err != nil || len(rows) != 1 {
		t.Fatalf("unknown task did not return for retry: %v, %v", rows, err)
	}
	claimed, err = r.ClaimBatchTasks(rows, "retry", later)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ResolveBatchTask(claimed[0], &absent, later); err != nil {
		t.Fatal(err)
	}
	preparing, err := r.Claim(claimed[0].ID, "media-worker", later)
	if err != nil || preparing.State != "preparing" || preparing.BatchCheckedAt != later.UnixMilli() || preparing.BatchDuplicate {
		t.Fatalf("valid absent verdict did not preserve prepublish evidence: %+v, %v", preparing, err)
	}
	if err = r.Advance(preparing.ID, preparing.LeaseToken, preparing.State, map[string]any{"state": "failed"}, later); err != nil {
		t.Fatal(err)
	}
	if resumed, err := r.ResumeBatchRun(run, later); err != nil || !resumed {
		t.Fatalf("settled batch did not resume the same run: %v, %v", resumed, err)
	}
	if resumed, err := r.ResumeBatchRun(run, later); err != nil || resumed {
		t.Fatalf("same batch was queued twice: %v, %v", resumed, err)
	}
}

func TestAutomationPublicationQuotaTracksShanghaiMonthAcrossRoutes(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC)
	if err := r.SaveSettings(`{"enabled":true,"auto_publish":true,"monthly_limit":1}`, false, now); err != nil {
		t.Fatal(err)
	}
	old := automationTestTask("old-month")
	old.State = "ready"
	newer := automationTestTask("current-month")
	newer.Month, newer.Target, newer.State = "2026-11", "new-destination", "ready"
	oldQueue := enqueueAutomationQuotaFixture(t, r, old, now)
	newQueue := enqueueAutomationQuotaFixture(t, r, newer, now)
	if err := r.BeginPublish(oldQueue.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := r.BeginPublish(newQueue.ID, now); !errors.Is(err, ErrAutomationQuota) {
		t.Fatalf("in-flight prior-month upload bypassed the current-month cap: %v", err)
	}
	if err := r.PublishOutcome(oldQueue.ID, AutomationStateAlreadyExists, "", "", now); err != nil {
		t.Fatal(err)
	}
	if err := r.BeginPublish(newQueue.ID, now); err != nil {
		t.Fatalf("confirmed existing torrent did not release the slot: %v", err)
	}
	if err := r.PublishOutcome(newQueue.ID, "published", "https://target.example/details.php?id=1", "", now); err != nil {
		t.Fatal(err)
	}
	seeding, err := r.Claim(newer.ID, "seeding-check", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Advance(seeding.ID, seeding.LeaseToken, seeding.State, map[string]any{"state": "needs_review"}, now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		month     string
		published int64
	}{{old.Month, 0}, {newer.Month, 1}} {
		q, err := r.Quota(tc.month)
		if err != nil || q.Published != tc.published || q.Pending != 0 || q.Bytes != old.Size {
			t.Fatalf("wrong monthly accounting %s: %+v, %v", tc.month, q, err)
		}
	}
	replacement := automationTestTask("rejected-after-success")
	replacement.Month = newer.Month
	if err := r.Reserve(&replacement, 1, 0); !errors.Is(err, ErrAutomationQuota) {
		t.Fatalf("actual publish did not consume the monthly count: %v", err)
	}
}

func TestRetryReopensCompletedBatchWithoutChangingItsAuthorization(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	run := AutomationRun{ID: "settled-batch", Month: "2026-10", Status: "completed", Selected: 2}
	if err := r.CreateRun(&run); err != nil {
		t.Fatal(err)
	}
	task := automationTestTask("predownload-retry")
	task.RunID, task.BatchCheck, task.State = run.ID, true, AutomationStateWaitingTarget
	if err := r.Reserve(&task, 10, 1); err != nil {
		t.Fatal(err)
	}
	if err := r.Retry(task.ID, now); err != nil {
		t.Fatal(err)
	}
	active, err := r.BatchRuns()
	if err != nil || len(active) != 1 || active[0].ID != run.ID || active[0].Selected != 2 || active[0].Execute || active[0].Explicit {
		t.Fatalf("manual retry lost the task's batch or expanded run authorization: %+v, %v", active, err)
	}
}

func enqueueAutomationQuotaFixture(t *testing.T, r *AutomationRepository, task AutomationTask, now time.Time) PublishQueueTask {
	t.Helper()
	if err := r.Reserve(&task, 1, 0); err != nil {
		t.Fatal(err)
	}
	claimed, err := r.Claim(task.ID, "lease-"+task.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	queue := PublishQueueTask{TaskID: "queue-" + task.ID, Status: PublishQueueStatusRunning, Trigger: "automation"}
	if err = r.Enqueue(claimed, &queue, now); err != nil {
		t.Fatal(err)
	}
	return queue
}
