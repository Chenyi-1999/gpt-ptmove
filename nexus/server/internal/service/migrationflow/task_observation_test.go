package migrationflow

import (
	"testing"
	"time"
)

func TestTaskObservationKeepsConcurrentWorkAndTerminalEvidence(t *testing.T) {
	s := NewMigrateService(nil, nil)
	preview := s.observeTask("candidate_preview", "获取预览候选", automationPluginRoute)
	media := s.observeTask("screenshot", "生成截图", "")
	if preview.id == media.id {
		t.Fatal("concurrent operations share an identifier")
	}
	recordScreenshotProgress(media, "remote_capture", -1, 5)
	if got := s.observations.rows[media.id].progress; got != "计划 5 张，等待客户端回传" {
		t.Fatalf("opaque remote progress was displayed as a measured count: %s", got)
	}
	recordScreenshotProgress(media, "capturing", 2, 5)
	if got := s.observations.rows[media.id].progress; got != "已截取 2/5 张" {
		t.Fatalf("measured frame count unavailable: %s", got)
	}
	preview.finish(true, "预览完成", "共 2 条")
	preview.update("迟到更新", "")
	if len(s.observations.snapshot()) != 2 || s.observations.rows[preview.id].status != "success" || s.observations.rows[media.id].status != "running" {
		t.Fatal("completion overwrote another task or was overwritten by a late update")
	}
	for _, row := range s.observations.snapshot() {
		if row["kind"] == "candidate_preview" && row["route_target"].(map[string]string)["path"] != "/plugins?plugin=automation" {
			t.Fatal("candidate preview must open the automatic transfer plugin")
		}
	}
	s.observations.prune(time.Now().Add(observationRetention + time.Second))
	if len(s.observations.rows) != 1 || s.observations.rows[media.id].status != "running" {
		t.Fatal("terminal retention expired an active task")
	}
}
