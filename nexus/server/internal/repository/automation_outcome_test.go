package repository

import (
	"errors"
	"testing"
	"time"
)

// claimAutomationPublishQueue 创建尚未提交的运行中队列，保留下载预算。
func claimAutomationPublishQueue(t *testing.T, r *AutomationRepository, now time.Time) (AutomationTask, PublishQueueTask) {
	t.Helper()
	if err := r.SaveSettings(`{"enabled":true,"auto_publish":true}`, false, now); err != nil {
		t.Fatal(err)
	}
	task := automationTestTask("outcome")
	task.State, task.DownloadAttempted = "ready", true
	if err := r.Reserve(&task, 6, 60); err != nil {
		t.Fatal(err)
	}
	claimed, err := r.Claim(task.ID, "media-owner", now)
	if err != nil {
		t.Fatal(err)
	}
	queue := PublishQueueTask{Trigger: "automation", Status: PublishQueueStatusQueued}
	if err = r.Enqueue(claimed, &queue, now); err != nil {
		t.Fatal(err)
	}
	native := NewPublishQueueRepository(&Store{DB: r.db})
	claimedQueue, ok, err := native.ClaimNextRunnableTaskFiltered(now, false, true)
	if err != nil || !ok || claimedQueue == nil || claimedQueue.ID != queue.ID {
		t.Fatalf("claim automated queue: %+v, found %v, err %v", claimedQueue, ok, err)
	}
	return task, *claimedQueue
}

// TestAutomationExistingRejectsRepeatedPublish 验证两种重复回执均为终态且保留已下载的预算。
func TestAutomationExistingRejectsRepeatedPublish(t *testing.T) {
	for _, variant := range []struct {
		name      string
		attempted bool
	}{
		{name: "confirmed before submit"},
		{name: "confirmed by upload response", attempted: true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			r := automationTestRepo(t)
			now := time.Now()
			task, queue := claimAutomationPublishQueue(t, r, now)
			if variant.attempted {
				if err := r.BeginPublish(queue.ID, now); err != nil {
					t.Fatal(err)
				}
				if err := r.PublishOutcome(queue.ID, AutomationStateAlreadyExists, "", `{"is_existing_torrent":true}`, now); err != nil {
					t.Fatal(err)
				}
			} else if err := r.ResolveTargetBeforePublish(queue.ID, AutomationStateAlreadyExists, now); err != nil {
				t.Fatal(err)
			}

			stored, err := r.Task(task.ID)
			if err != nil || stored.State != AutomationStateAlreadyExists || stored.PublishAttempted != variant.attempted {
				t.Fatalf("unexpected automation result: %+v, %v", stored, err)
			}
			native := NewPublishQueueRepository(&Store{DB: r.db})
			result, ok, err := native.FindTaskByID(queue.ID)
			if err != nil || !ok || result.Status != PublishQueueStatusExists || result.NextRunAt != nil {
				t.Fatalf("duplicate counted or requeued: %+v, found %v, err %v", result, ok, err)
			}
			if q, err := r.Quota(task.Month); err != nil || q.Count != 1 || q.Bytes != task.Size {
				t.Fatalf("used reservation changed: %+v, %v", q, err)
			}
			if err = r.Retry(task.ID, now); !errors.Is(err, ErrAutomationFence) {
				t.Fatalf("duplicate accepted retry: %v", err)
			}
			if err = r.RequestReconcile(task.ID, "https://target.example/details.php?id=2", now); !errors.Is(err, ErrAutomationFence) {
				t.Fatalf("duplicate accepted reconciliation: %v", err)
			}
			if err = r.PublishOutcome(queue.ID, "published", "", "", now); !errors.Is(err, ErrAutomationFence) {
				t.Fatalf("late successful response overwrote existing: %v", err)
			}
		})
	}
}

// TestAutomationExistingBeforeDownloadRefundsUntouchedQuota 验证下载前确认已有时只退还未使用的预留。
func TestAutomationExistingBeforeDownloadRefundsUntouchedQuota(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Now()
	task := automationTestTask("unused")
	if err := r.Reserve(&task, 1, task.Size); err != nil {
		t.Fatal(err)
	}
	claimed, err := r.Claim(task.ID, "lease", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.SkipExistingBeforeDownload(task, now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("unleased skip accepted: %v", err)
	}
	if err = r.SkipExistingBeforeDownload(claimed, now); err != nil {
		t.Fatal(err)
	}
	if q, err := r.Quota(task.Month); err != nil || q.Count != 0 || q.Bytes != 0 {
		t.Fatalf("unused reservation not restored: %+v, %v", q, err)
	}
	if err = r.Cancel(task.ID, now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("terminal result could be cancelled: %v", err)
	}
	if found, err := r.HasDownloadHistory(task.Source, task.SourceID, task.InfoHash, ""); err != nil || found {
		t.Fatalf("unused reservation became download history: %v, %v", found, err)
	}
}

// TestAutomationWaitingTargetRestoresTheSameQueue 验证结果未知的预检可重查，且不会提前标记上传。
func TestAutomationWaitingTargetRestoresTheSameQueue(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Now()
	task, queue := claimAutomationPublishQueue(t, r, now)
	if err := r.ResolveTargetBeforePublish(queue.ID, AutomationStateWaitingTarget, now); err != nil {
		t.Fatal(err)
	}
	if err := r.BeginPublish(queue.ID, now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("unknown target accepted upload: %v", err)
	}
	if rows, err := r.WorkTasks(); err != nil || len(rows) != 0 {
		t.Fatalf("unknown target can run before recheck: %v %v", rows, err)
	}
	if err := r.Retry(task.ID, now); err != nil {
		t.Fatal(err)
	}
	stored, err := r.Task(task.ID)
	if err != nil || stored.State != "queued_publish" || stored.QueueID == nil || *stored.QueueID != queue.ID || stored.PublishAttempted {
		t.Fatalf("retry did not restore original queue: %+v %v", stored, err)
	}
	if q, err := r.Quota(task.Month); err != nil || q.Count != 1 || q.Bytes != task.Size {
		t.Fatalf("retry changed reservation: %+v, %v", q, err)
	}
}

// TestAutomationCancelFencesRunningTargetCheck 模拟查重进行中取消，以及上传授权先行的两种交错顺序。
func TestAutomationCancelFencesRunningTargetCheck(t *testing.T) {
	t.Run("cancel during target check", func(t *testing.T) {
		r := automationTestRepo(t)
		now := time.Now()
		task, queue := claimAutomationPublishQueue(t, r, now)
		if err := r.Cancel(task.ID, now); err != nil {
			t.Fatal(err)
		}
		if err := r.ResolveTargetBeforePublish(queue.ID, AutomationStateAlreadyExists, now); !errors.Is(err, ErrAutomationFence) {
			t.Fatalf("late target check was not fenced: %v", err)
		}
		if err := r.BeginPublish(queue.ID, now); !errors.Is(err, ErrAutomationFence) {
			t.Fatalf("cancelled task could authorize an upload: %v", err)
		}
		next := now.Add(time.Minute)
		native := NewPublishQueueRepository(&Store{DB: r.db})
		if err := native.UpdateTaskAfterFailure(queue.ID, 1, &next, "late failure", ""); err != nil {
			t.Fatal(err)
		}
		if err := native.UpdateTaskAfterRequeue(queue.ID, next, "late requeue", ""); err != nil {
			t.Fatal(err)
		}
		result, ok, err := native.FindTaskByID(queue.ID)
		if err != nil || !ok || result.Status != PublishQueueStatusCancelled || result.FinishedAt == nil || result.NextRunAt != nil {
			t.Fatalf("native queue was not closed or changed after cancellation: %+v, %v", result, err)
		}
		stored, err := r.Task(task.ID)
		if err != nil || stored.State != "cancelled" || stored.PublishAttempted {
			t.Fatalf("unexpected task after cancellation: %+v, %v", stored, err)
		}
	})

	t.Run("upload authorized before cancel", func(t *testing.T) {
		r := automationTestRepo(t)
		now := time.Now()
		task, queue := claimAutomationPublishQueue(t, r, now)
		if err := r.BeginPublish(queue.ID, now); err != nil {
			t.Fatal(err)
		}
		if err := r.Cancel(task.ID, now); err == nil {
			t.Fatal("cancel succeeded after upload was marked")
		}
		stored, err := r.Task(task.ID)
		if err != nil || stored.State != "publishing" || !stored.PublishAttempted {
			t.Fatalf("cancel changed the upload attempt: %+v, %v", stored, err)
		}
		native := NewPublishQueueRepository(&Store{DB: r.db})
		result, ok, err := native.FindTaskByID(queue.ID)
		if err != nil || !ok || result.Status != PublishQueueStatusRunning {
			t.Fatalf("cancel changed the native upload queue: %+v, %v", result, err)
		}
	})
}

// TestAutomationWaitingMediaClaimHonorsBackoff 验证轮询写入不会清空媒体重试时间。
func TestAutomationWaitingMediaClaimHonorsBackoff(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Now()
	task := automationTestTask("media-backoff")
	task.State, task.DownloadAttempted = AutomationStateWaitingMedia, true
	task.MediaRetryAt = now.Add(2 * time.Minute).UnixMilli()
	if err := r.Reserve(&task, 6, 60); err != nil {
		t.Fatal(err)
	}
	if err := r.TouchPoll(task.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Claim(task.ID, "early", now.Add(time.Minute)); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("waiting task claimed before media retry time: %v", err)
	}
	claimed, err := r.Claim(task.ID, "due", now.Add(2*time.Minute))
	if err != nil || claimed.MediaRetryAt != task.MediaRetryAt {
		t.Fatalf("waiting task was not claimable when due: %+v, %v", claimed, err)
	}
}

// TestAutomationRecoverExistingLegacyResult 验证旧版的明确重复不会被对账为发布成功。
func TestAutomationRecoverExistingLegacyResult(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Now()
	task, queue := claimAutomationPublishQueue(t, r, now)
	if err := r.BeginPublish(queue.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := r.db.Model(&AutomationTask{}).Where("id = ?", task.ID).Updates(map[string]any{
		"state": "needs_review", "result_json": `{"success":true,"is_existing_torrent":true}`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.RecoverPublishing(now); err != nil {
		t.Fatal(err)
	}
	stored, err := r.Task(task.ID)
	if err != nil || stored.State != AutomationStateAlreadyExists || !stored.PublishAttempted {
		t.Fatalf("duplicate outcome discarded during recovery: %+v, %v", stored, err)
	}
	native := NewPublishQueueRepository(&Store{DB: r.db})
	result, ok, err := native.FindTaskByID(queue.ID)
	if err != nil || !ok || result.Status != PublishQueueStatusExists {
		t.Fatalf("legacy queue still records duplicate as another outcome: %+v, %v", result, err)
	}
	if err = r.RequestReconcile(task.ID, "https://target.example/details.php?id=2", now); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("recovered duplicate accepted reconciliation: %v", err)
	}
}

// TestAutomationRecoverReconciledLegacyDuplicate 核对覆盖任务结果后，仍以原始自动队列的结构化重复回执为准。
func TestAutomationRecoverReconciledLegacyDuplicate(t *testing.T) {
	for _, test := range []struct {
		name, trigger, queueResult, wantState string
	}{
		{"original existing receipt", "automation", `{"success":true,"is_existing_torrent":true}`, AutomationStateAlreadyExists},
		{"original successful upload", "automation", `{"success":true,"is_existing_torrent":false}`, "succeeded"},
		{"manual queue is not evidence", "manual", `{"is_existing_torrent":true}`, "succeeded"},
		{"quoted flag is not evidence", "automation", `{"is_existing_torrent":"true"}`, "succeeded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := automationTestRepo(t)
			now := time.Now()
			task, queue := claimAutomationPublishQueue(t, r, now)
			if err := r.BeginPublish(queue.ID, now); err != nil {
				t.Fatal(err)
			}
			if err := r.db.Model(&PublishQueueTask{}).Where("id = ?", queue.ID).Updates(map[string]any{
				"publish_trigger": test.trigger, "status": PublishQueueStatusFailed, "last_result": test.queueResult,
			}).Error; err != nil {
				t.Fatal(err)
			}
			if err := r.db.Model(&AutomationTask{}).Where("id = ?", task.ID).Updates(map[string]any{
				"state": "succeeded", "result_json": `{"same_payload":true,"target_tracker_verified":true}`,
			}).Error; err != nil {
				t.Fatal(err)
			}
			if err := r.RecoverPublishing(now); err != nil {
				t.Fatal(err)
			}
			stored, err := r.Task(task.ID)
			if err != nil || stored.State != test.wantState || !stored.PublishAttempted {
				t.Fatalf("recovered task: %+v, %v", stored, err)
			}
			wantQueueState := PublishQueueStatusFailed
			if test.wantState == AutomationStateAlreadyExists {
				wantQueueState = PublishQueueStatusExists
			}
			native := NewPublishQueueRepository(&Store{DB: r.db})
			result, ok, err := native.FindTaskByID(queue.ID)
			if err != nil || !ok || result.Status != wantQueueState || result.LastResult != test.queueResult {
				t.Fatalf("recovered queue or original receipt changed unexpectedly: %+v, %v", result, err)
			}
			if quota, err := r.Quota(task.Month); err != nil || quota.Count != 1 || quota.Bytes != task.Size {
				t.Fatalf("recovered quota: %+v, %v", quota, err)
			}
		})
	}
}
