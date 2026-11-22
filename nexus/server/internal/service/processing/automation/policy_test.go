package automation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDefaultsDisabledAndShanghaiMonthlySchedule(t *testing.T) {
	s := Defaults()
	if s.Enabled || s.AutoPublish || s.ScheduleEnabled {
		t.Fatal("new module enabled")
	}
	now := time.Date(2026, 11, 1, 1, 59, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	s.Enabled = true
	s.ScheduleEnabled = true
	if Due(now, s) || !Due(now.Add(time.Minute), s) {
		t.Fatal("Shanghai month day 1 02:00 schedule wrong")
	}
	if Month(time.Date(2026, 10, 31, 16, 1, 0, 0, time.UTC)) != "2026-11" {
		t.Fatal("month used server timezone")
	}
}
func TestPolicyUnknownChecksCannotBecomeEligible(t *testing.T) {
	s := Defaults()
	base := Candidate{ID: "1", Size: GiB, InfoHash: "0123456789012345678901234567890123456789", Torrent: []byte("metadata"), Seeders: 3, Allowed: true, Official: true}
	for _, c := range []Candidate{base, {ID: base.ID, Size: base.Size, InfoHash: base.InfoHash, Torrent: base.Torrent, Seeders: 3, RestrictionsKnown: true, Allowed: true, Official: true}} {
		if Evaluate(c, s).Eligible {
			t.Fatal("unknown permission/duplicate accepted")
		}
	}
	base.RestrictionsKnown = true
	base.DuplicateKnown = true
	if !Evaluate(base, s).Eligible {
		t.Fatal("affirmative eligible candidate rejected")
	}
	s.MinLeechers = 1
	base.Leechers = -1
	if Evaluate(base, s).Eligible {
		t.Fatal("unknown leecher count accepted")
	}
}

func TestOfficialEpisodeDownloadedAndTargetAbsenceAreMandatory(t *testing.T) {
	settings := Defaults()
	settings.PreferOfficial, settings.TargetAbsentPriority = false, false
	base := Candidate{
		ID: "40", Official: true, RestrictionsKnown: true, Allowed: true, DuplicateKnown: true,
		Size: GiB, InfoHash: strings.Repeat("a", 40), Torrent: []byte("metadata"), Seeders: 3,
	}
	if !Evaluate(base, settings).Eligible {
		t.Fatal("fully verified official release rejected")
	}
	for _, test := range []struct {
		name string
		set  func(*Candidate)
	}{
		{"未确认源站官种", func(c *Candidate) { c.Official = false }},
		{"源站禁止或限制转载", func(c *Candidate) { c.Allowed = false }},
		{"源站标记为分集", func(c *Candidate) { c.Episode = true }},
		{"源种子已下载或正在下载", func(c *Candidate) { c.PreviouslyDownloaded = true }},
		{"目标已有种子", func(c *Candidate) { c.Duplicate = true }},
		{"目标查重未知", func(c *Candidate) { c.DuplicateKnown = false }},
	} {
		candidate := base
		test.set(&candidate)
		checked := Evaluate(candidate, settings)
		if checked.Eligible || len(checked.Reasons) != 1 || checked.Reasons[0] != test.name {
			t.Fatalf("missing mandatory exclusion %q: %+v", test.name, checked)
		}
	}
}

func TestRankEligibleThenSizeThenPeers(t *testing.T) {
	rows := []Candidate{
		{ID: "larger", Eligible: true, Size: 2 * GiB, Seeders: 90},
		{ID: "small-rejected", Size: GiB / 2, Seeders: 90},
		{ID: "small", Eligible: true, Size: GiB, Seeders: 3},
		{ID: "small-popular", Eligible: true, Size: GiB, Seeders: 4},
	}
	Rank(rows, Defaults())
	for i, expected := range []string{"small-popular", "small", "larger", "small-rejected"} {
		if rows[i].ID != expected {
			t.Fatalf("rank at %d = %q, want %q", i, rows[i].ID, expected)
		}
	}
}

func TestLegacyPreferenceSettingsRemainAccepted(t *testing.T) {
	old, err := DecodeSettings(`{"prefer_official":false,"target_absent_priority":false}`)
	if err != nil || old.PreferOfficial || old.TargetAbsentPriority {
		t.Fatalf("legacy settings failed to decode: %+v, %v", old, err)
	}
	if _, err := MergeSettings(old, map[string]any{"prefer_official": true, "target_absent_priority": false}); err != nil {
		t.Fatalf("legacy settings patch failed to decode: %v", err)
	}
}
func TestSettingsValidateSizeBoundsAndBatchOptions(t *testing.T) {
	s := Defaults()
	for _, patch := range []map[string]any{
		{"password": "secret"}, {"monthly_limit": 0}, {"max_size_gib": 0.5}, {"reserve_space_gib": -1},
		{"schedule_time": "not a time"}, {"batch_size": 0}, {"batch_size": 101},
		{"duplicate_action": "unknown"}, {"duplicate_action": "move", "duplicate_directory": "relative"},
	} {
		if _, err := MergeSettings(s, patch); err == nil {
			t.Fatalf("invalid settings accepted %+v", patch)
		}
	}
	old, err := DecodeSettings(`{"max_size_gib":100,"monthly_budget_gib":1}`)
	if err != nil || old.BatchSize != 20 || old.DuplicateAction != "keep" {
		t.Fatalf("legacy byte cap blocked an otherwise valid setting: %+v, %v", old, err)
	}
	next, err := MergeSettings(s, map[string]any{
		"batch_size": 30, "duplicate_action": "move", "duplicate_directory": "/qb/duplicates",
		"monthly_budget_gib": 1,
	})
	if err != nil || next.BatchSize != 30 {
		t.Fatalf("valid batch options rejected: %v", err)
	}
	raw, err := json.Marshal(next)
	if err != nil || strings.Contains(string(raw), "monthly_budget_gib") {
		t.Fatalf("obsolete monthly size limit exposed by settings JSON: %s, %v", raw, err)
	}
}
