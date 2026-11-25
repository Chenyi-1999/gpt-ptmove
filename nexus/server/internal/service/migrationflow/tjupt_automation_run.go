package migrationflow

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/acquire/tjupt"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

const (
	tjuptAutoTag    = "starpt-tjupt-auto"
	tjuptPendingTag = "starpt-tjupt-pending-delete"
	tjuptAdding     = "adding"
	tjuptManaged    = "managed"
	tjuptMarked     = "marked"
	tjuptDeleting   = "delete_unconfirmed"
	tjuptDeleted    = "deleted"
)

var errTjuptUnconfirmed = errors.New("飞牛写操作结果未确认")

// runIfDue 领取租约并执行一轮。进程退出或回执不确定时保留台账和容量。
func (a *TjuptAutomation) runIfDue(parent context.Context) error {
	control, err := a.repo.Control(parent)
	if err != nil || !control.Enabled {
		return err
	}
	now := time.Now()
	if control.LastRun > 0 && now.Unix() < control.LastRun+int64(control.IntervalMinutes)*60 {
		return nil
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	lease := hex.EncodeToString(random[:])
	acquired, err := a.repo.Claim(parent, control.Policy, lease, now.Unix(), now.Add(3*time.Minute).Unix())
	if err != nil || !acquired {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 115*time.Second)
	defer cancel()
	message, runErr := a.runTjuptCycle(ctx, control.Policy, lease)
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer finishCancel()
	if errors.Is(runErr, errTjuptUnconfirmed) {
		return a.repo.HoldUntilExpiry(finishCtx, lease, message)
	}
	return a.repo.Finish(finishCtx, lease, message)
}

func (a *TjuptAutomation) runTjuptCycle(ctx context.Context, policy, lease string) (string, error) {
	var settings TjuptAutomationSettings
	if json.Unmarshal([]byte(policy), &settings) != nil || validateTjuptAutomation(settings) != nil ||
		!a.writeConfigured() || a.rss == "" {
		return "自动化配置无效，本轮未执行", nil
	}
	if !a.exclusiveQB {
		settings.CleanupMode = "mark"
	}
	d, err := a.plugin.downloader(settings.DownloaderID)
	if err != nil || d.APIKey == "" || hmac.Equal([]byte(d.APIKey), a.plugin.retireKey) {
		return "飞牛下载器或签名配置无法确认，本轮未执行", nil
	}
	tasks, err := a.repo.LiveTasks(ctx)
	if err != nil {
		return "无法读取容量台账，本轮未执行", err
	}
	registered := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if task.DownloaderID != settings.DownloaderID || task.SaveRoot != a.saveRoot {
			return "下载器或目录与在管任务不一致，本轮未执行", nil
		}
		registered[task.Hash] = true
	}
	rows, err := d.TaggedQBTorrents(ctx, tjuptAutoTag, "")
	if err != nil {
		return "飞牛 qB 状态查询失败，保留现有任务", a.invalidateTjuptObservations(ctx, tasks, lease)
	}
	qb := make(map[string]downloaderclient.TaggedQBTorrent, len(rows))
	for _, row := range rows {
		hash := strings.ToLower(row.Hash)
		if !tjupt.ValidHash(hash) {
			return "飞牛返回无效任务标识，保留现有任务", a.invalidateTjuptObservations(ctx, tasks, lease)
		}
		if !registered[hash] {
			return "qB 有未登记的北洋园自动任务，核对台账前暂停投种和清理", a.invalidateTjuptObservations(ctx, tasks, lease)
		}
		if _, duplicate := qb[hash]; duplicate {
			return "飞牛返回重复任务标识，保留现有任务", a.invalidateTjuptObservations(ctx, tasks, lease)
		}
		qb[hash] = row
	}
	hashes := make([]string, 0, len(tasks))
	for _, task := range tasks {
		row := qb[task.Hash]
		if task.State != tjuptDeleting && tjuptOwned(row, task) && row.Progress != nil && *row.Progress == 1 {
			hashes = append(hashes, task.Hash)
		}
	}
	var assessments map[string]tjupt.Assessment
	var hnrErr error
	if len(hashes) > 0 {
		assessments, hnrErr = a.plugin.site.Status(ctx, string(a.plugin.authorizedHNR), hashes)
	}
	var marks, deletes int
	for _, task := range tasks {
		mark, deleted, err := a.manageTjuptTask(ctx, d, settings, &task, qb[task.Hash], assessments[task.Hash], lease)
		marks += mark
		deletes += deleted
		if err != nil {
			return "飞牛操作或台账更新未确认，保留相关任务与额度", err
		}
	}
	if hnrErr != nil {
		return "北洋园 H&R 查询失败，暂停清理和新增下载", nil
	}
	adds, limited, err := a.addTjuptCandidates(ctx, d, settings, lease)
	summary := fmt.Sprintf("投种 %d 项，标记待删除 %d 项，确认删除 %d 项", adds, marks, deletes)
	if err != nil {
		return summary + "。RSS 读取或新增操作未确认", err
	}
	if limited {
		return summary + "。已达到容量或任务数上限，暂停新增", nil
	}
	return summary, nil
}

func (a *TjuptAutomation) invalidateTjuptObservations(ctx context.Context, tasks []repository.TjuptAutomationTask, lease string) error {
	for _, task := range tasks {
		if resetTjuptObservation(&task) {
			if err := a.repo.UpdateTask(ctx, task, lease, time.Now().Unix()); err != nil {
				return err
			}
		}
	}
	return nil
}

func resetTjuptObservation(task *repository.TjuptAutomationTask) bool {
	if task.State == tjuptDeleting || (task.LowSince == 0 && task.State != tjuptMarked) {
		return false
	}
	task.LowSince, task.LastSample, task.LastUploaded = 0, 0, 0
	if task.State == tjuptMarked {
		task.State = tjuptManaged
	}
	return true
}

// manageTjuptTask 每轮重新观察状态。首次满足清理条件只添加待删除标签。
func (a *TjuptAutomation) manageTjuptTask(ctx context.Context, d downloaderclient.Downloader, settings TjuptAutomationSettings,
	task *repository.TjuptAutomationTask, row downloaderclient.TaggedQBTorrent, assessment tjupt.Assessment, lease string,
) (int, int, error) {
	if !tjuptOwned(row, *task) {
		if resetTjuptObservation(task) {
			return 0, 0, a.repo.UpdateTask(ctx, *task, lease, time.Now().Unix())
		}
		return 0, 0, nil
	}
	now := time.Now().Unix()
	save := func() error { return a.repo.UpdateTask(ctx, *task, lease, time.Now().Unix()) }
	if task.State == tjuptDeleting {
		// 签名和旧请求均过期，且 qB 仍确认同一运行实例存在，重新开始观察。
		if now-task.UpdatedAt.Unix() <= 180 {
			return 0, 0, nil
		}
		task.State, task.LowSince, task.LastSample, task.LastUploaded = tjuptManaged, 0, 0, 0
		if tjuptHasTag(row.Tags, tjuptPendingTag) {
			if err := a.sendTjuptEffect(ctx, d, *task, lease, "unmark", nil, 0, 0); err != nil {
				return 0, 0, err
			}
		}
		return 0, 0, save()
	}
	if task.State == tjuptAdding {
		task.AddedOn, task.State = row.AddedOn, tjuptManaged
	}
	if task.State == tjuptMarked && !tjuptHasTag(row.Tags, tjuptPendingTag) {
		resetTjuptObservation(task)
	}
	minSeconds := tjuptSeedSeconds(task.SizeBytes, settings.BufferHours)
	maxBPS := settings.UploadThresholdKiB * 1024
	observed := tjuptMayRetire(row, assessment) && tjuptSeeding(row.State) && row.SeedingTime != nil &&
		*row.SeedingTime >= minSeconds && row.UploadBPS != nil && *row.UploadBPS >= 0 && *row.UploadBPS < maxBPS &&
		row.Uploaded != nil && *row.Uploaded >= 0
	stamp := fmt.Sprintf("%d/%d/%d/%d", minSeconds, maxBPS, settings.LowSpeedMinutes, settings.IntervalMinutes)
	if seconds := now - task.LastSample; observed && task.PolicyStamp == stamp && task.LastSample > 0 &&
		seconds > 0 && seconds <= int64(settings.IntervalMinutes)*60+30 {
		observed = *row.Uploaded >= task.LastUploaded && *row.Uploaded-task.LastUploaded < maxBPS*seconds
	}
	ready := observeTjuptLowSpeed(task, stamp, now, observed, settings.IntervalMinutes, settings.LowSpeedMinutes)
	if observed {
		task.LastUploaded = *row.Uploaded
	} else {
		task.LastUploaded = 0
	}
	if !observed && task.State == tjuptMarked {
		err := a.sendTjuptEffect(ctx, d, *task, lease, "unmark", nil, 0, 0)
		if err == nil {
			task.State = tjuptManaged
		}
		return 0, 0, errors.Join(save(), err)
	}
	if !ready {
		return 0, 0, save()
	}
	if task.State != tjuptMarked {
		if err := a.sendTjuptEffect(ctx, d, *task, lease, "mark", nil, maxBPS, minSeconds); err != nil {
			return 0, 0, errors.Join(save(), err)
		}
		task.State = tjuptMarked
		return 1, 0, save()
	}
	if settings.CleanupMode != "delete" {
		return 0, 0, save()
	}
	// 删除前不复用同一轮早先查询的 H&R 结果。失败时重新积累观察时间。
	current, err := a.plugin.site.Status(ctx, string(a.plugin.authorizedHNR), []string{task.Hash})
	if err != nil || !tjuptMayRetire(row, current[task.Hash]) {
		task.LowSince, task.LastSample, task.LastUploaded = 0, 0, 0
		return 0, 0, errors.Join(save(), err)
	}
	task.State = tjuptDeleting
	if err := save(); err != nil {
		return 0, 0, err
	}
	if err := a.sendTjuptEffect(ctx, d, *task, lease, "delete", nil, maxBPS, minSeconds); err != nil {
		return 0, 0, err
	}
	task.State, task.LowSince, task.LastSample, task.LastUploaded = tjuptDeleted, 0, 0, 0
	return 0, 1, save()
}

func observeTjuptLowSpeed(task *repository.TjuptAutomationTask, policy string, now int64, low bool, interval, minutes int) bool {
	if !low {
		task.PolicyStamp, task.LowSince, task.LastSample = policy, 0, 0
		return false
	}
	if task.PolicyStamp != policy || task.LowSince == 0 || now <= task.LastSample ||
		now-task.LastSample > int64(interval)*60+30 {
		task.PolicyStamp, task.LowSince, task.LastSample = policy, now, now
		return false
	}
	task.LastSample = now
	return now-task.LowSince >= int64(minutes)*60
}

func tjuptOwned(row downloaderclient.TaggedQBTorrent, task repository.TjuptAutomationTask) bool {
	return row.AddedOn > 0 && (task.AddedOn == 0 || task.AddedOn == row.AddedOn) &&
		strings.EqualFold(row.Hash, task.Hash) && row.SavePath != "" &&
		path.Clean(row.SavePath) == path.Join(task.SaveRoot, task.Hash) &&
		tjuptHasTag(row.Tags, tjuptTag) && tjuptHasTag(row.Tags, tjuptAutoTag)
}

func tjuptHasTag(tags, expected string) bool {
	for _, tag := range strings.Split(tags, ",") {
		if strings.TrimSpace(tag) == expected {
			return true
		}
	}
	return false
}

func tjuptSeeding(state string) bool {
	return state == "uploading" || state == "stalledUP" || state == "forcedUP"
}

func tjuptSeedSeconds(size int64, bufferHours float64) int64 {
	var base int64
	switch {
	case size <= 10*tjuptGiB:
		base = 24
	case size <= 20*tjuptGiB:
		base = 48
	case size <= 30*tjuptGiB:
		base = 72
	case size <= 40*tjuptGiB:
		base = 96
	case size <= 50*tjuptGiB:
		base = 120
	default:
		base = 168
	}
	return base*3600 + int64(math.Ceil(bufferHours*3600))
}

// addTjuptCandidates 核实种子的实际大小，并在外部请求前原子预留完整空间。
func (a *TjuptAutomation) addTjuptCandidates(ctx context.Context, d downloaderclient.Downloader, settings TjuptAutomationSettings, lease string) (int, bool, error) {
	tasks, err := a.repo.LiveTasks(ctx)
	if err != nil {
		return 0, false, err
	}
	used := int64(0)
	for _, task := range tasks {
		used += task.SizeBytes
	}
	capBytes := int64(settings.TotalLimitGiB * tjuptGiB)
	minBytes := int64(math.Ceil(settings.MinGiB * tjuptGiB))
	maxBytes := int64(settings.MaxGiB * tjuptGiB)
	if len(tasks) >= settings.ActiveLimit || used >= capBytes || minBytes > capBytes-used {
		return 0, true, nil
	}
	candidates, err := a.site.AutoCandidates(ctx, a.rss)
	if err != nil {
		return 0, false, err
	}
	adds, fetched := 0, 0
	for _, item := range candidates {
		if len(tasks)+adds >= settings.ActiveLimit || used >= capBytes || minBytes > capBytes-used {
			return adds, true, nil
		}
		if fetched >= 8 {
			break
		}
		seen, err := a.repo.SeenSource(ctx, item.ID)
		if err != nil {
			return adds, false, err
		}
		if seen {
			continue
		}
		fetched++
		torrent, err := a.site.DownloadCandidate(ctx, item)
		if err != nil {
			continue
		}
		if !tjupt.ValidHash(torrent.Hash) || torrent.SizeBytes <= 0 ||
			torrent.SizeBytes < minBytes || torrent.SizeBytes > maxBytes || torrent.SizeBytes > capBytes-used {
			continue
		}
		exists, err := d.QBHasTorrent(ctx, torrent.Hash)
		if err != nil {
			return adds, false, err
		}
		if exists {
			continue
		}
		task := repository.TjuptAutomationTask{
			Hash: torrent.Hash, SourceID: item.ID, DownloaderID: d.ID, SaveRoot: a.saveRoot,
			Title: item.Title, SizeBytes: torrent.SizeBytes, State: tjuptAdding,
		}
		reserved, err := a.repo.Reserve(ctx, &task, lease, time.Now().Unix(), capBytes, settings.ActiveLimit)
		if err != nil {
			return adds, false, err
		}
		if !reserved {
			continue
		}
		if err := a.sendTjuptEffect(ctx, d, task, lease, "add", torrent.Data, 0, 0); err != nil {
			if errors.Is(err, downloaderclient.ErrTjuptAutoNotApplied) {
				err = errors.Join(err, a.repo.ReleaseUnappliedAdd(ctx, task.Hash, lease, time.Now().Unix()))
			}
			return adds, false, err
		}
		used += torrent.SizeBytes
		adds++
	}
	return adds, false, nil
}

func (a *TjuptAutomation) sendTjuptEffect(ctx context.Context, d downloaderclient.Downloader, task repository.TjuptAutomationTask, lease, action string, torrent []byte, maxBPS, minSeconds int64) error {
	now := time.Now().Unix()
	until, err := a.repo.LeaseUntil(ctx, lease, now)
	if err != nil {
		return err
	}
	// 给本机 qB 执行留出余量。控制端超时后，设置在租约过期前保持锁定。
	expires := min(now+90, until-35)
	if expires <= now+2 {
		return repository.ErrTjuptAutomationLeaseLost
	}
	if torrent == nil {
		torrent = []byte{}
	}
	digest := sha256.Sum256(torrent)
	mac := hmac.New(sha256.New, a.plugin.retireKey)
	_, _ = fmt.Fprintf(mac, "STARPT-TJUPT-AUTO-V1\n%s\n%s\n%d\n%d\n%s\n%s\n%s\n%d\n%d",
		action, task.Hash, task.AddedOn, expires, a.plugin.trackerAccount, task.SaveRoot, hex.EncodeToString(digest[:]), maxBPS, minSeconds)
	err = d.RunTjuptAutoEffect(ctx, downloaderclient.TjuptAutoEffect{
		Action: action, Hash: task.Hash, AddedOn: task.AddedOn, ExpiresAt: expires, Account: a.plugin.trackerAccount,
		Root: task.SaveRoot, Torrent: torrent, Proof: hex.EncodeToString(mac.Sum(nil)), MaxUploadBPS: maxBPS, MinSeedingSeconds: minSeconds,
	})
	if err != nil && !errors.Is(err, downloaderclient.ErrTjuptAutoNotApplied) {
		return errors.Join(errTjuptUnconfirmed, err)
	}
	return err
}
