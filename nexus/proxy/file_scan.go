package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxFileScanEntries     = 20_000
	maxFileScanResultBytes = 2 << 20
)

type fileScanRequest struct {
	RemotePath string   `json:"remote_path"`
	Expected   []string `json:"expected"`
	Referenced []string `json:"referenced"`
}

type fileScanEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type fileScanResult struct {
	Success    bool            `json:"success"`
	Complete   bool            `json:"complete"`
	TotalItems int             `json:"total_items"`
	Missing    []string        `json:"missing"`
	Orphaned   []fileScanEntry `json:"orphaned"`
}

// fileScanHandler 只读取受信任媒体目录的一级文件元数据。未完成的扫描不会返回部分结果。
func (p *mediaPolicy) fileScanHandler(w http.ResponseWriter, r *http.Request) {
	var request fileScanRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Expected == nil || request.Referenced == nil {
		http.Error(w, "invalid file scan request", http.StatusBadRequest)
		return
	}
	referenced := make(map[string]struct{}, len(request.Referenced))
	for _, name := range request.Referenced {
		if !isFileScanBasename(name) {
			http.Error(w, "invalid referenced file name", http.StatusBadRequest)
			return
		}
		referenced[name] = struct{}{}
	}
	expected := make(map[string]struct{}, len(request.Expected))
	for _, name := range request.Expected {
		if _, ok := referenced[name]; !ok {
			http.Error(w, "expected file name must be referenced", http.StatusBadRequest)
			return
		}
		expected[name] = struct{}{}
	}

	directory, err := p.openFileScanDirectory(request.RemotePath)
	if err != nil {
		http.Error(w, "file scan directory unavailable", http.StatusUnprocessableEntity)
		return
	}
	defer directory.Close()

	result, err := scanFileDirectory(r.Context(), directory, expected, referenced)
	if err != nil {
		http.Error(w, "file scan incomplete", http.StatusUnprocessableEntity)
		return
	}
	payload, err := json.Marshal(result)
	if err != nil || len(payload) > maxFileScanResultBytes {
		http.Error(w, "file scan result too large", http.StatusUnprocessableEntity)
		return
	}
	if r.Context().Err() != nil {
		http.Error(w, "file scan incomplete", http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func isFileScanBasename(name string) bool {
	return name != "." && filepath.IsLocal(name) && utf8.ValidString(name) &&
		len(name) <= 255 && !strings.ContainsAny(name, "/\\\x00\r\n")
}

// openFileScanDirectory 将已验证的目录打开在白名单根下，防止后续文件访问越出扫描范围。
func (p *mediaPolicy) openFileScanDirectory(directory string) (*os.Root, error) {
	for _, allowed := range p.roots {
		relative, err := filepath.Rel(allowed.path, directory)
		if err != nil || !filepath.IsLocal(relative) {
			continue
		}
		root, err := os.OpenRoot(allowed.path)
		if err != nil {
			return nil, err
		}
		info, err := root.Stat(".")
		if err != nil || !os.SameFile(allowed.identity, info) {
			_ = root.Close()
			return nil, errors.New("configured file scan root changed")
		}
		scope, err := root.OpenRoot(relative)
		_ = root.Close()
		return scope, err
	}
	return nil, errors.New("file scan directory not allowed")
}

func scanFileDirectory(ctx context.Context, directory *os.Root, expected, referenced map[string]struct{}) (fileScanResult, error) {
	if err := ctx.Err(); err != nil {
		return fileScanResult{}, err
	}
	list, err := directory.Open(".")
	if err != nil {
		return fileScanResult{}, err
	}
	defer list.Close()
	before, err := list.Stat()
	if err != nil {
		return fileScanResult{}, err
	}
	entries, err := list.ReadDir(maxFileScanEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return fileScanResult{}, err
	}
	if len(entries) > maxFileScanEntries {
		return fileScanResult{}, errors.New("file scan entry limit exceeded")
	}
	result := fileScanResult{
		Success: true, Complete: true, TotalItems: len(entries),
		Missing: []string{}, Orphaned: []fileScanEntry{},
	}
	for name := range expected {
		if err := ctx.Err(); err != nil {
			return fileScanResult{}, err
		}
		info, err := directory.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			result.Missing = append(result.Missing, name)
			continue
		}
		if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fileScanResult{}, errors.New("cannot verify expected file type")
		}
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return fileScanResult{}, err
		}
		if _, ok := referenced[entry.Name()]; ok {
			if entry.Type()&os.ModeSymlink != 0 {
				return fileScanResult{}, errors.New("cannot verify referenced file link")
			}
			continue
		}
		if !entry.Type().IsRegular() {
			continue
		}
		if !isFileScanBasename(entry.Name()) {
			return fileScanResult{}, errors.New("cannot represent directory entry")
		}
		info, err := directory.Lstat(entry.Name())
		if err != nil || !info.Mode().IsRegular() {
			return fileScanResult{}, errors.New("cannot verify directory entry")
		}
		result.Orphaned = append(result.Orphaned, fileScanEntry{Name: entry.Name(), Size: info.Size()})
	}
	after, err := list.Stat()
	if err != nil || !before.ModTime().Equal(after.ModTime()) {
		return fileScanResult{}, errors.New("directory changed while scanning")
	}
	if err := ctx.Err(); err != nil {
		return fileScanResult{}, err
	}
	sort.Strings(result.Missing)
	sort.Slice(result.Orphaned, func(i, j int) bool { return result.Orphaned[i].Name < result.Orphaned[j].Name })
	return result, nil
}
