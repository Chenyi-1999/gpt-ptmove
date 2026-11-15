package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxMediaBody = 64 << 10

type mediaPolicy struct {
	tokenHash [32]byte
	roots     []string
}

// Roots are an explicit JSON array of absolute directories; no OS-dependent delimiter.
func loadMediaPolicy(token, rawRoots string) (*mediaPolicy, error) {
	if utf8.RuneCountInString(token) < 32 || strings.TrimSpace(token) != token {
		return nil, errors.New("PTMOVE_PROXY_TOKEN must contain at least 32 characters without surrounding whitespace")
	}
	var roots []string
	if json.Unmarshal([]byte(rawRoots), &roots) != nil || len(roots) == 0 {
		return nil, errors.New("PTMOVE_MEDIA_ROOTS must be a nonempty JSON array of absolute media directories")
	}
	policy := &mediaPolicy{tokenHash: sha256.Sum256([]byte(token))}
	for _, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
			return nil, errors.New("media root must be an absolute directory other than filesystem root")
		}
		canonical, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, errors.New("media root is unavailable")
		}
		if filepath.Dir(canonical) == canonical {
			return nil, errors.New("filesystem root cannot be exported")
		}
		info, err := os.Stat(canonical)
		if err != nil || !info.IsDir() {
			return nil, errors.New("media root must be a directory")
		}
		policy.roots = append(policy.roots, canonical)
	}
	return policy, nil
}

func (p *mediaPolicy) resolve(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || !filepath.IsAbs(raw) || strings.ContainsAny(raw, "\x00\r\n") || strings.Contains(raw, "://") || strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, `\\`) {
		return "", errors.New("invalid media path")
	}
	for _, segment := range strings.FieldsFunc(raw, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return "", errors.New("path traversal is forbidden")
		}
	}
	clean := filepath.Clean(raw)
	// Resolve the nearest existing ancestor so file checks can safely report missing files.
	existing := clean
	suffix := []string{}
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) || filepath.Dir(existing) == existing {
			return "", errors.New("media path unavailable")
		}
		suffix = append(suffix, filepath.Base(existing))
		existing = filepath.Dir(existing)
	}
	canonical, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", errors.New("invalid media symlink")
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		canonical = filepath.Join(canonical, suffix[i])
	}
	for _, root := range p.roots {
		relative, err := filepath.Rel(root, canonical)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return canonical, nil
		}
	}
	return "", errors.New("media path is outside allowed roots")
}

func newMediaHandler(policy *mediaPolicy) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, r, 200, map[string]string{"status": "ok", "version": "0.3.1", "mode": "media-only"})
	})
	gate := make(chan struct{}, 1)
	routes := map[string]http.HandlerFunc{
		"/api/media/screenshot": screenshotHandler, "/api/media/mediainfo": mediainfoHandler,
		"/api/file/check": fileCheckHandler, "/api/file/batch-check": batchFileCheckHandler,
		"/api/media/episode-count": episodeCountHandler,
	}
	for path, handler := range routes {
		mux.Handle(path, policy.protect(http.HandlerFunc(handler), gate))
	}
	// No qB forwarding or callback endpoints: the NAS only exports media capabilities.
	mux.Handle("/", policy.protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }), nil))
	return mux
}

func (p *mediaPolicy) protect(next http.Handler, gate chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		digest := sha256.Sum256([]byte(token))
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare(digest[:], p.tokenHash[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "only POST is supported", 405)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMediaBody))
		if err != nil {
			http.Error(w, "request body too large", 413)
			return
		}
		var data map[string]json.RawMessage
		if json.Unmarshal(body, &data) != nil || data == nil {
			http.Error(w, "invalid JSON body", 400)
			return
		}
		fields, required := mediaRequestFields(r.URL.Path)
		if fields == nil {
			http.NotFound(w, r)
			return
		}
		for key := range data {
			if !fields[key] {
				http.Error(w, "unknown request field", 400)
				return
			}
		}
		if _, ok := data[required]; !ok {
			http.Error(w, "media path required", 400)
			return
		}
		if value, ok := data["remote_path"]; ok {
			var path string
			if json.Unmarshal(value, &path) != nil {
				http.Error(w, "invalid media path", 400)
				return
			}
			resolved, err := p.resolve(path)
			if err != nil {
				http.Error(w, "media path denied", 403)
				return
			}
			data["remote_path"], _ = json.Marshal(resolved)
		}
		if value, ok := data["remote_paths"]; ok {
			var paths []string
			if json.Unmarshal(value, &paths) != nil || len(paths) == 0 || len(paths) > 256 {
				http.Error(w, "invalid media paths", 400)
				return
			}
			for i, path := range paths {
				resolved, err := p.resolve(path)
				if err != nil {
					http.Error(w, "media path denied", 403)
					return
				}
				paths[i] = resolved
			}
			data["remote_paths"], _ = json.Marshal(paths)
		} else if _, ok := data["remote_path"]; !ok {
			http.Error(w, "media path required", 400)
			return
		}
		if gate != nil {
			select {
			case gate <- struct{}{}:
				defer func() { <-gate }()
			default:
				http.Error(w, "media agent busy; try again after current request", 429)
				return
			}
		}
		sanitized, _ := json.Marshal(data)
		r.Body = io.NopCloser(bytes.NewReader(sanitized))
		next.ServeHTTP(w, r)
	})
}

// Only exact JSON field names are accepted, preventing encoding/json's case-insensitive
// struct matching from interpreting a path that the boundary validator did not inspect.
func mediaRequestFields(path string) (map[string]bool, string) {
	var names []string
	required := "remote_path"
	switch path {
	case "/api/media/screenshot":
		names = []string{"remote_path", "content_name", "mode", "preview_count", "selected_times", "selected_subtitle_sid"}
	case "/api/media/mediainfo":
		names = []string{"remote_path", "content_name"}
	case "/api/file/check", "/api/media/episode-count":
		names = []string{"remote_path"}
	case "/api/file/batch-check":
		names = []string{"remote_paths"}
		required = "remote_paths"
	default:
		return nil, ""
	}
	fields := make(map[string]bool, len(names))
	for _, name := range names {
		fields[name] = true
	}
	return fields, required
}
