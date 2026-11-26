package migrationflow

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/acquire/tjupt"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

const (
	tjuptFeedDefaultUploadKiB       = 100
	tjuptFeedDefaultLowSpeedMinutes = 60
	tjuptFeedHNRIntervalSeconds     = 15 * 60
	tjuptFeedCleanupSampleSeconds   = 60
	tjuptFeedSampleSlackSeconds     = 45
	tjuptFeedHNRBatchSize           = 50
)

type tjuptFeedCleanup struct {
	scope           string
	previousRunAt   int64
	runAt           int64
	token           string
	lookup          func(context.Context, string, []string) (map[string]tjupt.Assessment, error)
	uploadThreshold int64
	lowSeconds      int64
	sampleGap       int64
	due             []repository.TjuptFeedTask
	observing       int
	removed         int
	uncertain       int
	lookupFailed    bool
}

func (c *tjuptFeedCycle) observeCleanup(ctx context.Context, task repository.TjuptFeedTask, row *downloaderclient.TaggedQBTorrent, now int64) error {
	observed, due := c.cleanup.sample(task.Cleanup, row, now)
	if observed.LowSince > 0 {
		c.cleanup.observing++
	}
	if observed != task.Cleanup {
		task.Cleanup = observed
		if err := c.saveTask(ctx, task); err != nil {
			return err
		}
	}
	if due {
		c.cleanup.due = append(c.cleanup.due, task)
	}
	return nil
}

// sample 只把相邻轮次的完整采样用于低速时长，期间上传增量超过阈值也会重置计时。
func (r *tjuptFeedCleanup) sample(previous repository.TjuptFeedCleanupObservation, row *downloaderclient.TaggedQBTorrent, now int64) (repository.TjuptFeedCleanupObservation, bool) {
	observed := repository.TjuptFeedCleanupObservation{Scope: r.scope, CheckedAt: previous.CheckedAt}
	if row == nil || !row.TjuptFeedSeeding() || (row.Tracker != "" && !tjuptTracker(row.Tracker)) ||
		row.UploadBPS == nil || *row.UploadBPS < 0 || row.Uploaded == nil || *row.Uploaded < 0 {
		return observed, false
	}
	elapsed := now - previous.SampledAt
	continuous := previous.Scope == r.scope && previous.RunAt == r.previousRunAt &&
		previous.SampledAt > 0 && elapsed > 0 && elapsed <= r.sampleGap && *row.Uploaded >= previous.Uploaded
	if continuous {
		observed = previous
	}
	observed.RunAt, observed.SampledAt, observed.Uploaded = r.runAt, now, *row.Uploaded
	low := *row.UploadBPS < r.uploadThreshold
	if low && continuous {
		low = *row.Uploaded-previous.Uploaded < r.uploadThreshold*elapsed
	}
	if !low {
		observed.LowSince = 0
		return observed, false
	}
	if observed.ConfirmedAt > 0 && observed.LowSince == 0 {
		observed.LowSince = now
	}
	if observed.CheckedAt > 0 && now-observed.CheckedAt < tjuptFeedHNRIntervalSeconds {
		return observed, false
	}
	return observed, observed.ConfirmedAt == 0 ||
		(now-observed.ConfirmedAt >= tjuptFeedHNRIntervalSeconds && now-observed.LowSince >= r.lowSeconds)
}

func (c *tjuptFeedCycle) runCleanup(ctx context.Context, now func() time.Time) error {
	rules := c.cleanup
	defer func() {
		c.cleanupMessage = fmt.Sprintf("自动退种：低速观察 %d 项，已移除 %d 项，待核对 %d 项",
			rules.observing, rules.removed, rules.uncertain)
		if rules.lookupFailed {
			c.cleanupMessage += "，部分 H&R 查询未完成，保留相关任务"
		}
	}()
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for start := 0; start < len(rules.due); start += tjuptFeedHNRBatchSize {
		if requestCtx.Err() != nil {
			rules.lookupFailed = true
			break
		}
		batch := rules.due[start:min(start+tjuptFeedHNRBatchSize, len(rules.due))]
		hashes := make([]string, 0, len(batch))
		for _, task := range batch {
			hashes = append(hashes, task.Hash)
		}
		assessments, err := rules.lookup(requestCtx, rules.token, hashes)
		checkedAt := now().Unix()
		if err != nil {
			rules.lookupFailed = true
			assessments = nil
		}
		for _, task := range batch {
			if err := c.applyCleanupAssessment(ctx, requestCtx, task, assessments[task.Hash], checkedAt); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *tjuptFeedCycle) applyCleanupAssessment(ctx, requestCtx context.Context, task repository.TjuptFeedTask, assessment tjupt.Assessment, checkedAt int64) error {
	rules := c.cleanup
	wasObserved := task.Cleanup.LowSince > 0
	task.Cleanup.CheckedAt = checkedAt
	if !matchesTjuptCleanupAssessment(task, assessment) {
		task.Cleanup.ConfirmedAt, task.Cleanup.RecordID, task.Cleanup.LowSince = 0, 0, 0
		if wasObserved {
			rules.observing--
		}
		return c.saveTask(ctx, task)
	}
	if task.Cleanup.ConfirmedAt == 0 || task.Cleanup.RecordID != assessment.RecordID {
		task.Cleanup.ConfirmedAt, task.Cleanup.RecordID, task.Cleanup.LowSince = checkedAt, assessment.RecordID, checkedAt
		if !wasObserved {
			rules.observing++
		}
		return c.saveTask(ctx, task)
	}
	if requestCtx.Err() != nil || !wasObserved || checkedAt-task.Cleanup.ConfirmedAt < tjuptFeedHNRIntervalSeconds ||
		checkedAt-task.Cleanup.LowSince < rules.lowSeconds {
		return c.saveTask(ctx, task)
	}
	// 先登记待删除。即使程序在 qB 收到请求后退出，也不会再次删除同一任务。
	task.State = repository.TjuptFeedRemoving
	if err := c.saveTask(ctx, task); err != nil {
		return err
	}
	rules.observing--
	err := c.downloader.RemoveTjuptFeedTorrent(requestCtx, task.Hash, task.AddedOn, tjuptFeedOwnershipTag(task), rules.uploadThreshold)
	switch {
	case err == nil:
		task.State = repository.TjuptFeedGone
		if err := c.saveTask(ctx, task); err != nil {
			return err
		}
		c.active--
		c.used -= task.SizeBytes
		rules.removed++
		return nil
	case errors.Is(err, downloaderclient.ErrTjuptFeedUnconfirmed):
		rules.uncertain++
		return nil
	default:
		// 下载器只在确认尚未提交删除时返回其他错误。重新观察并重新做两次考核确认。
		task.State = repository.TjuptFeedStarted
		task.Cleanup = repository.TjuptFeedCleanupObservation{Scope: rules.scope, CheckedAt: checkedAt}
		return c.saveTask(ctx, task)
	}
}

func matchesTjuptCleanupAssessment(task repository.TjuptFeedTask, assessment tjupt.Assessment) bool {
	sourceID, err := strconv.ParseInt(task.SourceID, 10, 64)
	return err == nil && sourceID > 0 && task.AddedOn > 0 && assessment.RecordID > 0 && assessment.TorrentID == sourceID &&
		assessment.CanRetire && assessment.Code != nil && (*assessment.Code == 20 || *assessment.Code == 21) &&
		!assessment.AddedAt.IsZero() && assessment.AddedAt.Unix() >= task.AddedOn
}
