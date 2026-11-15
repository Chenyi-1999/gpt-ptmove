package repository

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

// TestAutomationPreparedPublishRollback 验证本地准备失败会回滚截图快照且不会产生上传尝试，成功仍只能标记一次。
func TestAutomationPreparedPublishRollback(t *testing.T) {
	r := automationTestRepo(t)
	now := time.Now()
	if err := r.SaveSettings(`{"enabled":true,"auto_publish":true}`, false, now); err != nil {
		t.Fatal(err)
	}
	queue := PublishQueueTask{Trigger: "automation", Status: "running", UploadDataJSON: `{"screenshots":"old"}`}
	if err := r.db.Create(&queue).Error; err != nil {
		t.Fatal(err)
	}
	task := automationTestTask("prepared")
	task.State, task.QueueID = "queued_publish", &queue.ID
	if err := r.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	rejected := errors.New("截图待确认")
	err := r.BeginPublishPrepared(queue.ID, now, func(tx *gorm.DB, current AutomationTask) error {
		if current.ID != task.ID {
			t.Fatal("wrong current task")
		}
		if err := tx.Model(&PublishQueueTask{}).Where("id = ?", queue.ID).Update("upload_data_json", `{"screenshots":"new"}`).Error; err != nil {
			return err
		}
		return rejected
	})
	if !errors.Is(err, rejected) {
		t.Fatalf("expected rejection, got %v", err)
	}
	row, _ := r.Task(task.ID)
	var native PublishQueueTask
	if err := r.db.First(&native, queue.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.PublishAttempted || row.State != "queued_publish" || native.UploadDataJSON != queue.UploadDataJSON {
		t.Fatalf("preparation partially committed: %+v %+v", row, native)
	}
	if err := r.BeginPublish(queue.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := r.BeginPublishPrepared(queue.ID, now, func(*gorm.DB, AutomationTask) error { t.Fatal("duplicate entered preparation"); return nil }); !errors.Is(err, ErrAutomationFence) {
		t.Fatalf("duplicate attempt: %v", err)
	}
}
