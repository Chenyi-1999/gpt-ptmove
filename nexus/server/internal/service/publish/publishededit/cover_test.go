package publishededit

import (
	"context"
	"errors"
	"html"
	"io"
	"net/http"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

const (
	oldTestCover  = "https://img2.pixhost.to/images/42/poster.jpg"
	newTestCover  = "https://img3.pixhost.to/images/42/poster.jpg"
	testCoverPage = "https://pixhost.to/show/42/poster.jpg"
)

type coverRoundTrip func(*http.Request) (*http.Response, error)

func (roundTrip coverRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func newCoverTestClient(roundTrip coverRoundTrip) *http.Client {
	client := *pixhostCoverClient
	client.Transport = roundTrip
	return &client
}

func coverTestResponse(status int, contentType, body string) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", contentType)
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

func TestResolvePixhostCoverPaths(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		input string
		paths []string
	}{
		{"show page", testCoverPage, []string{testCoverPage, newTestCover}},
		{"working original", newTestCover, []string{newTestCover}},
		{"stale original", oldTestCover, []string{oldTestCover, testCoverPage, newTestCover}},
		{"thumbnail", "https://t3.pixhost.to/thumbs/42/poster.jpg", []string{testCoverPage, newTestCover}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var paths []string
			client := newCoverTestClient(func(request *http.Request) (*http.Response, error) {
				got := request.URL.String()
				paths = append(paths, got)
				if request.Method != "GET" || request.Header.Get("Cookie") != "" {
					t.Fatalf("unexpected Pixhost method or credential: %s", request.Method)
				}
				switch got {
				case testCoverPage:
					return coverTestResponse(200, "text/html; charset=UTF-8", "<img src=\"//img3.pixhost.to/images/42/poster.jpg\" id=\"image\">"), nil
				case oldTestCover:
					response := coverTestResponse(302, "text/html", "")
					response.Header.Set("Location", "https://pixhost.to/image/removed.png")
					return response, nil
				case newTestCover:
					if request.Referer() != novaOrigin+"/" {
						t.Error("original image is not verified with the NovaHD referrer")
					}
					return coverTestResponse(200, "image/jpeg", "\xff\xd8\xff\xe0"), nil
				default:
					t.Fatalf("unexpected Pixhost request: %s", got)
					return nil, errors.New("unexpected request")
				}
			})
			got, err := resolvePixhostCover(context.Background(), client, scenario.input)
			if err != nil || got != newTestCover || !reflect.DeepEqual(scenario.paths, paths) {
				t.Fatalf("resolved %q, paths %v, error %v", got, paths, err)
			}
		})
	}
}

func TestResolvePixhostCoverRejectsOtherOriginsAndNonImages(t *testing.T) {
	noRequests := newCoverTestClient(func(request *http.Request) (*http.Response, error) {
		t.Fatalf("invalid URL caused traffic: %s", request.URL)
		return nil, errors.New("unexpected request")
	})
	for _, input := range []string{
		"http://pixhost.to/show/42/poster.jpg",
		"https://81.70.24.5/show/42/poster.jpg",
		"https://pixhost.to.example.com/show/42/poster.jpg",
		"https://pixhost.to@img3.pixhost.to/images/42/poster.jpg",
		"https://img3.pixhost.to:443/images/42/poster.jpg",
		"https://pixhost.to/show/42/..",
		"https://pixhost.to/show/42/poster.jpg?url=other",
		"https://img3.pixhost.to/images/42/poster.jpg#",
		"https://img3.pixhost.to/images/42/a%0Ab.jpg",
		"https://pixhost.to/image/removed.png",
	} {
		if _, err := resolvePixhostCover(context.Background(), noRequests, input); !errors.Is(err, ErrInvalidChange) {
			t.Errorf("wanted invalid for %s, got %v", input, err)
		}
	}
	for _, target := range []string{
		"https://external.example/images/42/poster.jpg",
		"https://pixhost.to/image/removed.png",
		"https://t3.pixhost.to/thumbs/42/poster.jpg",
		"https://img3.pixhost.to/images/11/unrelated.jpg",
	} {
		var calls int
		client := newCoverTestClient(func(request *http.Request) (*http.Response, error) {
			calls++
			return coverTestResponse(200, "text/html", "<img id=\"image\" src=\""+target+"\">"), nil
		})
		if _, err := resolvePixhostCover(context.Background(), client, testCoverPage); !errors.Is(err, ErrCoverUnavailable) || calls != 1 {
			t.Errorf("wanted blocked from show page: %q, got %v with %d requests", target, err, calls)
		}
	}
	client := newCoverTestClient(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == testCoverPage {
			return coverTestResponse(200, "text/html", "<img id=\"image\" src=\""+newTestCover+"\">"), nil
		}
		return coverTestResponse(200, "image/jpeg", "<html>not an image</html>"), nil
	})
	if _, err := resolvePixhostCover(context.Background(), client, testCoverPage); !errors.Is(err, ErrCoverUnavailable) {
		t.Fatalf("spoofed image accepted: %v", err)
	}
}

func TestPixhostPublicIPGuard(t *testing.T) {
	for _, address := range []string{"81.70.24.5", "127.0.0.1", "10.0.0.1", "169.254.1.2", "100.64.0.1", "198.18.1.1", "fc00::2", "::1", "ff02::1", "64:ff9b::7f00:1", "::ffff:81.70.24.5"} {
		if isPublicPixhostIP(netip.MustParseAddr(address)) {
			t.Errorf("non-public or protected address accepted: %s", address)
		}
	}
	if !isPublicPixhostIP(netip.MustParseAddr("93.184.216.34")) {
		t.Error("public address rejected")
	}
}

func novaCoverWithHero(t *testing.T, description, hero string) string {
	t.Helper()
	page, err := xhtml.Parse(strings.NewReader(hero))
	if err != nil {
		t.Fatal(err)
	}
	return verifiedNovaCover(page, description)
}

func TestPreviewCoverChangesOnlyVerifiedLeadImage(t *testing.T) {
	const prefix = "简介开头\r\n"
	const suffix = "\n\n[quote]保留正文[/quote]\n\n" + testScreenshot
	const replacement = "https://img3.pixhost.to/images/43/new.jpg"
	for _, test := range []struct{ oldLink, newLink string }{
		{testCoverPage, replacement},
		{oldTestCover, replacement},
		{"https://external.example/official-info", "https://external.example/official-info"},
	} {
		t.Run(test.oldLink, func(t *testing.T) {
			description := prefix + "[URL=\"" + test.oldLink + "\"] [IMG=520x760]" + oldTestCover + "[/IMG][/URL]" + suffix
			hero := "<img class=\"fit nova-movie-hero-poster\" src=\"" + oldTestCover + "\">"
			snapshot := Snapshot{Description: description, CoverURL: novaCoverWithHero(t, description, hero), MediaInfo: testMediaInfo}
			change, err := snapshot.PreviewCover(replacement)
			want := prefix + "[URL=\"" + test.newLink + "\"] [IMG=520x760]" + replacement + "[/IMG][/URL]" + suffix
			if err != nil || change.After != want || change.Before != description || change.MediaInfoBefore != testMediaInfo ||
				change.MediaInfoAfter != testMediaInfo || change.ScreenshotBBCodeBefore != testScreenshot ||
				change.ScreenshotBBCodeAfter != testScreenshot || change.CoverURLBefore != oldTestCover || change.CoverURLAfter != replacement {
				t.Fatalf("expected cover-only replacement, got %+v and %v", change, err)
			}
			if test.oldLink == testCoverPage {
				sameAsset, err := snapshot.PreviewCover(newTestCover)
				if err != nil || sameAsset.After != strings.Replace(description, oldTestCover, newTestCover, 1) {
					t.Errorf("same asset should keep existing preview page: %v", err)
				}
			}
			if _, err := snapshot.PreviewCover(oldTestCover); !errors.Is(err, ErrCoverUnchanged) {
				t.Fatalf("identical cover: %v", err)
			}
		})
	}
	description := "[img]" + oldTestCover + "[/img]" + suffix
	for _, hero := range []string{
		"",
		"<img class=\"nova-movie-hero-poster\" src=\"" + newTestCover + "\">",
		strings.Repeat("<img class=\"nova-movie-hero-poster\" src=\""+oldTestCover+"\">", 2),
	} {
		snapshot := Snapshot{Description: description, CoverURL: novaCoverWithHero(t, description, hero)}
		if _, err := snapshot.PreviewCover(newTestCover); !errors.Is(err, ErrCoverMissing) {
			t.Errorf("ambiguous cover accepted: %v for %q", err, hero)
		}
	}
}

func TestNovaHDSaveCoverFencesAndVerifiesFullForm(t *testing.T) {
	for _, s := range []struct {
		name       string
		setup      func(*testTracker)
		drift      bool
		badJPEG    bool
		journalErr error
		wantErr    error
		wantPosts  int
	}{
		{name: "read-back after ambiguous response", wantPosts: 1},
		{name: "stale form", setup: func(t *testTracker) { t.title += " updated" }, wantErr: ErrConflict},
		{name: "publisher changed", setup: func(t *testTracker) { t.owner = "15" }, wantErr: ErrPermission},
		{name: "selected image unavailable", badJPEG: true, wantErr: ErrCoverUnavailable},
		{name: "journal failed", journalErr: io.ErrUnexpectedEOF, wantErr: io.ErrUnexpectedEOF},
		{name: "unrelated field drift after submit", drift: true, wantErr: ErrUnverified, wantPosts: 1},
	} {
		t.Run(s.name, func(t *testing.T) {
			nova, tracker := newFixture(t)
			tracker.description = "[img]" + oldTestCover + "[/img]\n简介正文\n" + testScreenshot
			tracker.mediainfo = testMediaInfo
			tracker.commit = true
			tracker.postError = io.ErrUnexpectedEOF
			saving, pixhostChecks := false, 0
			nova.client.Transport = coverRoundTrip(func(request *http.Request) (*http.Response, error) {
				if saving && pixhostChecks == 0 {
					t.Error("save reads the tracker form before verifying the selected image")
				}
				response, err := tracker.RoundTrip(request)
				if request.Method == http.MethodPost && s.drift {
					tracker.title += " drifted"
				}
				if err != nil || request.URL.Path != "/details.php" {
					return response, err
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil {
					return nil, err
				}
				lead, _ := leadingCoverImage(tracker.description)
				cover := tracker.description[lead.imageStart:lead.imageEnd]
				response.Body = io.NopCloser(strings.NewReader(string(body) + "<img class=\"nova-movie-hero-poster\" src=\"" + html.EscapeString(cover) + "\">"))
				return response, nil
			})
			nova.coverClient = newCoverTestClient(func(request *http.Request) (*http.Response, error) {
				pixhostChecks++
				if request.URL.String() != newTestCover {
					t.Fatalf("save checked a different cover %s", request.URL)
				}
				if s.badJPEG {
					return coverTestResponse(404, "text/html", ""), nil
				}
				return coverTestResponse(200, "image/jpeg", "\xff\xd8\xff\xe0"), nil
			})
			ctx := context.Background()
			snapshot, err := nova.Read(ctx, testDetailsURL)
			if err != nil || snapshot.CoverURL != oldTestCover {
				t.Fatalf("cover not verified: %+v %v", snapshot, err)
			}
			preview, err := snapshot.PreviewCover(newTestCover)
			if err != nil || tracker.postCount != 0 || pixhostChecks != 0 {
				t.Fatalf("unsafe preview: %+v %v", preview, err)
			}
			if s.setup != nil {
				s.setup(tracker)
			}
			saving = true
			result, err := nova.SaveCover(ctx, testDetailsURL, snapshot.Revision, newTestCover, func(change Change) error {
				tracker.beforeSubmitCalled = true
				if tracker.postCount != 0 || change != preview {
					t.Errorf("attempt journal saw wrong change or ran too late: %+v", change)
				}
				return s.journalErr
			})
			if !errors.Is(err, s.wantErr) || result.Verified != (s.wantErr == nil) || tracker.postCount != s.wantPosts {
				t.Fatalf("result %+v, error %v, posts %d", result, err, tracker.postCount)
			}
			if s.wantPosts == 1 && (tracker.description != preview.After || tracker.mediainfo != testMediaInfo || pixhostChecks != 1) {
				t.Errorf("saved wrong fields or validated wrong number of images: %d", pixhostChecks)
			}
		})
	}
}
