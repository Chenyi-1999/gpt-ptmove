package repository

import (
	"errors"
	"testing"
	"time"
)

func TestAutomationPluginStopsLeasesAndPreservesPreparedWork(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Now()
	const rules = `{"enabled":true,"auto_publish":true}`
	if err := r.SaveSettings(rules, false, now); err != nil {
		t.Fatal(err)
	}
	run := AutomationRun{ID: "explicit-run", Status: "queued", Execute: true, Explicit: true}
	if err := r.CreateRun(&run); err != nil {
		t.Fatal(err)
	}
	claimedRun, err := r.ClaimRun(run.ID, "old-run", now)
	if err != nil {
		t.Fatal(err)
	}
	task := automationTestTask("prepared")
	task.State, task.Explicit = "ready", true
	if err := r.Reserve(&task, 10, 0); err != nil {
		t.Fatal(err)
	}
	claimedTask, err := r.Claim(task.ID, "old-task", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetPluginEnabled(false); err != nil {
		t.Fatal(err)
	}
	newRun := AutomationRun{ID: "blocked", Status: "queued", Execute: true}
	if err := r.CreateRun(&newRun); !errors.Is(err, ErrAutomationPluginDisabled) {
		t.Fatalf("execution intent was accepted: %v", err)
	}
	if err := r.Enqueue(claimedTask, &PublishQueueTask{Trigger: "automation"}, now); !errors.Is(err, ErrAutomationPluginDisabled) {
		t.Fatalf("disabled worker could enqueue: %v", err)
	}
	reservation := automationTestTask("new-reservation")
	if err := r.ReserveForRun(&reservation, 10, 0, run.ID, claimedRun.LeaseToken, now); !errors.Is(err, ErrAutomationPluginDisabled) {
		t.Fatalf("disabled scanner could reserve: %v", err)
	}
	if err := r.PauseRunLeased(claimedRun, 0); err != nil {
		t.Fatal(err)
	}
	if err := r.Release(claimedTask.ID, claimedTask.LeaseToken); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ClaimRun(run.ID, "next", now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("disabled scan was claimed: %v", err)
	}
	if jobs, err := r.WorkTasks(); err != nil || len(jobs) != 0 {
		t.Fatalf("disabled task was listed for work: %v, %v", jobs, err)
	}
	if _, err := r.Claim(task.ID, "next", now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("disabled task was claimed: %v", err)
	}
	if err := r.Retry(task.ID, now); !errors.Is(err, ErrAutomationPluginDisabled) {
		t.Fatalf("disabled retry was accepted: %v", err)
	}
	if got, err := r.Settings(); err != nil || got != rules {
		t.Fatalf("rules were modified: %q, %v", got, err)
	}
	opened, err := NewAutomationRepository(&Store{DB: r.db})
	if err != nil {
		t.Fatal(err)
	}
	if enabled, err := opened.PluginEnabled(); err != nil || enabled {
		t.Fatalf("module restart lost disabled state: %v, %v", enabled, err)
	}
	if err := opened.SetPluginEnabled(true); err != nil {
		t.Fatal(err)
	}
	if _, err := opened.Claim(task.ID, "restored", now); err != nil {
		t.Fatalf("old task did not resume: %v", err)
	}
	if _, err := opened.ClaimRun(run.ID, "restored", now); err != nil {
		t.Fatalf("old run did not resume: %v", err)
	}
}

func TestAutomationPluginStopsPublishBoundaryButKeepsAttemptEvidence(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Now()
	task, queue := claimAutomationPublishQueue(t, r, now)
	if err := r.SetPluginEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err := r.BeginPublish(queue.ID, now); !errors.Is(err, ErrAutomationPluginDisabled) {
		t.Fatalf("disabled plugin advanced publish: %v", err)
	}
	old, err := r.Task(task.ID)
	if err != nil || old.PublishAttempted || old.State != "queued_publish" {
		t.Fatalf("blocked attempt modified task: %+v %v", old, err)
	}
	if err := r.SetPluginEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := r.BeginPublish(queue.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := r.SetPluginEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err := r.PublishOutcome(queue.ID, "published", "https://target.example/details.php?id=1", "{}", now); err != nil {
		t.Fatal(err)
	}
	got, err := r.Claim(task.ID, "verify-existing", now)
	if err != nil || got.State != "published" || !got.PublishAttempted {
		t.Fatalf("published evidence could not be verified while disabled: %+v %v", got, err)
	}
	if err := r.Retry(task.ID, now); !errors.Is(err, ErrAutomationPluginDisabled) {
		t.Fatalf("disabled retry ignored plugin switch: %v", err)
	}
}

func TestAutomationPluginRestoresOnlyTheUnsentPublishAttempt(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Now()
	task, queue := claimAutomationPublishQueue(t, r, now)
	if err := r.BeginPublish(queue.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := r.SetPluginEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err := r.RestoreUnsentAutomationPublish(queue.ID, now.Add(-time.Second), now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("another attempt's fence was accepted: %v", err)
	}
	if err := r.RestoreUnsentAutomationPublish(queue.ID, now, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := r.Task(task.ID)
	if err != nil || got.State != "queued_publish" || got.PublishAttempted || got.PublishMonth != "" {
		t.Fatalf("unsent attempt kept publishing evidence: %+v %v", got, err)
	}
	var slots int64
	if err := automationPublishSlots(r.db, automationShanghaiMonth(now)).Count(&slots).Error; err != nil || slots != 0 {
		t.Fatalf("unsent attempt kept publish quota: %d %v", slots, err)
	}
	native := NewPublishQueueRepository(&Store{DB: r.db})
	after, _, err := native.FindTaskByID(queue.ID)
	if err != nil || after.Status != PublishQueueStatusQueued || after.AttemptCount != 0 || after.StartedAt != nil {
		t.Fatalf("unsent queue did not resume: %+v %v", after, err)
	}
	if err := r.SetPluginEnabled(true); err != nil {
		t.Fatal(err)
	}
	next := now.Add(2 * time.Second)
	claimed, ok, err := native.ClaimNextRunnableTaskFiltered(next, false, true)
	if err != nil || !ok || claimed.ID != queue.ID {
		t.Fatalf("could not reclaim restored queue: %+v %v %v", claimed, ok, err)
	}
	if err := r.BeginPublish(queue.ID, next); err != nil {
		t.Fatalf("re-enabled attempt cannot begin: %v", err)
	}
	if err := r.RestoreUnsentAutomationPublish(queue.ID, now, next); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("old proof reset a later attempt: %v", err)
	}
}
