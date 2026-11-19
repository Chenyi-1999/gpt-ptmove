package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	maxManifestBytes    = 64 << 10
	maxDirectoryEntries = 128
	maxBlocks           = 16
)

var pluginID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

type Block struct {
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Text   string `json:"text,omitempty"`
	Target string `json:"target,omitempty"`
	Metric string `json:"metric,omitempty"`
}

type Manifest struct {
	SchemaVersion int     `json:"schema_version"`
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Version       string  `json:"version"`
	Description   string  `json:"description,omitempty"`
	Blocks        []Block `json:"blocks"`
}

type Issue struct {
	File    string `json:"file"`
	Message string `json:"message"`
}

type Snapshot struct {
	Items  []Manifest `json:"items"`
	Issues []Issue    `json:"issues"`
}

type Catalog struct {
	root     string
	mu       sync.Mutex
	snapshot Snapshot
}

func New(dataDir string) *Catalog {
	return &Catalog{
		root:     filepath.Join(dataDir, "plugins"),
		snapshot: Snapshot{Items: []Manifest{}, Issues: []Issue{}},
	}
}

func (c *Catalog) List() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshot
}

// Reload atomically replaces the index after scanning the operator's local folder.
func (c *Catalog) Reload() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := Snapshot{Items: []Manifest{}, Issues: []Issue{}}
	entries, err := manifestEntries(c.root)
	if err != nil {
		return c.snapshot, err
	}
	seen := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		manifest, err := readManifest(filepath.Join(c.root, name))
		if err == nil && manifest.ID != strings.TrimSuffix(name, ".json") {
			err = errors.New("文件名须与插件 ID 相同")
		}
		if err == nil && seen[manifest.ID] {
			err = errors.New("插件 ID 重复")
		}
		if err != nil {
			next.Issues = append(next.Issues, Issue{File: name, Message: err.Error()})
			continue
		}
		seen[manifest.ID] = true
		next.Items = append(next.Items, manifest)
	}
	c.snapshot = next
	return next, nil
}

func manifestEntries(root string) ([]os.DirEntry, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() {
		return nil, errors.New("插件目录不可读或不是普通目录")
	}
	dir, err := os.Open(root)
	if err != nil {
		return nil, errors.New("无法读取插件目录")
	}
	defer dir.Close()
	current, err := dir.Stat()
	if err != nil || !os.SameFile(info, current) {
		return nil, errors.New("插件目录在读取时发生了变化")
	}
	entries, err := dir.ReadDir(maxDirectoryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.New("无法读取插件目录")
	}
	if len(entries) > maxDirectoryEntries {
		return nil, errors.New("插件目录的文件数量超出上限")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func readManifest(path string) (Manifest, error) {
	var manifest Manifest
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxManifestBytes {
		return manifest, errors.New("清单须为不超过 64 KiB 的普通 JSON 文件")
	}
	f, err := os.Open(path)
	if err != nil {
		return manifest, errors.New("无法读取清单")
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !os.SameFile(info, current) {
		return manifest, errors.New("清单在读取时发生了变化")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil || len(data) > maxManifestBytes {
		return manifest, errors.New("清单不可读或超出 64 KiB")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(data) {
		return manifest, errors.New("清单须使用 UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(new(json.RawMessage)) != io.EOF {
		return manifest, errors.New("JSON 格式错误或包含未知字段")
	}
	return manifest, validate(manifest)
}

func validate(m Manifest) error {
	if m.SchemaVersion != 1 || !pluginID.MatchString(m.ID) || !validText(m.Name, 64, true) ||
		!validText(m.Version, 24, true) || !validText(m.Description, 220, false) ||
		len(m.Blocks) < 1 || len(m.Blocks) > maxBlocks {
		return errors.New("清单的版本、ID、名称或内容不符合格式")
	}
	for _, b := range m.Blocks {
		if !validText(b.Title, 64, true) || !validText(b.Text, 360, false) {
			return errors.New("内容块的标题或文字不符合格式")
		}
		switch b.Kind {
		case "text":
			if b.Text != "" && b.Target == "" && b.Metric == "" {
				continue
			}
		case "link":
			if allowedTarget(b.Target) && b.Metric == "" {
				continue
			}
		case "metric":
			if KnownMetric(b.Metric) && b.Target == "" {
				continue
			}
		}
		return errors.New("内容块类型、入口或数据指标不受支持")
	}
	return nil
}

func validText(s string, limit int, required bool) bool {
	return (!required || strings.TrimSpace(s) != "") && utf8.RuneCountInString(s) <= limit &&
		strings.IndexFunc(s, unicode.IsControl) < 0
}

func allowedTarget(target string) bool {
	switch target {
	case "/torrents", "/data", "/seed-maintenance", "/publish-logs", "/sites",
		"/settings/downloader", "/settings/api", "/settings/site-profiles", "/settings/automation":
		return true
	}
	return false
}

func KnownMetric(metric string) bool {
	switch metric {
	case "visible_torrents", "transfer_drafts", "configured_sites":
		return true
	}
	return false
}
