package downloaderclient

import (
	"slices"
	"testing"
)

func TestParseTorrentTags(t *testing.T) {
	tests := []struct {
		name string
		raw  any
		want []string
	}{
		{
			name: "qB comma separated tags",
			raw:  " movie, seeding, movie, , favorite ",
			want: []string{"movie", "seeding", "favorite"},
		},
		{
			name: "Transmission label list",
			raw:  []any{"movie", " seeding ", "movie", 42},
			want: []string{"movie", "seeding", "42"},
		},
		{
			name: "missing labels",
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseTorrentTags(test.raw); !slices.Equal(got, test.want) {
				t.Fatalf("parseTorrentTags(%#v) = %#v, want %#v", test.raw, got, test.want)
			}
		})
	}
}

func TestTorrentGroupKeepsCategorySeparateFromTags(t *testing.T) {
	tags := []string{"movie", "favorite"}
	if got := torrentGroup("Films", tags); got != "Films" {
		t.Fatalf("torrentGroup(category, tags) = %q, want category %q", got, "Films")
	}
	if got := torrentGroup("", tags); got != "movie" {
		t.Fatalf("torrentGroup(empty category, tags) = %q, want first tag %q", got, "movie")
	}
}

func TestNormalizeInventoryState(t *testing.T) {
	tests := map[string]string{
		"error":              "error",
		"missingFiles":       "error",
		"checkingResumeData": "checking",
		"check_wait":         "checking",
		"queuedDL":           "queued",
		"queuedUP":           "queued",
		"downloading":        "downloading",
		"forcedDL":           "downloading",
		"metaDL":             "downloading",
		"uploading":          "seeding",
		"stalledUP":          "seeding",
		"forcedUP":           "seeding",
		"pausedDL":           "paused",
		"stalledDL":          "downloading",
		"stopped":            "paused",
		"status_8":           "other",
		"":                   "other",
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := normalizeInventoryState(input); got != want {
				t.Fatalf("normalizeInventoryState(%q) = %q, want %q", input, got, want)
			}
		})
	}
}
