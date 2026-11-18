package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	maxRequestBytes  = 64 << 10
	maxResponseBytes = 32 << 20
	claimWait        = 20 * time.Second
)

type Job struct {
	ID          string `json:"id"`
	Lease       string `json:"lease"`
	Lane        string `json:"lane"`
	Method      string `json:"method"`
	URI         string `json:"uri"`
	ContentType string `json:"content_type,omitempty"`
	Body        []byte `json:"body,omitempty"`
}

type Claim struct {
	Lane string `json:"lane"`
}

type Result struct {
	ID          string `json:"id"`
	Lease       string `json:"lease"`
	Status      int    `json:"status"`
	ContentType string `json:"content_type,omitempty"`
	Body        []byte `json:"body,omitempty"`
}

// validateJob 保留截图和 qB 只读请求的必要接口。每端独立校验。
func validateJob(j Job) error {
	if len(j.URI) > 16<<10 || !strings.HasPrefix(j.URI, "/") || strings.HasPrefix(j.URI, "//") ||
		strings.ContainsAny(j.URI, "\\#\r\n\x00") || len(j.Body) > maxRequestBytes || !validContentType(j.ContentType) {
		return errors.New("invalid task request")
	}
	u, err := url.ParseRequestURI(j.URI)
	if err != nil || u.Host != "" || u.IsAbs() || u.EscapedPath() != u.Path {
		return errors.New("expected a local API path")
	}
	switch j.Lane {
	case "qb":
		if j.Method != http.MethodGet || len(j.Body) != 0 {
			return errors.New("qB endpoint is read-only")
		}
		switch u.Path {
		case "/api/v2/app/version", "/api/v2/app/webapiVersion", "/api/v2/sync/maindata",
			"/api/v2/transfer/info", "/api/v2/torrents/info", "/api/v2/torrents/tags",
			"/api/v2/torrents/files", "/api/v2/torrents/properties":
			return nil
		}
	case "media":
		if u.RawQuery != "" || u.ForceQuery {
			return errors.New("media API does not accept query parameters")
		}
		if j.Method == http.MethodGet && u.Path == "/api/file/check" && len(j.Body) == 0 {
			return nil
		}
		mediaType, _, _ := mime.ParseMediaType(j.ContentType)
		if j.Method != http.MethodPost || mediaType != "application/json" || len(j.Body) == 0 || !json.Valid(j.Body) {
			return errors.New("media request requires a JSON body")
		}
		switch u.Path {
		case "/api/media/screenshot":
			var payload struct {
				Mode string `json:"mode"`
			}
			if err := json.Unmarshal(j.Body, &payload); err != nil {
				return errors.New("invalid screenshot request")
			}
			switch strings.ToLower(strings.TrimSpace(payload.Mode)) {
			case "", "preview", "inspect", "finalize":
				return nil
			}
			return errors.New("unsupported screenshot mode")
		case "/api/media/mediainfo", "/api/media/episode-count",
			"/api/file/check", "/api/file/batch-check":
			return nil
		}
	}
	return errors.New("task endpoint not allowed")
}

func secret(name string) (string, error) {
	raw := os.Getenv(name)
	filename := os.Getenv(name + "_FILE")
	if filename != "" {
		if raw != "" {
			return "", fmt.Errorf("set only %s or %s_FILE", name, name)
		}
		file, err := os.Open(filename)
		if err != nil {
			return "", fmt.Errorf("cannot open %s_FILE", name)
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 1025))
		if err != nil || len(data) > 1024 {
			return "", fmt.Errorf("cannot read %s_FILE", name)
		}
		raw = string(data)
	}
	value := strings.TrimSpace(raw)
	if len(value) < 16 || len(value) > 1024 || strings.ContainsAny(value, " \t\r\n\x00") {
		return "", fmt.Errorf("invalid or missing %s", name)
	}
	return value, nil
}

func authorized(r *http.Request, token string) bool {
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 {
		return false
	}
	provided, ok := strings.CutPrefix(headers[0], "Bearer ")
	if !ok || len(provided) > 1024 || token == "" {
		return false
	}
	supplied := sha256.Sum256([]byte(provided))
	expected := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(supplied[:], expected[:]) == 1
}

func decodeBounded(w http.ResponseWriter, r *http.Request, max int64, out any) error {
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/json" {
		return errors.New("expected JSON")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, max))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	switch err := dec.Decode(new(json.RawMessage)); err {
	case io.EOF:
		return nil
	case nil:
		return errors.New("expected one JSON document")
	default:
		return err
	}
}

func validContentType(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 256 || strings.ContainsAny(s, "\r\n\x00") {
		return false
	}
	t, _, err := mime.ParseMediaType(s)
	return err == nil && strings.Contains(t, "/")
}

func randomOpaqueID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func validOpaqueID(s string) bool {
	if len(s) != 32 || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func requestLimit(lane string) time.Duration {
	if lane == "qb" {
		return 35 * time.Second
	}
	return 18 * time.Minute
}
