package persist

import "testing"

func TestMaintenanceDraftPreservesIdentityAndMissingFields(t *testing.T) {
	previous := map[string]any{
		"site_name": "target", "hash": "target-hash", "torrent_id": "1", "name": "local.mkv",
		"poster": "current poster", "body": "current description", "mediainfo": "current media info",
		"screenshots": "current screenshots", "screenshot_review_status": "confirmed", "bdinfo_task_id": "active",
	}
	updates := maintenanceUpdates(previous, map[string]any{
		"site_name": "source", "hash": "source-hash", "torrent_id": "2", "name": "source name",
		"body": "new description", "mediainfo": "new media info", "screenshots": "current screenshots",
		"screenshot_review_status": "pending", "updated_at": "2026-11-20 00:00:01",
	}, true)
	for _, field := range []string{"site_name", "hash", "torrent_id", "name", "poster", "mediainfo", "screenshots"} {
		if _, exists := updates[field]; exists {
			t.Errorf("unexpected replacement of %s", field)
		}
	}
	if updates["body"] != "new description" || updates["screenshot_review_status"] != "pending" || updates["is_reviewed"] != false {
		t.Fatalf("unexpected updates: %#v", updates)
	}
}
