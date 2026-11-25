package migrationflow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/acquire/tjupt"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

const tjuptFeedMarkerPrefix = "starpt-tjupt-trial-"

// 超时后仍需通过 qB 读取确认任务不存在，才释放不确定操作的容量。
// 历史去重记录保留，不会重新发送同一来源或哈希的写请求。
const tjuptFeedPendingSeconds = 600

type tjuptFeedDownloader interface {
	TjuptFeedTorrents(context.Context, []string) (map[string]downloaderclient.TaggedQBTorrent, error)
	AddTjuptFeedTorrent(context.Context, []byte, string, string, bool, []string, string, string, int) (*downloaderclient.TaggedQBTorrent, error)
	ResumeTjuptFeedTorrent(context.Context, string, int64, string, string) (*downloaderclient.TaggedQBTorrent, error)
	RemoveTjuptFeedTorrent(context.Context, string, int64, string, int64) error
}

func (f *TjuptFeed) run(parent context.Context, trial bool) (TjuptFeedResult, error) {
	var result TjuptFeedResult
	row, err := f.repo.Control(parent)
	if err != nil {
		return result, err
	}
	now := time.Now()
	fetchRSS, sampleCleanup := true, false
	if trial {
		if row.Enabled || row.LeaseUntil > now.Unix() {
			return result, ErrTjuptFeedBusy
		}
		if _, _, err = f.runConfig(row); err != nil {
			return result, err
		}
		if err = f.checkLegacy(parent); err != nil {
			return result, err
		}
	} else {
		if !row.Enabled || row.LeaseUntil > now.Unix() {
			return result, nil
		}
		settings, _ := decodeTjuptFeedSettings(row.Policy)
		fetchRSS = row.LastRun == 0 || row.LastRun+int64(row.IntervalMinutes)*60 <= now.Unix()
		sampleCleanup = settings.CleanupEnabled && (row.LastCleanup == 0 ||
			row.LastCleanup+tjuptFeedCleanupSampleSeconds <= now.Unix())
		if !fetchRSS && !sampleCleanup {
			return result, nil
		}
	}
	lease, err := tjuptFeedRandomID()
	if err != nil {
		return result, err
	}
	acquired, err := f.repo.Claim(parent, row, lease, trial, fetchRSS, sampleCleanup, now.Unix(), now.Add(3*time.Minute).Unix())
	if err != nil || !acquired {
		if err == nil && trial {
			err = ErrTjuptFeedBusy
		}
		return result, err
	}
	ctx, cancel := context.WithTimeout(parent, 115*time.Second)
	defer cancel()
	result, runErr := f.runCycle(ctx, row, lease, trial, fetchRSS, sampleCleanup, now.Unix())
	finishCtx, done := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer done()
	return result, errors.Join(runErr, f.repo.Finish(finishCtx, lease, result.Message))
}

func (f *TjuptFeed) runCycle(ctx context.Context, row repository.TjuptFeedControl, lease string, trial, fetchRSS, sampleCleanup bool, runAt int64) (TjuptFeedResult, error) {
	cfg, d, err := f.runConfig(row)
	if err != nil {
		return TjuptFeedResult{Message: "插件设置不可用，请检查保存的 RSS 和下载器"}, err
	}
	if err := f.checkLegacy(ctx); err != nil {
		return TjuptFeedResult{Message: "旧版北洋园任务尚未结束，本轮未投种"}, err
	}
	scope := tjuptFeedDigest(d.ID, d.Host, d.Username)
	guard := func(uploadLimitRevision *int64) error {
		if err := f.repo.Verify(ctx, lease, trial, time.Now().Unix(), uploadLimitRevision); err != nil {
			return err
		}
		current, err := f.plugin.downloader(cfg.DownloaderID)
		if err != nil || tjuptFeedDigest(current.ID, current.Host, current.Username) != scope ||
			current.APIKey != d.APIKey || current.Password != d.Password {
			return tjuptFeedConfigError("下载器配置在运行期间已变更")
		}
		return f.checkLegacy(ctx)
	}
	cycle := tjuptFeedCycle{
		cfg: cfg, repo: f.repo, downloader: f.connect(d, func() error { return guard(nil) }), lease: lease, trial: trial, scope: scope,
		useSiteUploadLimit: row.UseSiteUploadLimit,
		newAdder: func() tjuptFeedDownloader {
			return f.connect(d, func() error { return guard(&row.SiteUploadLimitRevision) })
		},
		trialScope: tjuptFeedDigest(scope, cfg.rss, cfg.Tags, cfg.Category, cfg.SavePath,
			fmt.Sprintf("%g/%g/%g/%d", cfg.MinGiB, cfg.MaxGiB, cfg.TotalLimitGiB, cfg.ActiveLimit)),
	}
	if sampleCleanup {
		hnrConfig, err := f.loadConfig(row, false, true)
		if err != nil || hnrConfig.hnr == "" {
			cycle.cleanupMessage = "自动退种等待有效的北洋园 H&R API Token"
		} else {
			cycle.cleanup = &tjuptFeedCleanup{
				scope:         tjuptFeedDigest(f.cleanupEpoch, scope, d.Password, d.APIKey, fmt.Sprint(row.Revision)),
				previousRunAt: row.LastCleanup, runAt: runAt,
				token: hnrConfig.hnr, lookup: f.plugin.site.Status,
				uploadThreshold: int64(cfg.UploadThresholdKiB) * 1024,
				lowSeconds:      int64(cfg.LowSpeedMinutes) * 60,
				sampleGap:       tjuptFeedCleanupSampleSeconds + tjuptFeedSampleSlackSeconds,
			}
		}
	}
	return cycle.execute(ctx, f.site, fetchRSS)
}

type tjuptFeedCycle struct {
	cfg                tjuptFeedConfig
	repo               *repository.TjuptFeedRepository
	downloader         tjuptFeedDownloader
	lease              string
	scope              string
	trialScope         string
	trial              bool
	used               int64
	active             int
	resumed            int
	held               int
	resumeFailed       int
	cleanup            *tjuptFeedCleanup
	cleanupMessage     string
	useSiteUploadLimit bool
	newAdder           func() tjuptFeedDownloader
}

func (c *tjuptFeedCycle) execute(ctx context.Context, site tjuptAcquisition, fetchRSS bool) (result TjuptFeedResult, runErr error) {
	defer func() {
		if c.cleanupMessage != "" {
			if result.Message != "" {
				result.Message += "。"
			}
			result.Message += c.cleanupMessage
		}
	}()
	var candidates []tjupt.AutoCandidate
	var rssErr error
	if fetchRSS {
		candidates, rssErr = site.AutoCandidates(ctx, c.cfg.rss)
	}
	if !fetchRSS && c.cleanup == nil {
		return result, nil
	}
	if rssErr != nil && c.cleanup == nil {
		return TjuptFeedResult{Message: "无法获取北洋园 RSS，请检查已保存的订阅"}, rssErr
	}
	if err := c.reconcile(ctx); err != nil {
		result.Message = "本轮未能核对 qB 台账，请检查下载器连接"
		return result, err
	}
	if c.cleanup != nil {
		if err := c.runCleanup(ctx, time.Now); err != nil {
			return TjuptFeedResult{Message: "自动退种台账未能更新，本轮已停止"}, err
		}
	}
	if !fetchRSS {
		return result, nil
	}
	if rssErr != nil {
		return TjuptFeedResult{Message: "无法获取北洋园 RSS，请检查已保存的订阅"}, rssErr
	}
	minBytes := int64(math.Ceil(c.cfg.MinGiB * tjuptGiB))
	maxBytes := int64(c.cfg.MaxGiB * tjuptGiB)
	totalBytes := int64(c.cfg.TotalLimitGiB * tjuptGiB)
	fetched := 0
	for _, candidate := range candidates {
		if c.cfg.ActiveLimit > 0 && c.active >= c.cfg.ActiveLimit {
			result.Message = "已达到在管任务数上限"
			break
		}
		if c.used >= totalBytes || totalBytes-c.used < minBytes {
			result.Message = "已达到在管总容量上限"
			break
		}
		if fetched >= 8 || (c.trial && result.Added > 0) {
			break
		}
		if ctx.Err() != nil {
			result.Message = "本轮已中断，未继续投种"
			return result, ctx.Err()
		}
		seen, err := c.repo.SeenSource(ctx, candidate.ID)
		if err != nil {
			result.Message = "无法读取北洋园投种台账"
			return result, err
		}
		if seen || (candidate.SizeBytes != nil && (*candidate.SizeBytes < minBytes || *candidate.SizeBytes > maxBytes)) {
			result.Skipped++
			continue
		}
		fetched++
		meta, err := site.DownloadCandidate(ctx, candidate)
		if err != nil || !tjupt.ValidHash(meta.Hash) || meta.SizeBytes <= 0 || len(meta.Data) == 0 {
			result.Failed++
			continue
		}
		if meta.SizeBytes < minBytes || meta.SizeBytes > maxBytes {
			result.Skipped++
			continue
		}
		if meta.SizeBytes > totalBytes-c.used {
			result.Skipped++
			result.Message = "在管总容量剩余额度不足以添加部分种子"
			continue
		}
		notice, err := c.add(ctx, candidate.ID, meta, totalBytes, &result)
		if notice != "" {
			result.Message = notice
			return result, err
		}
	}
	summary := fmt.Sprintf("新增 %d，跳过 %d，失败 %d", result.Added, result.Skipped, result.Failed)
	if c.trial && result.Added > 0 {
		summary += "。试运行任务已暂停添加"
	}
	if c.resumed > 0 {
		summary += fmt.Sprintf("，已启动 %d 个试运行任务", c.resumed)
	}
	if c.resumeFailed > 0 {
		summary += fmt.Sprintf("，%d 个试运行任务启动结果需在 qB 核对", c.resumeFailed)
	}
	if c.held > 0 {
		summary += fmt.Sprintf("，%d 个任务保留原状态或占用额度，请在 qB 核对", c.held)
	}
	if result.Message != "" {
		summary += "。" + result.Message
	}
	result.Message = summary
	return result, nil
}

func (c *tjuptFeedCycle) add(ctx context.Context, sourceID string, meta tjupt.Torrent, limit int64, result *TjuptFeedResult) (string, error) {
	uploadLimitMBps := 0
	if c.useSiteUploadLimit {
		var err error
		uploadLimitMBps, err = tjuptSiteUploadLimit(ctx, c.repo)
		if err != nil {
			return "无法读取北洋园站点上传限速，本轮未继续投种", nil
		}
	}
	random, err := tjuptFeedRandomID()
	if err != nil {
		return "无法生成投种回执标识，本轮已停止", err
	}
	task := repository.TjuptFeedTask{
		Hash: strings.ToLower(meta.Hash), SourceID: sourceID, Scope: c.scope, SizeBytes: meta.SizeBytes,
		State: repository.TjuptFeedAdding, Marker: tjuptFeedMarkerPrefix + random, ReservedAt: time.Now().Unix(),
	}
	if c.trial {
		task.TrialScope = c.trialScope
	}
	acquired, err := c.repo.Reserve(ctx, c.lease, task.ReservedAt, task, limit, c.cfg.ActiveLimit)
	if err != nil {
		return "无法登记本次投种，本轮已停止", err
	}
	if !acquired {
		result.Skipped++
		return "", nil
	}
	receipt, err := c.newAdder().AddTjuptFeedTorrent(ctx, meta.Data, task.Hash+".torrent",
		c.cfg.SavePath, c.trial, append(tjuptFeedTags(c.cfg.Tags), task.Marker), c.cfg.Category, task.Hash, uploadLimitMBps)
	if err == nil && (!tjuptFeedReceiptMatches(task, receipt) || (c.trial && !receipt.TjuptFeedStopped()) ||
		(!c.trial && !receipt.TjuptFeedStarted())) {
		err = downloaderclient.ErrTjuptFeedUnconfirmed
	}
	if errors.Is(err, downloaderclient.ErrTjuptFeedUnconfirmed) {
		result.Failed++
		return "本次投种回执未确认，请在 qB 核对；同一哈希不会自动重试", nil
	}
	if errors.Is(err, downloaderclient.ErrTjuptFeedAlreadyExists) {
		task.State = repository.TjuptFeedGone
		if err := c.saveTask(ctx, task); err != nil {
			return "无法保存重复种子的核对结果", err
		}
		result.Skipped++
		return "", nil
	}
	if err != nil {
		if releaseErr := c.repo.ReleaseAdd(ctx, c.lease, time.Now().Unix(), task.Hash); releaseErr != nil {
			return "本轮无法更新投种台账", releaseErr
		}
		if errors.Is(err, repository.ErrTjuptFeedLease) {
			return "运行状态或上传限速开关已变更，本轮未继续投种", nil
		}
		result.Failed++
		return "qB 未能接收新任务，请检查下载器连接或配置", nil
	}
	task.AddedOn, task.State = receipt.AddedOn, repository.TjuptFeedStarted
	if c.trial {
		task.State = repository.TjuptFeedStopped
	}
	if err := c.saveTask(ctx, task); err != nil {
		return "qB 已回应，但本轮未能保存回执，请在 qB 核对", err
	}
	c.active++
	c.used += task.SizeBytes
	result.Added++
	return "", nil
}

func (c *tjuptFeedCycle) reconcile(ctx context.Context) error {
	tasks, err := c.repo.LiveTasks(ctx, c.scope)
	if err != nil || len(tasks) == 0 {
		return err
	}
	hashes := make([]string, 0, len(tasks))
	for _, task := range tasks {
		hashes = append(hashes, task.Hash)
	}
	// 完整取得所有批次后，只有明确不存在的任务才进入释放额度判断。
	receipts, err := c.downloader.TjuptFeedTorrents(ctx, hashes)
	if err != nil {
		return err
	}
	if receipts == nil {
		return errors.New("qB 未确认任务列表")
	}
	for _, task := range tasks {
		receipt, exists := receipts[strings.ToLower(task.Hash)]
		switch {
		case !exists && (task.AddedOn > 0 || (task.ReservedAt > 0 && time.Now().Unix()-task.ReservedAt >= tjuptFeedPendingSeconds)),
			exists && task.State != repository.TjuptFeedRemoving && task.AddedOn > 0 && receipt.AddedOn > 0 && task.AddedOn != receipt.AddedOn:
			task.State = repository.TjuptFeedGone
			if err := c.saveTask(ctx, task); err != nil {
				return err
			}
			continue
		case task.State == repository.TjuptFeedRemoving:
			if c.cleanup != nil {
				c.cleanup.uncertain++
			}
		case !exists || !tjuptFeedReceiptMatches(task, &receipt):
			c.held++
		case task.State == repository.TjuptFeedStarted && c.cleanup != nil:
			if err := c.observeCleanup(ctx, task, &receipt, time.Now().Unix()); err != nil {
				return err
			}
		default:
			if err := c.reconcileOwned(ctx, task, &receipt); err != nil {
				return err
			}
		}
		c.active++
		c.used += task.SizeBytes
	}
	return nil
}

func (c *tjuptFeedCycle) reconcileOwned(ctx context.Context, task repository.TjuptFeedTask, receipt *downloaderclient.TaggedQBTorrent) error {
	switch {
	case task.State == repository.TjuptFeedStarted:
		return nil // 已经启动过的任务由用户控制，即使后来手动停止也不重新启动。
	case receipt.TjuptFeedStarted():
		task.AddedOn, task.State = receipt.AddedOn, repository.TjuptFeedStarted
		return c.saveTask(ctx, task)
	case task.State == repository.TjuptFeedResuming:
		c.held++ // 已发出过启动请求，不重复尝试。
		return nil
	case task.TrialScope == "" || !receipt.TjuptFeedStopped():
		c.held++
		return nil
	}
	if task.State == repository.TjuptFeedAdding {
		task.AddedOn, task.State = receipt.AddedOn, repository.TjuptFeedStopped
		if err := c.saveTask(ctx, task); err != nil {
			return err
		}
	}
	if c.trial {
		return nil
	}
	if task.TrialScope != c.trialScope || !receipt.TjuptFeedUntouched() || receipt.Category != c.cfg.Category {
		c.held++
		return nil
	}
	task.State = repository.TjuptFeedResuming
	if err := c.saveTask(ctx, task); err != nil {
		return err
	}
	resumed, err := c.downloader.ResumeTjuptFeedTorrent(ctx, task.Hash, task.AddedOn, task.Marker, c.cfg.Category)
	if err == nil && tjuptFeedReceiptMatches(task, resumed) && resumed.TjuptFeedStarted() {
		task.State = repository.TjuptFeedStarted
		c.resumed++
		return c.saveTask(ctx, task)
	}
	if errors.Is(err, downloaderclient.ErrTjuptFeedScope) {
		task.State = repository.TjuptFeedStopped
		c.held++
		return c.saveTask(ctx, task)
	}
	c.resumeFailed++
	if err != nil && !errors.Is(err, downloaderclient.ErrTjuptFeedUnconfirmed) {
		task.State = repository.TjuptFeedStopped
		return c.saveTask(ctx, task)
	}
	return nil
}

func (c *tjuptFeedCycle) saveTask(ctx context.Context, task repository.TjuptFeedTask) error {
	return c.repo.UpdateTask(ctx, c.lease, time.Now().Unix(), task)
}

func tjuptFeedReceiptMatches(task repository.TjuptFeedTask, receipt *downloaderclient.TaggedQBTorrent) bool {
	return receipt != nil && receipt.AddedOn > 0 && strings.EqualFold(receipt.Hash, task.Hash) &&
		(task.AddedOn == 0 || task.AddedOn == receipt.AddedOn) &&
		tjuptHasTag(receipt.Tags, tjuptTag) && tjuptHasTag(receipt.Tags, task.Marker)
}

func tjuptFeedDigest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func tjuptFeedRandomID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
