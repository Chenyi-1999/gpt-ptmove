package migrationflow

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/acquire/tjupt"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

const tjuptTag = "starpt-tjupt"

var (
	// ErrTjuptDownloader 表示请求未指向一个已启用的 qB 下载器。
	ErrTjuptDownloader = errors.New("请选择已启用的飞牛 qB 下载器")
	// ErrTjuptRetireDenied 表示当前任务的退种条件无法确认。
	ErrTjuptRetireDenied = errors.New("无法确认当前任务已达标或免考，请保留做种并刷新状态")
	// ErrTjuptRetireDisabled 表示飞牛暂停所需的独立签名未启用。
	ErrTjuptRetireDisabled = errors.New("北洋园退种签名尚未配置")
)

type tjuptSite interface {
	Preview(context.Context, string, int) ([]tjupt.Candidate, error)
	Status(context.Context, string, []string) (map[string]tjupt.Assessment, error)
}

// TjuptRSSService 用北洋园站点信息预览候选，并校验飞牛 qB 的手动退种。
type TjuptRSSService struct {
	cfg            *config.Manager
	site           tjuptSite
	retireKey      []byte
	authorizedHNR  []byte
	trackerAccount string
}

// TjuptDownloader 是公开给插件的下载器选择项。
type TjuptDownloader struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TjuptTorrentStatus 是标签内任务与站点考核的匹配结果。
type TjuptTorrentStatus struct {
	Hash       string           `json:"hash"`
	Name       string           `json:"name"`
	State      string           `json:"state"`
	Progress   *float64         `json:"progress"`
	Assessment tjupt.Assessment `json:"assessment"`
}

// NewTjuptRSSService 接入现有下载器配置。初始化不访问站点或下载器。
func NewTjuptRSSService(cfg *config.Manager) *TjuptRSSService {
	key := loadTjuptSecret("PTMOVE_TJUPT_RETIRE_KEY", 32)
	token := loadTjuptSecret("PTMOVE_TJUPT_HNR_TOKEN", 16)
	passkey := loadTjuptSecret("PTMOVE_TJUPT_TRACKER_PASSKEY", 16)
	service := &TjuptRSSService{cfg: cfg, site: tjupt.New(), retireKey: key, authorizedHNR: token}
	if len(key) > 0 && len(passkey) > 0 {
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte("STARPT-TJUPT-ACCOUNT-V1\n"))
		_, _ = mac.Write(passkey)
		service.trackerAccount = hex.EncodeToString(mac.Sum(nil))
	}
	return service
}

// RetireConfigured 表示 STARPT 已配置独立签名。飞牛 Agent 在接单时验证其是否匹配。
func (s *TjuptRSSService) RetireConfigured() bool {
	return len(s.retireKey) > 0 && len(s.authorizedHNR) > 0 && s.trackerAccount != ""
}

// trustedToken 防止通过同一 infohash 的其他北洋园账号授权退种。
func (s *TjuptRSSService) trustedToken(token string) bool {
	return s.RetireConfigured() && hmac.Equal([]byte(strings.TrimSpace(token)), s.authorizedHNR)
}

// Downloaders 只返回已启用的 qB 下载器名称和标识。
func (s *TjuptRSSService) Downloaders() []TjuptDownloader {
	cfg := s.cfg.Get()
	rows, _ := cfg["downloaders"].([]any)
	items := make([]TjuptDownloader, 0, len(rows))
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		id, _ := row["id"].(string)
		d, err := downloaderclient.FromConfig(cfg, strings.TrimSpace(id))
		if err == nil && d.Type == "qbittorrent" {
			items = append(items, TjuptDownloader{ID: d.ID, Name: d.Name})
		}
	}
	return items
}

// Preview 使用 RSS 元数据筛选候选。只读取订阅，预览不修改应用或下载器状态。
func (s *TjuptRSSService) Preview(ctx context.Context, rssURL string, maxGiB int) ([]tjupt.Candidate, error) {
	return s.site.Preview(ctx, rssURL, maxGiB)
}

// PreviewRange 在原有 RSS 预览上应用精确的小数 GiB 范围。缺少体积的条目只展示。
func (s *TjuptRSSService) PreviewRange(ctx context.Context, rssURL string, minGiB, maxGiB float64) ([]tjupt.Candidate, error) {
	if !validTjuptSizeRange(minGiB, maxGiB) {
		return nil, ErrTjuptPolicy
	}
	rows, err := s.Preview(ctx, rssURL, int(math.Ceil(maxGiB)))
	if err != nil {
		return nil, err
	}
	minimum := int64(math.Ceil(minGiB * tjuptGiB))
	maximum := int64(maxGiB * tjuptGiB)
	for i := range rows {
		row := &rows[i]
		if row.SizeBytes == nil {
			continue
		}
		if *row.SizeBytes < minimum {
			row.Eligible, row.Reason = false, "低于预览设置的最小体积"
		} else if *row.SizeBytes > maximum {
			row.Eligible, row.Reason = false, "超过预览设置的最大体积"
		}
	}
	return rows, nil
}

// Status 只查询指定 qB 标签中的任务和站点的 H&R 记录。
func (s *TjuptRSSService) Status(ctx context.Context, downloaderID, token string) ([]TjuptTorrentStatus, error) {
	d, err := s.downloader(downloaderID)
	if err != nil {
		return nil, err
	}
	rows, err := d.TjuptFeedTaggedTorrents(ctx, tjuptTag)
	if err != nil {
		return nil, err
	}
	hashes := make([]string, 0, len(rows))
	items := make([]TjuptTorrentStatus, 0, len(rows))
	matched := make(map[string]downloaderclient.TaggedQBTorrent, len(rows))
	for _, row := range rows {
		hash := strings.ToLower(row.Hash)
		if (row.Tracker != "" && !tjuptTracker(row.Tracker)) || !tjupt.ValidHash(hash) {
			continue
		}
		if _, exists := matched[hash]; exists {
			return nil, errors.New("qB 返回了重复的任务标识")
		}
		matched[hash] = row
		hashes = append(hashes, hash)
		items = append(items, TjuptTorrentStatus{
			Hash: hash, Name: row.Name, State: row.State, Progress: row.Progress,
		})
	}
	if len(hashes) == 0 {
		return items, nil
	}
	records := make(map[string]tjupt.Assessment, len(hashes))
	for first := 0; first < len(hashes); first += tjupt.MaxHNRHashes {
		batch := hashes[first:min(first+tjupt.MaxHNRHashes, len(hashes))]
		found, err := s.site.Status(ctx, token, batch)
		if err != nil {
			return nil, err
		}
		for hash, assessment := range found {
			records[hash] = assessment
		}
	}
	for index, hash := range hashes {
		assessment := records[hash]
		if assessment.Label == "" {
			assessment.Label = "站点未返回考核记录"
		}
		assessment.CanRetire = s.trustedToken(token) && !hmac.Equal(s.retireKey, []byte(d.APIKey)) &&
			tjuptMayRetire(matched[hash], assessment)
		items[index].Assessment = assessment
	}
	return items, nil
}

// Retire 重新核对当前任务与站点 H&R，仅将允许的暂停请求交给飞牛。
// 任一查询失败、考核未完成或任务变更时禁止发送请求。
func (s *TjuptRSSService) Retire(ctx context.Context, downloaderID, token, hash string) error {
	if !s.RetireConfigured() {
		return ErrTjuptRetireDisabled
	}
	if !s.trustedToken(token) {
		return ErrTjuptRetireDenied
	}
	hash = strings.ToLower(strings.TrimSpace(hash))
	if !tjupt.ValidHash(hash) {
		return ErrTjuptRetireDenied
	}
	d, err := s.downloader(downloaderID)
	if err != nil {
		return err
	}
	if hmac.Equal(s.retireKey, []byte(d.APIKey)) {
		return ErrTjuptRetireDisabled
	}
	rows, err := d.TaggedQBTorrents(ctx, tjuptTag, hash)
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return ErrTjuptRetireDenied
	}
	records, err := s.site.Status(ctx, string(s.authorizedHNR), []string{hash})
	if err != nil {
		return err
	}
	if !tjuptMayRetire(rows[0], records[hash]) {
		return ErrTjuptRetireDenied
	}
	addedOn, expiresAt := rows[0].AddedOn, time.Now().Add(90*time.Second).Unix()
	mac := hmac.New(sha256.New, s.retireKey)
	_, _ = fmt.Fprintf(mac, "STARPT-TJUPT-RETIRE-V1\n%s\n%d\n%d\n%s", hash, addedOn, expiresAt, s.trackerAccount)
	if err := d.RetireTJUPTTorrent(ctx, hash, addedOn, expiresAt, s.trackerAccount, hex.EncodeToString(mac.Sum(nil))); err != nil {
		logx.Warnf("北洋园刷流", "飞牛未确认暂停请求，种子哈希=%s", hash)
		return err
	}
	logx.Infof("北洋园刷流", "飞牛已确认暂停做种，种子哈希=%s", hash)
	return nil
}

// loadTjuptSecret 从环境或独立文件读取凭据。缺失或错误时保留只读功能。
func loadTjuptSecret(name string, minimum int) []byte {
	return loadTjuptFileSecret(name, minimum, 1024, true)
}

// loadTjuptFileSecret 读取有固定长度上限的部署凭据。RSS 仍由客户端核对站点和路径。
func loadTjuptFileSecret(name string, minimum, maximum int, allowInline bool) []byte {
	value := os.Getenv(name)
	filename := os.Getenv(name + "_FILE")
	if filename != "" {
		if value != "" {
			return nil
		}
		file, err := os.Open(filename)
		if err != nil {
			return nil
		}
		defer file.Close()
		body, err := io.ReadAll(io.LimitReader(file, int64(maximum+1)))
		if err != nil || len(body) > maximum {
			return nil
		}
		value = string(body)
	} else if !allowInline {
		return nil
	}
	value = strings.TrimSpace(value)
	if len(value) < minimum || len(value) > maximum || strings.ContainsAny(value, " \t\r\n\x00") {
		return nil
	}
	return []byte(value)
}

// downloader 只从现有设置解析下载器，避免使用请求中指定的主机或凭据。
func (s *TjuptRSSService) downloader(id string) (downloaderclient.Downloader, error) {
	d, err := downloaderclient.FromConfig(s.cfg.Get(), strings.TrimSpace(id))
	if err != nil || d.Type != "qbittorrent" {
		return downloaderclient.Downloader{}, ErrTjuptDownloader
	}
	return d, nil
}

// tjuptMayRetire 绑定站点记录与 qB 本次添加的已完成任务，防止使用旧考核记录。
func tjuptMayRetire(row downloaderclient.TaggedQBTorrent, assessment tjupt.Assessment) bool {
	return assessment.CanRetire && row.AddedOn > 0 && !assessment.AddedAt.IsZero() &&
		assessment.AddedAt.Unix() >= row.AddedOn && row.Progress != nil && *row.Progress == 1 &&
		row.State != "pausedUP" && row.State != "stoppedUP" &&
		(row.Tracker == "" || tjuptTracker(row.Tracker))
}

// tjuptTracker 仅使用 Tracker 主机判断来源，避免暴露含 passkey 的地址。
func tjuptTracker(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http" || u.Scheme == "udp") &&
		u.User == nil && tjupt.TrackerHost(u.Hostname())
}
