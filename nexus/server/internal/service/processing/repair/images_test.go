package repair

import (
	"reflect"
	"testing"
)

func TestExtractImageURLsFromTextLinkedPixhostBBCode(t *testing.T) {
	text := `[url=https://pixhost.to/show/1000/ptmove_sample_scene_00h38m49s.png][img]https://img3.pixhost.to/images/1000/ptmove_sample_scene_00h38m49s.png[/img][/url]
[url="https://pixhost.to/show/1000/ptmove_sample_scene_00h39m49s.png"][img=800x600]https://img3.pixhost.to/images/1000/ptmove_sample_scene_00h39m49s.png[/img][/url]
[url=https://pixhost.to/show/1000/ptmove_sample_scene_00h40m49s.png][img]https://img3.pixhost.to/images/1000/ptmove_sample_scene_00h40m49s.png[/img][/url]
[url=https://pixhost.to/show/1000/ptmove_sample_scene_00h41m49s.png][img]https://img3.pixhost.to/images/1000/ptmove_sample_scene_00h41m49s.png[/img][/url]
[url=https://pixhost.to/show/1000/ptmove_sample_scene_00h42m49s.png][img]https://img3.pixhost.to/images/1000/ptmove_sample_scene_00h42m49s.png[/img][/url]`

	got := ExtractImageURLsFromText(text)
	want := []string{
		"https://img3.pixhost.to/images/1000/ptmove_sample_scene_00h38m49s.png",
		"https://img3.pixhost.to/images/1000/ptmove_sample_scene_00h39m49s.png",
		"https://img3.pixhost.to/images/1000/ptmove_sample_scene_00h40m49s.png",
		"https://img3.pixhost.to/images/1000/ptmove_sample_scene_00h41m49s.png",
		"https://img3.pixhost.to/images/1000/ptmove_sample_scene_00h42m49s.png",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want five direct image URLs %#v", got, want)
	}
}

func TestExtractImageURLsFromTextMixedLinkedAndBareImagesWithQuery(t *testing.T) {
	text := `[url=https://pixhost.to/show/1000/sample_a.png?view=1][img]https://img3.pixhost.to/images/1000/sample_a.png?token=end.[/img][/url]
bare https://img3.pixhost.to/images/1000/sample_b.png?width=1280&height=720`
	want := []string{
		"https://img3.pixhost.to/images/1000/sample_a.png?token=end.",
		"https://img3.pixhost.to/images/1000/sample_b.png?width=1280&height=720",
	}
	if got := ExtractImageURLsFromText(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestExtractImageURLsFromTextDropsLegacyPixhostShowWhenDirectImageExists(t *testing.T) {
	text := `[img]https://pixhost.to/show/1000/sample_a.png[/img]
[img]https://img3.pixhost.to/images/1000/sample_a.png?cache=1[/img]`
	want := []string{"https://img3.pixhost.to/images/1000/sample_a.png?cache=1"}
	if got := ExtractImageURLsFromText(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestExtractImageURLsFromTextPreservesIndependentPixhostShowLink(t *testing.T) {
	showURL := "https://pixhost.to/show/1000/sample_a.png"
	if got := ExtractImageURLsFromText("See image: " + showURL); !reflect.DeepEqual(got, []string{showURL}) {
		t.Fatalf("independent show link was not preserved: %#v", got)
	}
	if got := ExtractImageURLsFromText("[url=" + showURL + "]view[/url]"); !reflect.DeepEqual(got, []string{showURL}) {
		t.Fatalf("independent BBCode show link was not preserved: %#v", got)
	}
}

func TestExtractImageURLsFromTextDoesNotInflateScreenshotThreshold(t *testing.T) {
	text := `[url=https://pixhost.to/show/1000/sample_a.png][img]https://img3.pixhost.to/images/1000/sample_a.png?token=end![/img][/url]
[url=https://pixhost.to/show/1000/sample_b.png][img]https://img3.pixhost.to/images/1000/sample_b.png?token=end![/img][/url]`
	got := ExtractImageURLsFromText(text)
	if len(got) != 2 || len(got) >= fetchMinValidScreenshots {
		t.Fatalf("linked pages inflated %d actual screenshots to %d (minimum %d)", 2, len(got), fetchMinValidScreenshots)
	}
}

func TestExtractImageURLsFromTextFragmentsDoNotInflateScreenshotThreshold(t *testing.T) {
	text := `[img]https://img3.pixhost.to/images/1000/sample_a.png#one[/img]
[img]https://img3.pixhost.to/images/1000/sample_a.png#two[/img]
[img]https://img3.pixhost.to/images/1000/sample_b.png[/img]`
	got := ExtractImageURLsFromText(text)
	if len(got) != 2 || len(got) >= fetchMinValidScreenshots {
		t.Fatalf("URL fragments counted as distinct screenshots: %#v (minimum %d)", got, fetchMinValidScreenshots)
	}
}

func TestPixhostCDNAliasesAndCacheQueriesCountOnce(t *testing.T) {
	first := "https://img3.pixhost.to/images/1000/scene_a.png?cache=1"
	second := "https://img2.pixhost.to/images/1000/scene_b.png"
	text := first + " https://img2.pixhost.to/images/1000/scene_a.png?cache=2 " + second
	if got := ExtractImageURLsFromText(text); !reflect.DeepEqual(got, []string{first, second}) {
		t.Fatalf("Pixhost aliases inflated the screenshot count: %#v", got)
	}
}
