package main

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestCoordinatorDialAddressProtection(t *testing.T) {
	for _, address := range []string{"81.70.24.5:28443", "[::ffff:81.70.24.5]:28443"} {
		if err := protectAgentCoordinator(context.Background(), "tcp", address, nil); err == nil {
			t.Errorf("coordinator dial to protected address %s would be allowed", address)
		}
	}
	if err := protectAgentCoordinator(context.Background(), "tcp", "152.136.236.238:28443", nil); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownScreenshotModeCannotStartUpload(t *testing.T) {
	job := Job{
		Lane:        "media",
		Method:      http.MethodPost,
		URI:         "/api/media/screenshot",
		ContentType: "application/json",
		Body:        []byte(`{"remote_path":"/keep/example.mkv","mode":"typo"}`),
	}
	if err := validateJob(job); err == nil {
		t.Fatal("unexpected screenshot mode could reach the NAS")
	}
	for _, mode := range []string{"", "inspect", "preview", "finalize"} {
		job.Body = []byte(`{"remote_path":"/keep/example.mkv","mode":"` + mode + `"}`)
		if err := validateJob(job); err != nil {
			t.Errorf("expected screenshot mode %q to be accepted: %v", mode, err)
		}
	}
}

const qbAddTestTorrent = "d4:infod6:lengthi1e4:name4:test12:piece lengthi16384e6:pieces20:012345678901234567897:privatei1eee"

type qbAddTestPart struct {
	name     string
	filename string
	value    string
}

func makeQBAddTestJob(t *testing.T, parts ...qbAddTestPart) Job {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.SetBoundary("ptmove-qb-add-boundary"); err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if part.filename == "" {
			if err := writer.WriteField(part.name, part.value); err != nil {
				t.Fatal(err)
			}
			continue
		}
		file, err := writer.CreateFormFile(part.name, part.filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(part.value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return Job{Lane: "qb", Method: http.MethodPost, URI: qbAddURI, ContentType: writer.FormDataContentType(), Body: body.Bytes()}
}

func TestQBAddAllowsNativeClientMultipart(t *testing.T) {
	for _, path := range []string{"/vol1/保种/默认 路径/", `D:\保种\默认 路径`, "D:/Seed Library", "/"} {
		t.Run(path, func(t *testing.T) {
			job := makeQBAddTestJob(t,
				qbAddTestPart{name: "torrents", filename: "自定义 种子.torrent", value: qbAddTestTorrent},
				qbAddTestPart{name: "savepath", value: path},
				qbAddTestPart{name: "paused", value: "false"},
				qbAddTestPart{name: "skip_checking", value: "true"},
				qbAddTestPart{name: "tags", value: "发布,留种"},
				qbAddTestPart{name: "category", value: "电影/已发布"},
				qbAddTestPart{name: "upLimit", value: "2097152"},
			)
			original := bytes.Clone(job.Body)
			if err := validateJob(job); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(job.Body, original) {
				t.Fatal("validation changed the body that will be forwarded")
			}
		})
	}
}

func TestQBAddRejectsInvalidParts(t *testing.T) {
	file := qbAddTestPart{name: "torrents", filename: "test.torrent", value: qbAddTestTorrent}
	for _, tc := range []struct {
		name  string
		parts []qbAddTestPart
	}{
		{"missing file", []qbAddTestPart{{name: "paused", value: "true"}}},
		{"duplicate file", []qbAddTestPart{file, file}},
		{"empty file", []qbAddTestPart{{name: "torrents", filename: "test.torrent"}}},
		{"wrong extension", []qbAddTestPart{{name: "torrents", filename: "test.txt", value: qbAddTestTorrent}}},
		{"path as filename", []qbAddTestPart{{name: "torrents", filename: "../test.torrent", value: qbAddTestTorrent}}},
		{"oversized filename", []qbAddTestPart{{name: "torrents", filename: strings.Repeat("a", 256) + ".torrent", value: qbAddTestTorrent}}},
		{"nonfile part", []qbAddTestPart{{name: "torrents", value: qbAddTestTorrent}}},
		{"magnet as file", []qbAddTestPart{{name: "torrents", filename: "test.torrent", value: "magnet:?xt=urn:btih:example"}}},
		{"URL as file", []qbAddTestPart{{name: "torrents", filename: "test.torrent", value: "https://example.invalid/test.torrent"}}},
		{"urls field", []qbAddTestPart{file, {name: "urls", value: "https://example.invalid/test.torrent"}}},
		{"magnet field", []qbAddTestPart{file, {name: "urls", value: "magnet:?xt=urn:btih:example"}}},
		{"unsupported field", []qbAddTestPart{file, {name: "autoTMM", value: "true"}}},
		{"duplicate field", []qbAddTestPart{file, {name: "paused", value: "true"}, {name: "paused", value: "false"}}},
		{"file in scalar field", []qbAddTestPart{file, {name: "tags", filename: "tags.torrent", value: "tag"}}},
		{"relative path", []qbAddTestPart{file, {name: "savepath", value: "downloads/torrents"}}},
		{"parent component", []qbAddTestPart{file, {name: "savepath", value: "/volume/downloads/../torrents"}}},
		{"Windows parent component", []qbAddTestPart{file, {name: "savepath", value: `D:\downloads\.. \torrents`}}},
		{"UNC path", []qbAddTestPart{file, {name: "savepath", value: `\\computer\downloads`}}},
		{"remote URL as path", []qbAddTestPart{file, {name: "savepath", value: "file://computer/downloads"}}},
		{"path control character", []qbAddTestPart{file, {name: "savepath", value: "/downloads/\x00torrents"}}},
		{"tags control character", []qbAddTestPart{file, {name: "tags", value: "tag\r\nother"}}},
		{"oversized tags", []qbAddTestPart{file, {name: "tags", value: strings.Repeat("a", (4<<10)+1)}}},
		{"nonboolean paused", []qbAddTestPart{file, {name: "paused", value: "maybe"}}},
		{"nonpositive upload limit", []qbAddTestPart{file, {name: "upLimit", value: "-800"}}},
		{"overflowing upload limit", []qbAddTestPart{file, {name: "upLimit", value: "9223372036854775808"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateJob(makeQBAddTestJob(t, tc.parts...)); err == nil {
				t.Fatal("invalid multipart upload passed validation")
			}
		})
	}
}

func TestQBAddRejectsAmbiguousMultipart(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Job)
	}{
		{"alternate content type", func(j *Job) { j.ContentType = "application/x-www-form-urlencoded" }},
		{"missing boundary", func(j *Job) { j.ContentType = "multipart/form-data" }},
		{"duplicate boundary", func(j *Job) { j.ContentType += "; boundary=ptmove-qb-add-boundary" }},
		{"duplicate part name", func(j *Job) {
			j.Body = bytes.Replace(j.Body, []byte(`name="torrents"`), []byte(`name="torrents"; name="urls"`), 1)
		}},
		{"extra part header", func(j *Job) {
			j.Body = bytes.Replace(j.Body, []byte("Content-Type:"), []byte("Content-Transfer-Encoding: quoted-printable\r\nContent-Type:"), 1)
		}},
		{"preamble", func(j *Job) { j.Body = append([]byte("ignored\r\n"), j.Body...) }},
		{"epilogue", func(j *Job) { j.Body = append(j.Body, []byte("ignored")...) }},
		{"truncated boundary", func(j *Job) { j.Body = j.Body[:len(j.Body)-4] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := makeQBAddTestJob(t, qbAddTestPart{name: "torrents", filename: "test.torrent", value: qbAddTestTorrent})
			tc.mutate(&job)
			if err := validateJob(job); err == nil {
				t.Fatal("ambiguous multipart upload passed validation")
			}
		})
	}
}

func TestQBAddOnlyExtendsExactRouteSizeLimit(t *testing.T) {
	for _, tc := range []struct {
		lane, method, uri string
	}{
		{"qb", http.MethodPost, qbAddURI + "?urls=x"},
		{"qb", http.MethodPost, qbAddURI + "?"},
		{"qb", http.MethodPost, qbAddURI + "/"},
		{"qb", http.MethodPost, "/api/v2/torrents/%61dd"},
		{"qb", http.MethodPost, "/api/v2/torrents/ADD"},
		{"qb", http.MethodPost, "/api/v2/torrents/../torrents/add"},
		{"qb", http.MethodPost, "/api/v2/torrents/delete"},
		{"qb", http.MethodGet, qbAddURI},
		{"qb", http.MethodPut, qbAddURI},
		{"media", http.MethodPost, qbAddURI},
	} {
		t.Run(tc.lane+"/"+tc.method+tc.uri, func(t *testing.T) {
			if got := jobRequestLimit(tc.lane, tc.method, tc.uri); got != maxRequestBytes {
				t.Fatalf("unexpected extended size limit: %d", got)
			}
			job := makeQBAddTestJob(t, qbAddTestPart{name: "torrents", filename: "test.torrent", value: qbAddTestTorrent})
			job.Lane, job.Method, job.URI = tc.lane, tc.method, tc.uri
			if err := validateJob(job); err == nil {
				t.Fatal("request outside exact qB route passed validation")
			}
		})
	}
	if got := jobRequestLimit("qb", http.MethodPost, qbAddURI); got != maxTorrentRequestBytes {
		t.Fatalf("unexpected qB upload limit: %d", got)
	}
	file := qbAddTestPart{name: "torrents", filename: "test.torrent"}
	const baseCommentLength = maxTorrentRequestBytes - 1024
	file.value = "d7:comment" + strconv.Itoa(baseCommentLength) + ":" + strings.Repeat("a", baseCommentLength) + qbAddTestTorrent[1:]
	job := makeQBAddTestJob(t, file)
	extra := maxTorrentRequestBytes - len(job.Body)
	commentLength := baseCommentLength + extra
	file.value = "d7:comment" + strconv.Itoa(commentLength) + ":" + strings.Repeat("a", commentLength) + qbAddTestTorrent[1:]
	job = makeQBAddTestJob(t, file)
	if len(job.Body) != maxTorrentRequestBytes {
		t.Fatalf("test body should be at the limit, got %d bytes", len(job.Body))
	}
	if err := validateJob(job); err != nil {
		t.Fatalf("request at limit was rejected: %v", err)
	}
	commentLength++
	file.value = "d7:comment" + strconv.Itoa(commentLength) + ":" + strings.Repeat("a", commentLength) + qbAddTestTorrent[1:]
	if err := validateJob(makeQBAddTestJob(t, file)); err == nil {
		t.Fatal("request above limit passed validation")
	}
}

func TestQBAddSetsStoppedForNewQBittorrent(t *testing.T) {
	file := qbAddTestPart{name: "torrents", filename: "fixture.torrent", value: qbAddTestTorrent}
	for _, pause := range []string{"true", "false"} {
		flag := qbAddTestPart{name: "paused", value: pause}
		job := makeQBAddTestJob(t, file, flag)
		want := makeQBAddTestJob(t, file, flag, qbAddTestPart{name: "stopped", value: pause})
		got, err := qbAddBody(job.ContentType, job.Body, true)
		if err != nil || !bytes.Equal(got, want.Body) {
			t.Fatalf("upstream must preserve the file and append stopped=%s: %v", pause, err)
		}
		if validateJob(want) == nil {
			t.Fatal("controller must not supply the bridge-owned stopped field")
		}
	}
}
