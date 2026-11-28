package torrentdata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

const inventorySnapshotTTL = 5 * time.Minute
const inventorySnapshotLimit = 6
const inventoryTorrentLimit = 25000

var (
	ErrInventoryDownloader = errors.New("所选下载器不可用，请检查其启用状态和配置")
	ErrInventoryQuery      = errors.New("下载器统计筛选无效")
	ErrInventoryExpired    = errors.New("本次统计已失效，请刷新统计")
	ErrInventoryLimit      = errors.New("该下载器的任务数量超过统计上限")
	ErrInventoryRead       = errors.New("读取下载器失败，请检查连接后重试")
)

var inventoryStates = []string{"seeding", "downloading", "paused", "checking", "queued", "error", "other"}
var inventoryDimensions = map[string]string{
	"category": "未分类",
	"tag":      "无标签",
	"site":     "未识别站点",
	"path":     "未知路径",
}

// InventoryTorrent 是统计面板允许展示的种子字段，仅在展开指定分组后返回。
type InventoryTorrent struct {
	Name     string   `json:"name"`
	State    string   `json:"state"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
	Site     string   `json:"site"`
	Path     string   `json:"path"`
	Progress float64  `json:"progress"`
	Size     int64    `json:"size"`
}

// InventoryGroup 包含该维度分组的总数和各状态数。
type InventoryGroup struct {
	Key    string         `json:"key"`
	Label  string         `json:"label"`
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts"`
}

// InventorySummary 是一次下载器统计的折叠结果。
type InventorySummary struct {
	RefreshedAt int64            `json:"refreshed_at"`
	Total       int              `json:"total"`
	Counts      map[string]int   `json:"counts"`
	Groups      []InventoryGroup `json:"groups"`
}

// InventoryDetails 是同一快照上的种子明细页。
type InventoryDetails struct {
	RefreshedAt int64              `json:"refreshed_at"`
	Total       int                `json:"total"`
	Page        int                `json:"page"`
	PageSize    int                `json:"page_size"`
	Rows        []InventoryTorrent `json:"rows"`
}

type inventorySnapshot struct {
	refreshedAt time.Time
	torrents    []InventoryTorrent
}

type inventoryLoad struct {
	done   chan struct{}
	result inventorySnapshot
	err    error
}

// DownloaderInventory 数量统计使用只读下载器快照，不写种子索引。
type DownloaderInventory struct {
	cfg   *config.Manager
	repo  *repository.TorrentDataRepository
	mutex sync.Mutex
	cache map[[32]byte]inventorySnapshot
	loads map[[32]byte]*inventoryLoad
}

// NewDownloaderInventory 创建按下载器隔离的分页统计服务。
func NewDownloaderInventory(cfg *config.Manager, repo *repository.TorrentDataRepository) *DownloaderInventory {
	return &DownloaderInventory{
		cfg: cfg, repo: repo,
		cache: make(map[[32]byte]inventorySnapshot),
		loads: make(map[[32]byte]*inventoryLoad),
	}
}

// Summary 按维度读取计数。首次访问、快照过期或刷新时读取下载器，连接失败返回错误。
func (s *DownloaderInventory) Summary(ctx context.Context, id, dimension string, refresh bool) (InventorySummary, error) {
	if inventoryDimensions[dimension] == "" {
		return InventorySummary{}, ErrInventoryQuery
	}
	snapshot, err := s.getSnapshot(ctx, id, refresh, false)
	if err != nil {
		return InventorySummary{}, err
	}
	return snapshot.summary(dimension), nil
}

// Details 从已展示的统计快照分页读取指定分组，支持按种子状态筛选。
func (s *DownloaderInventory) Details(ctx context.Context, id, dimension, group, state string, page, pageSize int) (InventoryDetails, error) {
	if inventoryDimensions[dimension] == "" || len(group) != 24 ||
		(state != "" && !slices.Contains(inventoryStates, state)) ||
		page < 1 || page > inventoryTorrentLimit || pageSize < 1 || pageSize > 50 {
		return InventoryDetails{}, ErrInventoryQuery
	}
	snapshot, err := s.getSnapshot(ctx, id, false, true)
	if err != nil {
		return InventoryDetails{}, err
	}
	return snapshot.details(dimension, group, state, page, pageSize), nil
}

func (s *DownloaderInventory) getSnapshot(ctx context.Context, id string, refresh, details bool) (inventorySnapshot, error) {
	downloader, err := downloaderclient.FromConfig(s.cfg.Get(), id)
	if err != nil {
		return inventorySnapshot{}, ErrInventoryDownloader
	}

	// 连接设置一旦改变，旧快照即不再用于该下载器。
	key := sha256.Sum256([]byte(fmt.Sprintf("%q|%q|%q|%q|%q|%q|%t|%d|%q",
		downloader.ID, downloader.Type, downloader.Host, downloader.Username, downloader.Password,
		downloader.APIKey, downloader.UseProxy, downloader.ProxyPort, downloader.ProxyToken)))
	s.mutex.Lock()
	current, exists := s.cache[key]
	if exists && !refresh && time.Since(current.refreshedAt) < inventorySnapshotTTL {
		s.mutex.Unlock()
		return current, nil
	}
	if details {
		s.mutex.Unlock()
		return inventorySnapshot{}, ErrInventoryExpired
	}
	load := s.loads[key]
	if load == nil {
		if len(s.loads) >= inventorySnapshotLimit {
			s.mutex.Unlock()
			return inventorySnapshot{}, ErrInventoryRead
		}
		load = &inventoryLoad{done: make(chan struct{})}
		s.loads[key] = load
		go s.loadSnapshot(downloader, key, load)
	}
	s.mutex.Unlock()

	select {
	case <-ctx.Done():
		return inventorySnapshot{}, ctx.Err()
	case <-load.done:
		return load.result, load.err
	}
}

func (s *DownloaderInventory) loadSnapshot(downloader downloaderclient.Downloader, key [32]byte, load *inventoryLoad) {
	load.result, load.err = s.fetchSnapshot(downloader)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if load.err == nil {
		if _, exists := s.cache[key]; !exists && len(s.cache) >= inventorySnapshotLimit {
			var oldestKey [32]byte
			var oldest time.Time
			for candidate, snapshot := range s.cache {
				if oldest.IsZero() || snapshot.refreshedAt.Before(oldest) {
					oldestKey, oldest = candidate, snapshot.refreshedAt
				}
			}
			delete(s.cache, oldestKey)
		}
		s.cache[key] = load.result
	}
	delete(s.loads, key)
	close(load.done)
}

func (s *DownloaderInventory) fetchSnapshot(downloader downloaderclient.Downloader) (inventorySnapshot, error) {
	torrents, err := downloader.FetchInventoryTorrents()
	if err != nil {
		return inventorySnapshot{}, ErrInventoryRead
	}
	if len(torrents) > inventoryTorrentLimit {
		return inventorySnapshot{}, ErrInventoryLimit
	}
	sites, err := s.repo.ListSiteIdentities()
	if err != nil {
		return inventorySnapshot{}, errors.New("读取站点配置失败，请稍后重试")
	}
	matcher := newRefreshSiteMatcher(sites)
	items := make([]InventoryTorrent, 0, len(torrents))
	for _, torrent := range torrents {
		site := matcher.Match(torrent.Trackers, "", torrent.Comment)
		if site == "" {
			for _, tracker := range torrent.Trackers {
				if host := inventoryTrackerHost(tracker); host != "" {
					site = host
					break
				}
			}
		}
		state := torrent.InventoryState
		if !slices.Contains(inventoryStates, state) {
			state = "other"
		}
		items = append(items, InventoryTorrent{
			Name: torrent.Name, State: state, Category: torrent.Category, Tags: append([]string{}, torrent.Tags...),
			Site: site, Path: torrent.SavePath, Progress: torrent.Progress / 100, Size: torrent.Size,
		})
	}
	// 名称相同的任务维持下载器顺序，分页在同一快照中可重现。
	slices.SortStableFunc(items, func(a, b InventoryTorrent) int { return strings.Compare(a.Name, b.Name) })
	return inventorySnapshot{refreshedAt: time.Now(), torrents: items}, nil
}

func inventoryTrackerHost(tracker string) string {
	value := strings.TrimSpace(tracker)
	if !strings.Contains(value, "://") {
		value = "//" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if strings.Contains(host, ".") || net.ParseIP(host) != nil {
		return host
	}
	return ""
}

func inventoryGroupKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}

func (torrent InventoryTorrent) groupValues(dimension string) []string {
	switch dimension {
	case "category":
		return []string{torrent.Category}
	case "tag":
		if len(torrent.Tags) > 0 {
			return torrent.Tags
		}
	case "site":
		return []string{torrent.Site}
	case "path":
		return []string{torrent.Path}
	}
	return []string{""}
}

func (snapshot inventorySnapshot) summary(dimension string) InventorySummary {
	byValue := make(map[string]*InventoryGroup)
	counts := make(map[string]int)
	for _, torrent := range snapshot.torrents {
		counts[torrent.State]++
		for _, value := range torrent.groupValues(dimension) {
			group := byValue[value]
			if group == nil {
				label := value
				if label == "" {
					label = inventoryDimensions[dimension]
				}
				group = &InventoryGroup{Key: inventoryGroupKey(value), Label: label, Counts: make(map[string]int)}
				byValue[value] = group
			}
			group.Total++
			group.Counts[torrent.State]++
		}
	}
	groups := make([]InventoryGroup, 0, len(byValue))
	for _, group := range byValue {
		groups = append(groups, *group)
	}
	slices.SortFunc(groups, func(a, b InventoryGroup) int {
		if a.Total != b.Total {
			return b.Total - a.Total
		}
		if order := strings.Compare(a.Label, b.Label); order != 0 {
			return order
		}
		return strings.Compare(a.Key, b.Key)
	})
	return InventorySummary{RefreshedAt: snapshot.refreshedAt.Unix(), Total: len(snapshot.torrents), Counts: counts, Groups: groups}
}

func (snapshot inventorySnapshot) details(dimension, group, state string, page, pageSize int) InventoryDetails {
	rows := make([]InventoryTorrent, 0, pageSize)
	offset := (page - 1) * pageSize
	total := 0
	for _, torrent := range snapshot.torrents {
		if state != "" && torrent.State != state {
			continue
		}
		for _, value := range torrent.groupValues(dimension) {
			if inventoryGroupKey(value) != group {
				continue
			}
			if total >= offset && len(rows) < pageSize {
				rows = append(rows, torrent)
			}
			total++
			break
		}
	}
	return InventoryDetails{RefreshedAt: snapshot.refreshedAt.Unix(), Total: total, Page: page, PageSize: pageSize, Rows: rows}
}
