package localquery

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/repository"
)

type scanTorrent struct {
	record     repository.LocalTorrentRecord
	downloader downloaderMeta
}

type directoryScan struct {
	items    int
	missing  map[string]bool
	orphaned []map[string]any
}

type scanIssue struct {
	Path           string `json:"path"`
	DownloaderName string `json:"downloader_name"`
	Status         string `json:"status"`
	Message        string `json:"message"`
}

// Scan 对查询范围逐目录核验；未完成的目录不会进入缺失或孤立计数。
func (s *Service) Scan(ctx context.Context, targetPath string, videoOnly bool) (map[string]any, error) {
	torrents, err := s.repo.ListTorrents(targetPath, videoOnly)
	if err != nil {
		return nil, err
	}
	requestCtx := ctx
	ctx, cancel := context.WithTimeout(requestCtx, 75*time.Second)
	defer cancel()

	downloaders := s.downloaderMap()
	localScopes := map[string][]scanTorrent{}
	remoteScopes := map[remoteScopeKey][]scanTorrent{}
	remoteCount := 0
	scans := scanAccumulator{
		missing: map[string][]map[string]any{}, found: map[string]*syncedItem{}, localFound: map[string]bool{},
		orphaned: []map[string]any{}, issues: []scanIssue{},
	}
	if len(torrents) == 0 && targetPath != "" {
		scans.issues = append(scans.issues, scanIssue{
			Path: targetPath, Status: "unverified", Message: "此路径在当前筛选中没有可核验的任务",
		})
	}
	for _, torrent := range torrents {
		meta, ok := downloaders[torrent.DownloaderID]
		if !ok {
			meta = downloaderMeta{Name: "未知"}
		}
		item := scanTorrent{record: torrent, downloader: meta}
		savePath := normalizePath(torrent.SavePath)
		if !ok {
			scans.issue(savePath, meta.Name, errors.New("找不到该任务的下载器配置"), false)
			continue
		}
		if meta.Remote {
			remoteCount++
			peer := meta.ScanPeer
			if peer == "" {
				peer = meta.ID
			}
			key := remoteScopeKey{peer: peer, directory: strings.TrimRight(savePath, "/")}
			remoteScopes[key] = append(remoteScopes[key], item)
		} else {
			key := filepath.Clean(applyRemoteToLocal(savePath, meta.Mappings))
			localScopes[key] = append(localScopes[key], item)
		}
	}

	if len(localScopes) > 0 {
		references, err := s.repo.ListScanReferences()
		if err != nil {
			return nil, err
		}
		for _, directory := range sortedKeys(localScopes) {
			rows := localScopes[directory]
			displayPath := normalizePath(rows[0].record.SavePath)
			names := localReferences(directory, references, downloaders)
			validNames := true
			for _, row := range rows {
				name := row.record.Name
				if !filepath.IsLocal(name) || name == "." || filepath.Base(name) != name {
					validNames = false
					break
				}
				names[name] = true
			}
			if !validNames {
				scans.issue(displayPath, rows[0].downloader.Name, errors.New("本地任务的文件名无法核验"), false)
				continue
			}
			observed, err := scanLocalDirectory(ctx, directory, displayPath, names)
			if err != nil {
				scans.issue(displayPath, rows[0].downloader.Name, err, false)
				continue
			}
			scans.add(observed)
			for _, row := range rows {
				expected := filepath.Join(directory, row.record.Name)
				scans.record(row, expected, observed.missing[row.record.Name])
			}
		}
	}

	if len(remoteScopes) > 0 {
		peers := newRemoteScanPeers(s.openRemoteScan, s.cfg.Get(), downloaders)
		keys := make([]remoteScopeKey, 0, len(remoteScopes))
		for key := range remoteScopes {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].directory == keys[j].directory {
				return keys[i].peer < keys[j].peer
			}
			return keys[i].directory < keys[j].directory
		})
		for _, key := range keys {
			rows := remoteScopes[key]
			observed, expected, err := peers.scan(ctx, key, rows)
			if err != nil {
				scans.issue(key.directory, rows[0].downloader.Name, err, true)
				continue
			}
			scans.add(observed)
			for index, row := range rows {
				name := expected[index]
				scans.record(row, path.Join(key.directory, name), observed.missing[name])
			}
		}
	}

	missing := scans.missingFiles()
	syncedRows := syncedItems(scans.found)
	orderByNameAndPath(missing)
	orderByNameAndPath(scans.orphaned)
	orderByNameAndPath(syncedRows)
	status := "complete"
	if len(scans.issues) > 0 {
		status = "partial"
		if scans.checked == 0 {
			status = "unverified"
		}
	}
	result := map[string]any{
		"scan_status": status,
		"scan_issues": scans.issues,
		"scanned_at":  time.Now().UTC(),
		"scan_summary": map[string]any{
			"total_torrents":        len(torrents),
			"total_local_items":     scans.items,
			"missing_count":         len(missing),
			"orphaned_count":        len(scans.orphaned),
			"synced_count":          len(syncedRows),
			"remote_torrents_count": remoteCount,
			"skipped_remote":        scans.skippedRemote,
		},
		"missing_files":   missing,
		"orphaned_files":  scans.orphaned,
		"synced_torrents": syncedRows,
	}
	if requestCtx.Err() == nil {
		_ = s.saveScanCache(result)
	}
	return result, nil
}

// localReferences 保留同一物理目录下被任意任务引用的顶层名称，包括子目录引用。
func localReferences(directory string, records []repository.LocalTorrentRecord, downloaders map[string]downloaderMeta) map[string]bool {
	names := map[string]bool{}
	for _, record := range records {
		meta := downloaders[record.DownloaderID]
		if meta.Remote || record.Name == "" {
			continue
		}
		owner := applyRemoteToLocal(record.SavePath, meta.Mappings)
		rel, err := filepath.Rel(directory, filepath.Join(owner, record.Name))
		if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		name, _, _ := strings.Cut(rel, string(filepath.Separator))
		names[name] = true
	}
	return names
}

// scanLocalDirectory 核验原有的顶层范围，读取失败时丢弃该目录的部分结果。
func scanLocalDirectory(ctx context.Context, directory, displayPath string, references map[string]bool) (directoryScan, error) {
	if ctx.Err() != nil {
		return directoryScan{}, errScanIncomplete
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return directoryScan{}, errors.New("无法读取本地目录")
	}
	result := directoryScan{items: len(entries), missing: map[string]bool{}}
	listed := make(map[string]os.DirEntry, len(entries))
	for _, entry := range entries {
		listed[entry.Name()] = entry
	}
	var aliases []os.FileInfo
	for name := range references {
		if ctx.Err() != nil {
			return directoryScan{}, errScanIncomplete
		}
		entry := listed[name]
		if entry != nil && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		info, err := os.Stat(filepath.Join(directory, name))
		if os.IsNotExist(err) {
			result.missing[name] = true
		} else if err != nil {
			return directoryScan{}, errors.New("本地文件无法核验")
		} else {
			// 大小写别名或符号链接不应让实际指向的已引用文件成为孤立项。
			aliases = append(aliases, info)
		}
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return directoryScan{}, errScanIncomplete
		}
		name := entry.Name()
		if references[name] || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return directoryScan{}, errors.New("本地目录内容发生变化或不可读")
		}
		protected := false
		for _, alias := range aliases {
			if os.SameFile(alias, info) {
				protected = true
				break
			}
		}
		if protected {
			continue
		}
		result.orphaned = append(result.orphaned, map[string]any{
			"name": name, "path": displayPath, "full_path": filepath.Join(directory, name), "size": info.Size(),
		})
	}
	return result, nil
}

type scanAccumulator struct {
	items, checked int
	skippedRemote  bool
	missing        map[string][]map[string]any
	found          map[string]*syncedItem
	localFound     map[string]bool
	orphaned       []map[string]any
	issues         []scanIssue
}

func (sc *scanAccumulator) add(result directoryScan) {
	sc.items += result.items
	sc.checked++
	sc.orphaned = append(sc.orphaned, result.orphaned...)
}

func (sc *scanAccumulator) issue(path, name string, err error, remote bool) {
	status := "failed"
	if errors.Is(err, errScanIncomplete) {
		status = "unverified"
	}
	sc.issues = append(sc.issues, scanIssue{Path: path, DownloaderName: name, Status: status, Message: err.Error()})
	sc.skippedRemote = sc.skippedRemote || remote
}

func (sc *scanAccumulator) record(row scanTorrent, expected string, missing bool) {
	torrent := row.record
	key := buildNameSizeGroupKey(torrent.Name, torrent.Size)
	if !missing {
		addSyncedItem(sc.found, row, normalizePath(torrent.SavePath))
		if !row.downloader.Remote {
			sc.localFound[key] = true
		}
		return
	}
	if row.downloader.Remote {
		key = "remote\x00" + row.downloader.ScanPeer + "\x00" + expected
	}
	sc.missing[key] = append(sc.missing[key], map[string]any{
		"name": torrent.Name, "save_path": normalizePath(torrent.SavePath), "expected_path": expected,
		"size": torrent.Size, "downloader_name": row.downloader.Name,
	})
}

func (sc *scanAccumulator) missingFiles() []map[string]any {
	result := make([]map[string]any, 0, len(sc.missing))
	for key, candidates := range sc.missing {
		if sc.localFound[key] {
			continue
		}
		orderByNameAndPath(candidates)
		names := map[string]struct{}{}
		for _, candidate := range candidates {
			names[stringFromAny(candidate["downloader_name"])] = struct{}{}
		}
		row := candidates[0]
		row["downloader_name"] = strings.Join(sortedKeys(names), ", ")
		row["torrents_count"] = len(candidates)
		result = append(result, row)
	}
	return result
}

func addSyncedItem(items map[string]*syncedItem, row scanTorrent, savePath string) {
	torrent := row.record
	key := buildNameSizeGroupKey(torrent.Name, torrent.Size)
	item := items[key]
	if item == nil {
		item = &syncedItem{Name: torrent.Name, Path: savePath, Size: torrent.Size, DownloaderNames: map[string]struct{}{}}
		items[key] = item
	}
	item.Count++
	item.DownloaderNames[row.downloader.Name] = struct{}{}
	if item.Path == "" || (savePath != "" && savePath < item.Path) {
		item.Path = savePath
	}
}

func syncedItems(index map[string]*syncedItem) []map[string]any {
	items := make([]map[string]any, 0, len(index))
	for _, item := range index {
		items = append(items, map[string]any{
			"name": item.Name, "path": item.Path, "size": item.Size,
			"torrents_count": item.Count, "downloader_names": sortedKeys(item.DownloaderNames),
		})
	}
	return items
}

func sortedKeys[T any](set map[string]T) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

func applyRemoteToLocal(remotePath string, mappings []pathMapping) string {
	normalized := normalizePath(remotePath)
	for _, mapping := range mappings {
		if normalized == mapping.Remote || stringsHasPrefix(normalized, mapping.Remote+"/") {
			return mapping.Local + stringsTrimPrefix(normalized, mapping.Remote)
		}
	}
	return normalized
}

func buildNameSizeGroupKey(name string, size int64) string {
	return strings.TrimSpace(name) + "\x00" + strconv.FormatInt(size, 10)
}
