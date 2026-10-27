package automation

import (
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
	base := Candidate{ID: "1", Size: GiB, InfoHash: "0123456789012345678901234567890123456789", Torrent: []byte("metadata"), Seeders: 3, Allowed: true}
	for _, c := range []Candidate{base, {ID: base.ID, Size: base.Size, InfoHash: base.InfoHash, Torrent: base.Torrent, Seeders: 3, RestrictionsKnown: true, Allowed: true}} {
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
func TestSettingsRejectUnknownAndImpossibleBudget(t *testing.T) {
	s := Defaults()
	for _, patch := range []map[string]any{{"password": "secret"}, {"monthly_limit": 0}, {"max_size_gib": 100}, {"reserve_space_gib": -1}, {"schedule_time": "not a time"}} {
		if _, err := MergeSettings(s, patch); err == nil {
			t.Fatalf("invalid settings accepted %+v", patch)
		}
	}
}
