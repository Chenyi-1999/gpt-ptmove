package migrationflow

import (
	"strings"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/repository"
)

func TestPublishedEditCandidate(t *testing.T) {
	cases := []struct {
		url, status, id string
	}{
		{"https://pt.novahd.top/details.php?id=17&uploaded=1", "success", "17"},
		{"https://pt.novahd.top/details.php?id=17", "exists", ""},
		{"https://pt.novahd.top/details.php?id=17&id=22", "success", ""},
		{"https://pt.novahd.top/details.php?id=17#fragment", "success", ""},
		{"http://pt.novahd.top/details.php?id=17", "success", ""},
		{"https://pt.novahd.top@tracker.test/details.php?id=17", "success", ""},
		{"https://pt.novahd.top:8443/details.php?id=17", "success", ""},
		{"https://pt.novahd.top/upload.php?id=17", "success", ""},
	}
	for _, tt := range cases {
		row := repository.PublishLogEntry{Status: tt.status, ResultURL: tt.url}
		if got := publishedEditCandidate(&row); got != tt.id {
			t.Errorf("candidate %s/%s = %q, want %q", tt.url, tt.status, got, tt.id)
		}
	}
}

func TestPublishedEditConfirmationBindsPreview(t *testing.T) {
	s := &MigrateService{publishedEditKey: [32]byte{9}}
	now := time.Unix(100, 0)
	screenshot, info := "image code", "media report"
	draft := PublishedEditDraft{Revision: "some revision", ScreenshotCode: &screenshot, MediaInfo: &info}
	draft.Confirmation = s.signPublishedEdit(7, "12", draft, 130)
	if !s.validPublishedEditConfirmation(7, "12", draft, now) {
		t.Fatal("untampered preview rejected")
	}
	if s.validPublishedEditConfirmation(8, "12", draft, now) || s.validPublishedEditConfirmation(7, "13", draft, now) ||
		s.validPublishedEditConfirmation(7, "12", draft, time.Unix(130, 0)) {
		t.Fatal("preview reused for a different record, target, or expiry")
	}
	screenshot = "different image code"
	if s.validPublishedEditConfirmation(7, "12", draft, now) {
		t.Fatal("modified draft replayed a confirmation")
	}
}

func TestPublishedMediaRoutingAndReportName(t *testing.T) {
	if !isPublishedEditPullHub("http://ptmove-pull-hub:28380") ||
		isPublishedEditPullHub("https://ptmove-pull-hub.other.example/") ||
		isPublishedEditPullHub("http://localhost@outside.example:28380") {
		t.Fatal("pull-hub restriction violated")
	}
	got := sanitizePublishedMediaInfo("General\nComplete name : /keep/only-local/Film.mkv\nVideo\n")
	if strings.Contains(got, "/keep/") || !strings.Contains(got, "Complete name : Film.mkv\n") {
		t.Fatalf("unredacted filename path: %q", got)
	}
}

func TestPublishedCoverDraftValidatesMode(t *testing.T) {
	cover := "https://pixhost.to/show/10/cover.jpg"
	value := "other field"
	resolved := "https://img3.pixhost.to/images/10/cover.jpg"
	tests := []struct {
		name   string
		draft  PublishedEditDraft
		saving bool
		valid  bool
	}{
		{"media remains supported", PublishedEditDraft{MediaInfo: &value}, false, true},
		{"cover preview", PublishedEditDraft{CoverURL: &cover}, false, true},
		{"reviewed cover save", PublishedEditDraft{CoverURL: &cover, ResolvedCoverURL: resolved}, true, true},
		{"mixed screenshot", PublishedEditDraft{CoverURL: &cover, ScreenshotCode: &value}, false, false},
		{"mixed report", PublishedEditDraft{CoverURL: &cover, MediaInfo: &value}, true, false},
		{"client cannot resolve before preview", PublishedEditDraft{CoverURL: &cover, ResolvedCoverURL: resolved}, false, false},
		{"save requires reviewed URL", PublishedEditDraft{CoverURL: &cover}, true, false},
		{"orphan resolved URL", PublishedEditDraft{ResolvedCoverURL: resolved}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.draft.coverValidationMessage(tt.saving); (got == "") != tt.valid {
				t.Errorf("validation = %q", got)
			}
		})
	}
}

func TestPublishedCoverConfirmationBindsResolvedURL(t *testing.T) {
	s := &MigrateService{publishedEditKey: [32]byte{90}}
	now := time.Unix(100, 0)
	raw := "https://pixhost.to/show/10/cover.jpg"
	draft := PublishedEditDraft{
		Revision: "owner's current revision", CoverURL: &raw,
		ResolvedCoverURL: "https://img3.pixhost.to/images/10/cover.jpg",
	}
	draft.Confirmation = s.signPublishedEdit(6, "226481", draft, 130)
	if !s.validPublishedEditConfirmation(6, "226481", draft, now) {
		t.Fatal("reviewed cover was rejected")
	}
	forged := draft
	forged.ResolvedCoverURL = "https://img7.pixhost.to/images/10/cover.jpg"
	if s.validPublishedEditConfirmation(6, "226481", forged, now) {
		t.Fatal("unreviewed cover URL was accepted")
	}
	raw = "https://pixhost.to/show/11/cover.jpg"
	if s.validPublishedEditConfirmation(6, "226481", draft, now) {
		t.Fatal("updated user link reused an old confirmation")
	}
}
