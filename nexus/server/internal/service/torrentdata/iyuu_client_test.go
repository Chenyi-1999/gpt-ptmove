package torrentdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const (
	iyuuHashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	iyuuHashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	iyuuHashC = "cccccccccccccccccccccccccccccccccccccccc"
	iyuuHashD = "dddddddddddddddddddddddddddddddddddddddd"
	iyuuHashE = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

type iyuuRoundTripFunc func(*http.Request) (*http.Response, error)

func (f iyuuRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func iyuuTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func iyuuTestClient(t *testing.T, sites string, batch func(*http.Request) *http.Response) (*iyuuClient, *[]string) {
	t.Helper()
	client := newIYUUClient()
	client.rateLimitDelay = 0
	var paths []string
	client.httpClient.Transport = iyuuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.URL.Host != "2025.iyuu.cn" || req.Header.Get("Token") != "fixture-token" {
			t.Fatalf("unexpected IYUU request origin or token")
		}
		switch req.URL.Path {
		case "/reseed/sites/index":
			if req.Method != http.MethodGet {
				t.Fatalf("site list used method %s", req.Method)
			}
			return iyuuTestResponse(http.StatusOK, sites), nil
		case "/reseed/sites/reportExisting":
			var payload struct {
				SIDs []int64 `json:"sid_list"`
			}
			if req.Method != http.MethodPost || json.NewDecoder(req.Body).Decode(&payload) != nil || !reflect.DeepEqual(payload.SIDs, []int64{7, 42}) {
				t.Fatalf("unexpected IYUU site scope: %v", payload.SIDs)
			}
			return iyuuTestResponse(http.StatusOK, `{"code":0,"data":{"sid_sha1":"`+iyuuHashE+`"}}`), nil
		case "/reseed/index/index":
			if batch != nil {
				return batch(req), nil
			}
		}
		t.Fatalf("unexpected IYUU path %s", req.URL.Path)
		return nil, nil
	})
	return client, &paths
}

const iyuuTestSites = `{"code":0,"data":{"sites":[{"id":42,"base_url":"pt.example.org"},{"id":7,"base_url":"https://other.example.org/"}]}}`

func TestIYUUDuplicateQueryBatchesHashesAndKeepsPartialEvidence(t *testing.T) {
	client, paths := iyuuTestClient(t, iyuuTestSites, func(req *http.Request) *http.Response {
		if req.Method != http.MethodPost || req.ParseForm() != nil {
			t.Fatal("batch lookup did not submit the hash form")
		}
		var sent []string
		body := req.Form.Get("hash")
		if json.Unmarshal([]byte(body), &sent) != nil || !reflect.DeepEqual(sent, []string{iyuuHashA, iyuuHashB, iyuuHashC, iyuuHashD, iyuuHashE}) ||
			req.Form.Get("sha1") != sha1Hex(body) || req.Form.Get("sid_sha1") != iyuuHashE || req.Form.Get("timestamp") == "" || req.Form.Get("version") != iyuuClientVersion {
			t.Fatalf("unexpected hash query: %v", sent)
		}
		return iyuuTestResponse(http.StatusOK, fmt.Sprintf(`{"code":0,"data":{
			%q:{"torrent":[{"sid":42,"torrent_id":1,"info_hash":%q}]},
			%q:{"torrent":[]},
			%q:{"torrent":[{"sid":42.5,"torrent_id":2,"info_hash":%q}]},
			%q:{"torrent":[{"sid":7,"torrent_id":3,"info_hash":%q}]}
		}}`, iyuuHashA, iyuuHashC, iyuuHashB, iyuuHashD, iyuuHashB, iyuuHashE, iyuuHashC))
	})
	got, err := client.queryDuplicates(context.Background(), "fixture-token", "https://PT.EXAMPLE.ORG/", []string{
		strings.ToUpper(iyuuHashA), iyuuHashA, "not-an-infohash", iyuuHashB, iyuuHashC, iyuuHashD, iyuuHashE,
	})
	want := map[string]bool{iyuuHashA: true, iyuuHashB: false, iyuuHashE: false}
	if err != nil || !reflect.DeepEqual(got, want) || len(*paths) != 3 {
		t.Fatalf("evidence=%v err=%v paths=%v, want %v from one batch lookup", got, err, *paths, want)
	}
}

func TestIYUUDuplicateQueryDistinguishesExplicitEmptyAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantEmpty  bool
	}{
		{"explicit no match", `{"code":400,"data":[],"msg":"未查询到可辅种数据"}`, http.StatusOK, true},
		{"bad signature", `{"code":400,"data":[],"msg":"做种列表sha1校验失败"}`, http.StatusOK, false},
		{"missing no-match payload", `{"code":400,"msg":"未查询到可辅种数据"}`, http.StatusOK, false},
		{"HTTP error", `{"code":400,"data":[],"msg":"未查询到可辅种数据"}`, http.StatusBadRequest, false},
		{"expired token", `{"code":403,"data":[]}`, http.StatusOK, false},
		{"missing status", `{"data":{}}`, http.StatusOK, false},
		{"success with invalid data", `{"code":0,"data":[]}`, http.StatusOK, false},
		{"malformed JSON", `{"code":0,"data":{}}{}`, http.StatusOK, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := iyuuTestClient(t, iyuuTestSites, func(_ *http.Request) *http.Response {
				return iyuuTestResponse(tc.status, tc.body)
			})
			got, err := client.queryDuplicates(context.Background(), "fixture-token", "https://pt.example.org", []string{iyuuHashA, iyuuHashB})
			if tc.wantEmpty {
				if err != nil || !reflect.DeepEqual(got, map[string]bool{iyuuHashA: false, iyuuHashB: false}) {
					t.Fatalf("explicit no match was lost: %v, %v", got, err)
				}
			} else if err == nil || len(got) != 0 {
				t.Fatalf("unrecognized response produced evidence: %v, %v", got, err)
			}
		})
	}
}

func TestIYUUDuplicateQueryRejectsAmbiguousOrUnsupportedTarget(t *testing.T) {
	for _, sites := range []string{
		`{"code":0,"data":{"sites":[{"id":42,"base_url":"pt.example.org.lookalike.example"}]}}`,
		`{"code":0,"data":{"sites":[{"id":42,"base_url":"pt.example.org"},{"id":7,"base_url":"https://pt.example.org/"}]}}`,
		`{"code":0,"data":{"sites":[{"id":42,"base_url":"pt.example.org"},{"id":42,"base_url":"https://other.example.org/"}]}}`,
	} {
		client, paths := iyuuTestClient(t, sites, nil)
		result, err := client.queryDuplicates(context.Background(), "fixture-token", "https://pt.example.org", []string{iyuuHashA})
		if err == nil || len(result) != 0 || len(*paths) != 1 {
			t.Fatalf("ambiguous or unsupported target produced evidence: %v, %v, paths=%v", result, err, *paths)
		}
	}
}

func TestIYUUDuplicateQueryDoesNotFollowRedirectWithToken(t *testing.T) {
	client := newIYUUClient()
	client.httpClient.Transport = iyuuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "2025.iyuu.cn" || req.URL.Path != "/reseed/sites/index" {
			t.Fatal("authenticated IYUU request followed redirect")
		}
		response := iyuuTestResponse(http.StatusFound, "")
		response.Header.Set("Location", "https://other.example.org/")
		return response, nil
	})
	result, err := client.queryDuplicates(context.Background(), "fixture-token", "https://pt.example.org", []string{iyuuHashA})
	if err == nil || len(result) != 0 {
		t.Fatalf("redirect produced evidence: %v, %v", result, err)
	}
}
