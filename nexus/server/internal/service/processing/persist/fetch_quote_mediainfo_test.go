package persist

import (
	"html"
	"os"
	"strings"
	"testing"

	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
)

func TestFetchQuoteMediaInfo(t *testing.T) {
	sample, err := os.ReadFile("../../acquire/extract/testdata/mediainfo-quote-without-general.bbcode")
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.TrimSpace(strings.ReplaceAll(string(sample), "\r\n", "\n"))
	body := strings.TrimSuffix(strings.TrimPrefix(raw, "[quote]"), "[/quote]")
	expected := "General\n" + strings.TrimSpace(strings.ReplaceAll(body, "\\\n", "\n"))
	makeQuote := func(content string) string {
		return "<fieldset><legend>Quote</legend>" + strings.ReplaceAll(html.EscapeString(content), "\n", "<br>") + "</fieldset>"
	}
	dedicated := strings.ReplaceAll(expected, "Matroska", "MPEG-4")
	cases := []struct {
		name, description, expected string
		fromQuote                   bool
	}{
		{name: "HTML quote", description: makeQuote(body), expected: expected, fromQuote: true},
		{name: "pre nested in quote", description: "<fieldset><pre>" + html.EscapeString(body) + "</pre></fieldset>", expected: expected, fromQuote: true},
		{name: "ambiguous pre quotes", description: "<fieldset><pre>" + html.EscapeString(body) + "</pre></fieldset>" + makeQuote(strings.ReplaceAll(body, "E-AC-3", "AAC"))},
		{name: "dedicated report first", description: makeQuote(body) + "<pre>" + html.EscapeString(dedicated) + "</pre>", expected: dedicated},
		{name: "ambiguous quote", description: makeQuote(body) + makeQuote(strings.ReplaceAll(body, "E-AC-3", "AAC"))},
		{name: "incomplete quote", description: makeQuote("Format : Matroska")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := `<html><body><div id="kdescr">` + tc.description + "</div></body></html>"
			review := parser.ExtractReviewData(nil, parser.Input{PageHTML: page, FallbackTitle: "Fixture"}).ReviewData
			// The restriction keeps all network repair hooks out of this test.
			review.Tags = []string{"tag.禁转"}
			draft := NewSeedDraft(strings.Repeat("a", 40), "1", "zmpt", "fixture")
			fromQuote := false
			result, err := RunFetchRepairAndFinalize(FetchRepairFinalizeInput{
				TaskID: "sample", ReviewData: review, DetailHTML: page, Draft: draft, SiteIdentifier: "zmpt",
			}, FetchRepairFinalizeDeps{FetchRepairDeps: processingrepair.FetchRepairDeps{
				EmitLog: func(taskID, step, message, status string) {
					if taskID == "sample" && message == "MediaInfo 从简介提取" && status == "success" {
						fromQuote = true
					}
				},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if !result.RestrictionPrecheck.Matched {
				t.Fatal("fixture restriction should disable network repairs")
			}
			if draft.Mediainfo != tc.expected || result.FinalizeResult.MediainfoValid != (tc.expected != "") || fromQuote != tc.fromQuote {
				t.Fatalf("media mismatch: got %q, valid %t, from quote %t", draft.Mediainfo, result.FinalizeResult.MediainfoValid, fromQuote)
			}
		})
	}
}
