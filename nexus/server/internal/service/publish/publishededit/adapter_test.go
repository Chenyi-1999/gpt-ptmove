package publishededit

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const (
	testDetailsURL = novaOrigin + "/details.php?id=21&uploaded=1"
	testScreenshot = "[url=https://pixhost.to/show/100/a.png][img]https://img3.pixhost.to/images/100/a.png[/img][/url]"
	testMediaInfo  = "General\nDuration : 90 min\nVideo\nWidth : 3840 pixels\nAudio\nFormat : AAC"
)

type testTracker struct {
	owner                 string
	description           string
	mediainfo             string
	title                 string
	getEditCount          int
	postCount             int
	commit                bool
	postError             error
	redirectDetails       bool
	nfoAction             string
	beforeSubmitCalled    bool
	noticeUnexpectedRoute func(string)
}

func (tracker *testTracker) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != "pt.novahd.top" || request.URL.Scheme != "https" {
		tracker.noticeUnexpectedRoute(request.URL.Host + request.URL.Path)
		return nil, errors.New("unexpected host")
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	body := ""
	switch request.Method + " " + request.URL.Path {
	case "GET /details.php":
		if tracker.redirectDetails {
			response.StatusCode = http.StatusFound
			response.Header.Set("Location", "https://other.example/private")
			break
		}
		body = `<div class="nova-userbar-hello"><a href="/userdetails.php?id=13">测试账号</a></div>
<a href="edit.php?id=21" title="点击编辑或删除本种子"><img class="dt_edit" alt="edit"></a>
<table><tr><td class="rowfollow"><a class="index" href="download.php?id=21">下载</a>
由 <i>匿名</i> (<span class="nowrap"><a class="Uploader_Name" href="https://pt.novahd.top/userdetails.php?id=` + tracker.owner + `">发布者</a></span>)</td></tr></table>`
	case "GET /edit.php":
		tracker.getEditCount++
		token := fmt.Sprint(tracker.getEditCount)
		response.Header.Set("Set-Cookie", "edit_nonce="+token+"; Path=/; Secure")
		nfoControls := ""
		for _, action := range []string{"keep", "update", "remove"} {
			checked := ""
			if action == tracker.nfoAction {
				checked = " checked"
			}
			nfoControls += `<input name="nfoaction" type="radio" value="` + action + `"` + checked + `>`
		}
		body = `<div class="nova-userbar-hello"><a href="/userdetails.php?id=13">测试账号</a></div>
<form id="compose" name="edittorrent" action="takeedit.php" method="post" enctype="multipart/form-data">
<input type="hidden" name="id" value="21"><input type="hidden" name="_csrf_token" value="` + token + `">
<input name="name" value="` + html.EscapeString(tracker.title) + `"><input name="small_descr" value="">
<textarea name="descr">` + html.EscapeString(tracker.description) + `</textarea>
<textarea name="technical_info">` + html.EscapeString(tracker.mediainfo) + `</textarea>
` + nfoControls + `
<input name="nfo" type="file"><input name="anonymous" type="checkbox" value="yes" checked>
<input name="visible" type="checkbox" value="yes"><input name="tags[4][]" type="checkbox" value="4" checked>
<input name="tags[4][]" type="checkbox" value="12" checked><input name="tags[4][]" type="checkbox" value="9">
<select name="type"><option value="8">8</option><option selected value="4">4</option></select>
<input type="button" name="img" value="IMG"><input type="submit" name="edit" value="保存">
</form>`
	case "POST /takeedit.php":
		tracker.postCount++
		if !tracker.beforeSubmitCalled {
			tracker.noticeUnexpectedRoute("POST before attempt journal")
		}
		if err := request.ParseMultipartForm(2 << 20); err != nil {
			return nil, err
		}
		fields := request.MultipartForm.Value
		expected := map[string][]string{
			"id": {"21"}, "_csrf_token": {fmt.Sprint(tracker.getEditCount)}, "name": {tracker.title}, "small_descr": {""},
			"nfoaction": {"keep"}, "nfo": {""}, "anonymous": {"yes"}, "tags[4][]": {"4", "12"}, "type": {"4"}, "edit": {"保存"},
		}
		for key, value := range fields {
			if key != "descr" && key != "technical_info" && !reflect.DeepEqual(expected[key], value) {
				tracker.noticeUnexpectedRoute(fmt.Sprintf("unexpected form field %s: %v", key, value))
			}
		}
		if len(fields) != len(expected)+2 {
			tracker.noticeUnexpectedRoute(fmt.Sprintf("unexpected field count: %v", fields))
		}
		if nonce, err := request.Cookie("edit_nonce"); err != nil || nonce.Value != fmt.Sprint(tracker.getEditCount) {
			tracker.noticeUnexpectedRoute("fresh form cookie missing")
		}
		if request.Header.Get("Origin") != novaOrigin || request.Referer() != novaOrigin+"/edit.php?id=21" {
			tracker.noticeUnexpectedRoute("POST origin or referrer missing")
		}
		if tracker.commit {
			tracker.description = normalizeNewlines(request.FormValue("descr"))
			tracker.mediainfo = normalizeNewlines(request.FormValue("technical_info"))
		}
		if tracker.postError != nil {
			return nil, tracker.postError
		}
		response.StatusCode = http.StatusSeeOther
		response.Header.Set("Location", "https://other.example/private")
	default:
		tracker.noticeUnexpectedRoute(request.Method + " " + request.URL.Path)
		return nil, errors.New("unexpected path")
	}
	response.Body = io.NopCloser(strings.NewReader(body))
	return response, nil
}

func newFixture(t *testing.T) (*NovaHD, *testTracker) {
	t.Helper()
	nova, err := NewNovaHD(novaOrigin, "login=fake-test-session")
	if err != nil {
		t.Fatal(err)
	}
	tracker := &testTracker{
		owner: "13", title: " original title & spacing  ", nfoAction: "keep",
		description:           "[img]https://other.example/cover.png[/img]\n[quote]原简介 & 内容[/quote]\n\n",
		noticeUnexpectedRoute: func(route string) { t.Errorf("unexpected tracker request: %s", route) },
	}
	nova.client.Transport = tracker
	return nova, tracker
}

func TestNovaHDSavePreservesFormAndVerifiesAfterAmbiguousSubmit(t *testing.T) {
	nova, tracker := newFixture(t)
	tracker.commit = true
	tracker.postError = io.ErrUnexpectedEOF
	ctx := context.Background()
	snapshot, err := nova.Read(ctx, testDetailsURL)
	if err != nil {
		t.Fatal(err)
	}
	shot, media := testScreenshot, testMediaInfo
	change, err := snapshot.Preview(&shot, &media)
	if err != nil || change.After != tracker.description+testScreenshot || change.MediaInfoAfter != testMediaInfo || tracker.postCount != 0 {
		t.Fatalf("incorrect preview or unexpected side effects: %+v, %v, POSTs %d", change, err, tracker.postCount)
	}
	result, err := nova.Save(ctx, testDetailsURL, snapshot.Revision, &shot, &media, func(journalChange Change) error {
		if journalChange != change || tracker.postCount != 0 {
			t.Errorf("attempt journal ran out of order or saved wrong change: %+v", journalChange)
		}
		tracker.beforeSubmitCalled = true
		return nil
	})
	if err != nil || !result.Verified || tracker.postCount != 1 || tracker.getEditCount != 3 {
		t.Fatalf("expected one attempted POST and read-back success, got %+v, %v, %d POSTs, %d edit reads", result, err, tracker.postCount, tracker.getEditCount)
	}
}

func TestNovaHDSaveFailsClosed(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*testTracker)
		journalErr error
		wantErr    error
		wantPosts  int
	}{
		{name: "changed unrelated field", mutate: func(t *testTracker) { t.title += " edited" }, wantErr: ErrConflict},
		{name: "third party", mutate: func(t *testTracker) { t.owner = "14" }, wantErr: ErrPermission},
		{name: "cross origin redirect", mutate: func(t *testTracker) { t.redirectDetails = true }, wantErr: ErrPermission},
		{name: "destructive NFO action", mutate: func(t *testTracker) { t.nfoAction = "remove" }, wantErr: ErrUnsupported},
		{name: "journal failure", journalErr: io.ErrUnexpectedEOF, wantErr: io.ErrUnexpectedEOF},
		{name: "response success but changes absent", wantErr: ErrUnverified, wantPosts: 1},
	}
	for _, scenario := range tests {
		t.Run(scenario.name, func(t *testing.T) {
			nova, tracker := newFixture(t)
			ctx := context.Background()
			snapshot, err := nova.Read(ctx, testDetailsURL)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.mutate != nil {
				scenario.mutate(tracker)
			}
			shot := testScreenshot
			result, err := nova.Save(ctx, testDetailsURL, snapshot.Revision, &shot, nil, func(_ Change) error {
				tracker.beforeSubmitCalled = true
				return scenario.journalErr
			})
			if !errors.Is(err, scenario.wantErr) || result.Verified || tracker.postCount != scenario.wantPosts {
				t.Fatalf("want %v and %d POSTs, got %v, %+v and %d POSTs", scenario.wantErr, scenario.wantPosts, err, result, tracker.postCount)
			}
		})
	}
}

func TestNovaHDRejectsUnrelatedTargets(t *testing.T) {
	nova, tracker := newFixture(t)
	for _, input := range []string{
		"http://pt.novahd.top/details.php?id=21", "https://other.example/details.php?id=21",
		"https://pt.novahd.top@other.example/details.php?id=21", novaOrigin + "/offers.php?id=21", novaOrigin + "/details.php?id=21&id=22",
	} {
		if _, err := nova.Read(context.Background(), input); !errors.Is(err, ErrUnsupported) {
			t.Errorf("unexpected result for %s: %v", input, err)
		}
	}
	if tracker.getEditCount != 0 || tracker.postCount != 0 {
		t.Fatal("request was made for unrelated target")
	}
}

func TestSnapshotPreviewPreservesAndDeduplicatesScreenshots(t *testing.T) {
	alias := "[img]https://t3.pixhost.to/thumbs/100/a.png[/img]"
	newImage := "https://img3.pixhost.to/images/100/b.png"
	original := "[img]https://other.example/cover.png[/img]\n[quote]内容[/quote]\n" + testScreenshot
	snapshot := Snapshot{Description: original, ScreenshotBBCode: existingScreenshots(original), MediaInfo: testMediaInfo, MediaInfoEditable: true}
	input := alias + "\n" + newImage + "\n" + newImage
	change, err := snapshot.Preview(&input, nil)
	if err != nil || change.After != original+"\n[img]"+newImage+"[/img]" || change.MediaInfoAfter != testMediaInfo {
		t.Fatalf("invalid screenshot preview: %+v, %v", change, err)
	}
	for _, invalid := range []string{
		"[url=https://other.example/][img]" + newImage + "[/img][/url]", "[b]Extra description[/b]" + testScreenshot,
		"[img]https://img4.pixhost.to.evil/images/100/a.png[/img]", testScreenshot,
	} {
		if _, err := snapshot.Preview(&invalid, nil); !errors.Is(err, ErrInvalidChange) {
			t.Errorf("invalid or no-op screenshot was accepted: %q (%v)", invalid, err)
		}
	}
}

func TestPublishedEditAcceptsPixhostImageAndShowPathNames(t *testing.T) {
	s := Snapshot{Description: "Description"}
	code := "[url=https://pixhost.to/show/42/image.png][img]https://img99.pixhost.to/images/42/original.png[/img][/url]"
	change, err := s.Preview(&code, nil)
	if err != nil || change.ScreenshotBBCodeAfter != code {
		t.Fatalf("Pixhost service response rejected: %v", err)
	}
}
