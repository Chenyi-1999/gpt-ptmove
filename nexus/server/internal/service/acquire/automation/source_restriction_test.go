package automation

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSourceRestrictionUsesExactDetailAndTorrentIdentity(t *testing.T) {
	const validDetail = `<h1>Movie</h1><div id="kdescr">基本信息</div><a href="/download.php?id=12">download</a>`
	const validTorrent = "d8:announce35:https://source.example.com/announce4:infod6:lengthi1073741824e4:name4:test12:piece lengthi1073741824e6:pieces20:xxxxxxxxxxxxxxxxxxxx7:privatei1eee"
	for _, test := range []struct {
		name, details, redirect string
		known, allowed          bool
		requests                int
	}{
		{"allowed", validDetail, "", true, true, 2},
		{"badge denial wins", validDetail + `<div id="allow"></div><img title="严禁转载">`, "", true, false, 1},
		{"unrecognized", "<h1>Maintenance</h1>", "", false, false, 1},
		{"login despite allow selector", `<div id="allow"></div><input type="password">`, "", false, false, 1},
		{"another detail", validDetail, "/details.php?id=99", false, false, 1},
		{"different host", validDetail, "https://another.example.com/details.php?id=12", false, false, 1},
		{"wrong download id", strings.ReplaceAll(validDetail, "id=12", "id=99"), "", false, false, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := testProfile("https://source.example.com")
			p.Selectors.RestrictionAllowed = "#allow"
			s := NewScanner()
			requests := 0
			s.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.Method != "GET" || req.URL.Host != "source.example.com" || req.URL.Query().Get("id") != "12" {
					t.Fatalf("unexpected request %s %s", req.Method, req.URL)
				}
				status, body := 200, test.details
				header := make(http.Header)
				if req.URL.Path == "/details.php" && test.redirect != "" {
					status = http.StatusFound
					header.Set("Location", test.redirect)
				}
				if req.URL.Path == "/download.php" {
					body = validTorrent
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			known, allowed, hash, err := s.InspectSourceRestriction(context.Background(), p, "test-cookie", "12")
			if known != test.known || allowed != test.allowed || (hash != "") != test.allowed || requests != test.requests {
				t.Fatalf("known=%v allowed=%v hash=%q requests=%d err=%v", known, allowed, hash, requests, err)
			}
		})
	}
}

func TestSourceRestrictionLegacyReferencesMustMatchRegisteredDetail(t *testing.T) {
	p := testProfile("https://source.example.com")
	if got, err := SourceRestrictionID(p, "12", "https://source.example.com/details.php?id=12&hit=1"); got != "12" || err != nil {
		t.Fatalf("legacy detail rejected: %q %v", got, err)
	}
	for _, detail := range []string{
		"https://elsewhere.example.com/details.php?id=12",
		"/torrents.php?id=12", "/details.php?id=999",
		"/details.php?id=12&id=999", "/details.php?id=12&torrent_id=999",
	} {
		if got, err := SourceRestrictionID(p, "12", detail); err == nil {
			t.Errorf("unrelated details accepted: %q -> %q", detail, got)
		}
	}
}
