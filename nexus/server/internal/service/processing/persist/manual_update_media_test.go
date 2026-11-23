package persist

import "testing"

func TestPreserveUntouchedMaintenanceMedia(t *testing.T) {
	base := map[string]any{"screenshots": "", "screenshot_review_status": "none", "mediainfo": ""}
	existing := map[string]any{"screenshots": "batch screenshots", "screenshot_review_status": "pending", "mediainfo": "batch report"}
	updated := map[string]any{"screenshots": "", "screenshot_review_status": "none", "mediainfo": "", "title": "new title"}
	preserveUntouchedMaintenanceMedia(updated, base)
	result := BuildManualUpdatedSeedRecord(BuildManualUpdateInput{Existing: existing, Updated: updated})
	for field, value := range existing {
		if result.Record[field] != value {
			t.Fatalf("%s overwritten by unchanged draft: %v", field, result.Record[field])
		}
	}
	updated["screenshots"] = "manual selection"
	updated["screenshot_review_status"] = "confirmed"
	preserveUntouchedMaintenanceMedia(updated, base)
	result = BuildManualUpdatedSeedRecord(BuildManualUpdateInput{Existing: existing, Updated: updated})
	if result.Record["screenshots"] != "manual selection" || result.Record["screenshot_review_status"] != "confirmed" ||
		result.Record["mediainfo"] != "batch report" {
		t.Fatalf("manual reselection was not applied independently: %#v", result.Record)
	}
}
