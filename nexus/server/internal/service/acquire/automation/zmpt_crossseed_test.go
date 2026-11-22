package automation

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestZmptCrossSeedEvidence(t *testing.T) {
	for _, tt := range []struct {
		name, body       string
		known, duplicate bool
	}{
		{"target listed", `{"code":0,"data":[{"url":"https://pt.novahd.top/details.php?id=123","name":"NovaHD"}]}`, true, true},
		{"another site", `{"code":0,"data":[{"url":"https://other.example/details.php?site=pt.novahd.top","name":"Another site"}]}`, true, false},
		{"lookalike host", `{"code":0,"data":[{"url":"https://pt.novahd.top.other.example/"}]}`, true, false},
		{"explicit empty list", `{"code":0,"data":[]}`, true, false},
		{"explicit no cross-seed data", `{"code":-1,"message":"查询辅种信息失败！Error: 查询失败: IYUU API Error: 未查询到可辅种数据","data":null}`, true, false},
		{"no-match message without data field", `{"code":-1,"message":"查询辅种信息失败！Error: 查询失败: IYUU API Error: 未查询到可辅种数据"}`, false, false},
		{"unsuccessful API", `{"code":-1,"message":"error","data":[]}`, false, false},
		{"upstream timeout", `{"code":-1,"message":"查询辅种信息失败！Error: 查询失败: IYUU API Error: timeout","data":null}`, false, false},
		{"missing code", `{"data":[]}`, false, false},
		{"missing list", `{"code":0}`, false, false},
		{"null list", `{"code":0,"data":null}`, false, false},
		{"unexpected list format", `{"code":0,"data":{}}`, false, false},
		{"site missing URL", `{"code":0,"data":[{"name":"NovaHD"}]}`, false, false},
		{"relative URL", `{"code":0,"data":[{"url":"/details.php?id=123"}]}`, false, false},
		{"partial list", `{"code":0,"data":[{"url":"https://pt.novahd.top/"},null]}`, false, false},
		{"truncated JSON", `{"code":0,"data":[`, false, false},
		{"HTML login page", `<html><input type="password"></html>`, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			known, duplicate := zmptCrossSeedEvidence([]byte(tt.body), "https://pt.novahd.top")
			if known != tt.known || duplicate != tt.duplicate {
				t.Fatalf("known=%v, duplicate=%v, want %v, %v", known, duplicate, tt.known, tt.duplicate)
			}
		})
	}
}

func TestZmptCrossSeedWidgetReadFailuresStayUnknown(t *testing.T) {
	const hash = "0123456789012345678901234567890123456789"
	const detail = "https://zmpt.cc/details.php?id=5"
	const cell = `<div id="resultContainer"></div>`
	const validPage = cell + `<span id="hashID">` + hash + `</span>`
	for _, tt := range []struct {
		name, page, body string
		status           int
		readError        error
		requested        bool
	}{
		{name: "empty placeholder without hash", page: cell},
		{name: "hash from different torrent", page: cell + `<span id="hashID">` + strings.Repeat("0", 40) + `</span>`},
		{name: "no widget", page: `<span id="hashID">` + hash + `</span>`},
		{name: "timeout", page: validPage, readError: context.DeadlineExceeded, requested: true},
		{name: "HTTP failure", page: validPage, status: 503, body: `{"code":0,"data":[]}`, requested: true},
		{name: "widget still empty", page: validPage, status: 200, requested: true},
		{name: "HTML returned instead of widget data", page: validPage, status: 200, body: cell, requested: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := html.Parse(strings.NewReader(tt.page))
			if err != nil {
				t.Fatal(err)
			}
			requested := false
			s := NewScanner()
			s.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
				requested = true
				if req.Method != http.MethodGet || req.URL.String() != "https://zmpt.cc"+zmptCrossSeedPath+"?hash="+hash ||
					req.Header.Get("Referer") != detail || req.Header.Get("X-Requested-With") != "XMLHttpRequest" || req.Header.Get("Cookie") != "fixture=zmpt" {
					t.Fatalf("unexpected widget request: %s %s", req.Method, req.URL)
				}
				if tt.readError != nil {
					return nil, tt.readError
				}
				return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})
			known, duplicate := s.zmptCrossSeedDuplicate(context.Background(), testProfile("https://zmpt.cc"), testProfile("https://pt.novahd.top"), "fixture=zmpt", doc, detail, hash)
			if known || duplicate || requested != tt.requested {
				t.Fatalf("known=%v, duplicate=%v, requested=%v (want %v)", known, duplicate, requested, tt.requested)
			}
		})
	}
}
