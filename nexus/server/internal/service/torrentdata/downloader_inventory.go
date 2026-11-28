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

var inventoryStateLabels = map[string]string{
	"seeding":     "做种中",
	"downloading": "下载中",
	"paused":      "已暂停",
	"checking":    "校验中",
	"queued":      "排队中",
	"error":       "异常",
	"other":       "其他",
}
var inventoryDimensions = map[string]string{
	"category": "未分类",
	"tag":      "无标签",
	"tracker":  "无 Tracker",
	"state":    "其他",
	"site":     "未识别站点",
	"path":     "未知路径",
}

// InventoryTorrent 是统计面板允许展示的种子字段，仅在展开指定分组后返回。
type InventoryTorrent struct {
	Name     string   `json:"name"`
	State    string   `json:"state"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
	Trackers []string `json:"trackers"`
	Site     string   `json:"site"`
	Path     string   `json:"path"`
	Progress float64  `json:"progress"`
	Size     int64    `json:"size"`
}

// InventoryFilters 表示同时匹配的分类、标签、Tracker 键及种子状态。空值不限制。
type InventoryFilters struct {
	Category string
	Tag      string
	Tracker  string
	Status   string
}

// InventoryOption 是筛选项的显示名称和对应的不透明查询键。
type InventoryOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
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
	RefreshedAt int64                        `json:"refreshed_at"`
	SourceTotal int                          `json:"source_total"`
	Total       int                          `json:"total"`
	Counts      map[string]int               `json:"counts"`
	Groups      []InventoryGroup             `json:"groups"`
	Options     map[string][]InventoryOption `json:"options"`
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

// Summary 返回匹配筛选条件的分组计数和完整筛选选项。首次访问、快照过期或刷新时读取下载器。
func (s *DownloaderInventory) Summary(ctx context.Context, id, dimension string, filters InventoryFilters, refresh bool) (InventorySummary, error) {
	if inventoryDimensions[dimension] == "" || !filters.valid() {
		return InventorySummary{}, ErrInventoryQuery
	}
	snapshot, err := s.getSnapshot(ctx, id, refresh, false)
	if err != nil {
		return InventorySummary{}, err
	}
	return snapshot.summary(dimension, filters), nil
}

// Details 从已有快照分页读取指定分组，同时应用汇总使用的筛选条件。快照失效时提示刷新。
func (s *DownloaderInventory) Details(ctx context.Context, id, dimension, group string, filters InventoryFilters, page, pageSize int) (InventoryDetails, error) {
	if inventoryDimensions[dimension] == "" || len(group) != 24 || !filters.valid() ||
		page < 1 || page > inventoryTorrentLimit || pageSize < 1 || pageSize > 50 {
		return InventoryDetails{}, ErrInventoryQuery
	}
	snapshot, err := s.getSnapshot(ctx, id, false, true)
	if err != nil {
		return InventoryDetails{}, err
	}
	return snapshot.details(dimension, group, filters, page, pageSize), nil
}

func (filters InventoryFilters) valid() bool {
	for _, key := range [...]string{filters.Category, filters.Tag, filters.Tracker} {
		if key != "" && (len(key) != 24 || strings.Trim(key, "0123456789abcdef") != "") {
			return false
		}
	}
	return filters.Status == "" || inventoryStateLabels[filters.Status] != ""
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
		trackers := inventoryTrackerHosts(torrent.Trackers)
		if site == "" && len(trackers) > 0 {
			site = trackers[0]
		}
		state := torrent.InventoryState
		if inventoryStateLabels[state] == "" {
			state = "other"
		}
		items = append(items, InventoryTorrent{
			Name: torrent.Name, State: state, Category: torrent.Category, Tags: append([]string{}, torrent.Tags...),
			Trackers: trackers, Site: site, Path: torrent.SavePath, Progress: torrent.Progress / 100, Size: torrent.Size,
		})
	}
	// 名称相同的任务维持下载器顺序，分页在同一快照中可重现。
	slices.SortStableFunc(items, func(a, b InventoryTorrent) int { return strings.Compare(a.Name, b.Name) })
	return inventorySnapshot{refreshedAt: time.Now(), torrents: items}, nil
}

// inventoryTrackerHosts 将同一任务的 Tracker 按主机去重，保留下载器给出的顺序。
func inventoryTrackerHosts(trackers []string) []string {
	hosts := make([]string, 0, len(trackers))
	for _, tracker := range trackers {
		host := inventoryTrackerHost(tracker)
		if host != "" && !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// inventoryTrackerHost 从 Tracker 地址中提取主机，丢弃端口、凭据、路径与查询参数。
func inventoryTrackerHost(tracker string) string {
	value := strings.TrimSpace(tracker)
	if ip := net.ParseIP(value); ip != nil {
		return ip.String()
	}
	if !strings.Contains(value, "://") {
		value = "//" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	if !strings.Contains(host, ".") {
		return ""
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' ||
			strings.Trim(label, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			return ""
		}
	}
	return host
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
	case "tracker":
		if len(torrent.Trackers) > 0 {
			return torrent.Trackers
		}
	case "state":
		return []string{torrent.State}
	case "site":
		return []string{torrent.Site}
	case "path":
		return []string{torrent.Path}
	}
	return []string{""}
}

func (torrent InventoryTorrent) matchesGroup(dimension, key string) bool {
	for _, value := range torrent.groupValues(dimension) {
		if inventoryGroupKey(value) == key {
			return true
		}
	}
	return false
}

func (filters InventoryFilters) matches(torrent InventoryTorrent) bool {
	return (filters.Status == "" || torrent.State == filters.Status) &&
		(filters.Category == "" || torrent.matchesGroup("category", filters.Category)) &&
		(filters.Tag == "" || torrent.matchesGroup("tag", filters.Tag)) &&
		(filters.Tracker == "" || torrent.matchesGroup("tracker", filters.Tracker))
}

func inventoryGroupLabel(dimension, value string) string {
	if dimension == "state" && inventoryStateLabels[value] != "" {
		return inventoryStateLabels[value]
	}
	if value == "" {
		return inventoryDimensions[dimension]
	}
	return value
}

func (snapshot inventorySnapshot) summary(dimension string, filters InventoryFilters) InventorySummary {
	byValue := make(map[string]*InventoryGroup)
	counts := make(map[string]int)
	optionValues := map[string]map[string]struct{}{
		"category": {},
		"tag":      {},
		"tracker":  {},
	}
	total := 0
	for _, torrent := range snapshot.torrents {
		for optionDimension, values := range optionValues {
			for _, value := range torrent.groupValues(optionDimension) {
				values[value] = struct{}{}
			}
		}
		if !filters.matches(torrent) {
			continue
		}
		total++
		counts[torrent.State]++
		for _, value := range torrent.groupValues(dimension) {
			group := byValue[value]
			if group == nil {
				group = &InventoryGroup{Key: inventoryGroupKey(value), Label: inventoryGroupLabel(dimension, value), Counts: make(map[string]int)}
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
	options := make(map[string][]InventoryOption, len(optionValues))
	for optionDimension, values := range optionValues {
		choices := make([]InventoryOption, 0, len(values))
		for value := range values {
			choices = append(choices, InventoryOption{Key: inventoryGroupKey(value), Label: inventoryGroupLabel(optionDimension, value)})
		}
		slices.SortFunc(choices, func(a, b InventoryOption) int {
			if order := strings.Compare(a.Label, b.Label); order != 0 {
				return order
			}
			return strings.Compare(a.Key, b.Key)
		})
		options[optionDimension] = choices
	}
	return InventorySummary{
		RefreshedAt: snapshot.refreshedAt.Unix(), SourceTotal: len(snapshot.torrents),
		Total: total, Counts: counts, Groups: groups, Options: options,
	}
}

func (snapshot inventorySnapshot) details(dimension, group string, filters InventoryFilters, page, pageSize int) InventoryDetails {
	rows := make([]InventoryTorrent, 0, pageSize)
	offset := (page - 1) * pageSize
	total := 0
	for _, torrent := range snapshot.torrents {
		if !filters.matches(torrent) || !torrent.matchesGroup(dimension, group) {
			continue
		}
		if total >= offset && len(rows) < pageSize {
			rows = append(rows, torrent)
		}
		total++
	}
	return InventoryDetails{RefreshedAt: snapshot.refreshedAt.Unix(), Total: total, Page: page, PageSize: pageSize, Rows: rows}
}
