package migrationflow

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pt-nexus/server/internal/repository"
	policy "github.com/pt-nexus/server/internal/service/processing/automation"
)

// TestAutomationTargetCheckStateRequiresUnchangedSource 保证查重状态不会掩盖源站或载荷错误。
func TestAutomationTargetCheckStateRequiresUnchangedSource(t *testing.T) {
	settings := policy.Defaults()
	task := repository.AutomationTask{InfoHash: strings.Repeat("a", 40), Size: 6 * policy.GiB}
	candidate := policy.Candidate{
		Official: true, RestrictionsKnown: true, Allowed: true, Size: task.Size, InfoHash: task.InfoHash,
		Seeders: 12, Torrent: []byte{1},
	}
	if got := automationTargetCheckState(candidate, task, settings); got != repository.AutomationStateWaitingTarget {
		t.Fatalf("unknown target got state %q", got)
	}
	candidate.DuplicateKnown, candidate.Duplicate = true, true
	if got := automationTargetCheckState(candidate, task, settings); got != repository.AutomationStateAlreadyExists {
		t.Fatalf("confirmed duplicate got state %q", got)
	}
	candidate.Allowed = false
	if got := automationTargetCheckState(candidate, task, settings); got != "" {
		t.Fatalf("source restriction masked by state %q", got)
	}
	candidate.Allowed = true
	candidate.Size++
	if got := automationTargetCheckState(candidate, task, settings); got != "" {
		t.Fatalf("changed payload masked by state %q", got)
	}
}

// TestAutomationStructuredExistingResponseIsNotPublishSuccess 验证结构化重复回执不计成功。
func TestAutomationStructuredExistingResponseIsNotPublishSuccess(t *testing.T) {
	service, db := automationFixture(t)
	if err := db.AutoMigrate(&repository.PublishLogEntry{}); err != nil {
		t.Fatal(err)
	}
	target, err := service.profiles.Resolve("novahd")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(automationSnapshot{Target: *target})
	if err != nil {
		t.Fatal(err)
	}
	queue := repository.PublishQueueTask{Trigger: "automation", Status: repository.PublishQueueStatusRunning}
	if err = db.Create(&queue).Error; err != nil {
		t.Fatal(err)
	}
	task := repository.AutomationTask{
		ID: "existing-response", State: "publishing", PublishAttempted: true, QueueID: &queue.ID, Snapshot: string(raw),
	}
	if err = db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	logs := repository.NewPublishLogRepository(&repository.Store{DB: db})
	migrate := &MigrateService{automation: service, publishLogRepo: logs}
	url := target.BaseURL + "/details.php?id=22"
	safe, err := migrate.automationPublishOutcome(queue, map[string]any{
		"success": true, "is_existing_torrent": true, "url": url, "logs": "untrusted response",
	}, http.StatusConflict)
	if err != nil || safe["success"] != false || safe["is_existing_torrent"] != true || safe["url"] != url {
		t.Fatalf("duplicate response not normalized: %v, %v", safe, err)
	}
	migrate.appendPublishLog(map[string]any{
		"publish_trigger": "automation", "targetSite": "NovaHD", "queue_task_id": queue.ID,
	}, task.ID, "220", safe, http.StatusConflict, 0)
	log, found, err := logs.FindLatestByQueueTaskID(queue.ID)
	if err != nil || !found || log.Status != "exists" {
		t.Fatalf("duplicate response did not reach the distinct publish log: %+v, %v", log, err)
	}
	stored, err := service.repo.Task(task.ID)
	if err != nil || stored.State != repository.AutomationStateAlreadyExists {
		t.Fatalf("automatic task did not record duplicate: %+v, %v", stored, err)
	}
}
