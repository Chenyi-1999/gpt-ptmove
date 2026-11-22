package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractQuotedMediaInfoFromProvidedDescription(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "mediainfo-quote-without-general.bbcode"))
	if err != nil {
		t.Fatal(err)
	}

	got := ExtractQuotedMediaInfo("电影简介\n" + string(data) + "\n其他说明")
	if got == "" {
		t.Fatal("expected the complete MediaInfo quote to be extracted")
	}
	if !strings.HasPrefix(got, "General\nFormat") {
		t.Fatalf("expected a synthesized General heading, got prefix %q", got[:min(len(got), 50)])
	}
	if !strings.Contains(got, "Bits/(Pixel\\*Frame)                       : 0.050") {
		t.Fatal("expected the interior backslash before an asterisk to be preserved")
	}
	if strings.Contains(got, "GiB\\\n") || strings.Contains(got, "GiB\\\r\n") {
		t.Fatal("expected terminal continuation backslashes to be removed")
	}
}

func TestExtractQuotedMediaInfoKeepsExistingGeneralReport(t *testing.T) {
	input := "[QUOTE=source]\r\nGeneral\r\nFormat : Matroska\\\r\nFile size : 4 GiB\\\r\nVideo #1\r\nFormat : HEVC\\\r\nCodec ID : V_MPEGH/ISO/HEVC\\\r\nWidth : 3840 pixels\\\r\nHeight : 1440 pixels\\\r\nAudio #1\r\nFormat : E-AC-3\\\r\nChannel(s) : 2 channels\\\r\n[/quote]"
	got := ExtractQuotedMediaInfo(input)
	want := "General\nFormat : Matroska\nFile size : 4 GiB\nVideo #1\nFormat : HEVC\nCodec ID : V_MPEGH/ISO/HEVC\nWidth : 3840 pixels\nHeight : 1440 pixels\nAudio #1\nFormat : E-AC-3\nChannel(s) : 2 channels"
	if got != want {
		t.Fatalf("unexpected normalized report\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestExtractQuotedMediaInfoRejectsIncompleteAndAmbiguousQuotes(t *testing.T) {
	valid := "[quote]Format : Matroska\nFile size : 4 GiB\nVideo\nFormat : HEVC\nWidth : 3840 pixels\nHeight : 1440 pixels\nAudio\nFormat : E-AC-3\nChannel(s) : 2 channels[/quote]"
	incomplete := "[quote]Format : Matroska\nFile size : 4 GiB\nVideo\nFormat : HEVC\nWidth : 3840 pixels\nAudio\nFormat : E-AC-3\nChannel(s) : 2 channels[/quote]"
	other := strings.ReplaceAll(valid, "Matroska", "WebM")

	for _, input := range []string{
		"[quote]普通的简介引用[/quote]",
		incomplete,
		"[quote]" + valid + "[/quote]",
		valid + "\n" + other,
		"[quote]Format : Matroska\n[quote]nested[/quote]\nVideo\nFormat : HEVC[/quote]",
	} {
		if got := ExtractQuotedMediaInfo(input); got != "" {
			t.Errorf("expected no report for invalid or ambiguous input, got %q", got)
		}
	}
	if got := ExtractQuotedMediaInfo(valid); got == "" {
		t.Fatal("expected a complete report without a General heading")
	}
}
