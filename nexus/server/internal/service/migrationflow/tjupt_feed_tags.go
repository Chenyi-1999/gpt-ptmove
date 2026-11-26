package migrationflow

import (
	"context"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

const (
	tjuptFeedTagBatch   = 4
	tjuptFeedTagTimeout = 8 * time.Second
)

func validTjuptFeedTaskMarker(marker string) bool {
	id, ok := strings.CutPrefix(marker, tjuptFeedMarkerPrefix)
	if !ok || len(id) != 24 || id != strings.ToLower(id) {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func tjuptFeedOwnershipTag(task repository.TjuptFeedTask) string {
	if !validTjuptFeedTaskMarker(task.Marker) {
		return ""
	}
	if tjuptFeedUsesSharedTag(task) {
		return downloaderclient.TjuptFeedDisplayTag
	}
	return task.Marker
}

func tjuptFeedUsesSharedTag(task repository.TjuptFeedTask) bool {
	if !task.TagUnified || task.AddedOn <= 0 || task.Scope == "" {
		return false
	}
	switch task.State {
	case repository.TjuptFeedStarted, repository.TjuptFeedStopped, repository.TjuptFeedResuming, repository.TjuptFeedRemoving:
		return true
	}
	return false
}

// normalizeKnownTags 每轮优先整理等待最久的已确认任务，并保留本轮后续操作所需的新归属状态。
func (c *tjuptFeedCycle) normalizeKnownTags(ctx context.Context, tasks []repository.TjuptFeedTask, rows map[string]downloaderclient.TaggedQBTorrent) error {
	var candidates []int
	for i, task := range tasks {
		row, present := rows[strings.ToLower(task.Hash)]
		if present && c.canUnifyTag(task) && tjuptFeedReceiptMatches(task, &row) {
			candidates = append(candidates, i)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return tasks[candidates[i]].TagTriedAt < tasks[candidates[j]].TagTriedAt })
	tagCtx, cancel := context.WithTimeout(ctx, tjuptFeedTagTimeout)
	defer cancel()
	for _, i := range candidates[:min(tjuptFeedTagBatch, len(candidates))] {
		if tagCtx.Err() != nil {
			break
		}
		if err := c.unifyOwnedTags(tagCtx, &tasks[i]); err != nil && tagCtx.Err() == nil {
			return err
		}
		if tasks[i].TagUnified {
			hash := strings.ToLower(tasks[i].Hash)
			row := rows[hash]
			if !tjuptHasTag(row.Tags, downloaderclient.TjuptFeedDisplayTag) {
				row.Tags += "," + downloaderclient.TjuptFeedDisplayTag
				rows[hash] = row
			}
		}
	}
	return ctx.Err()
}

func (c *tjuptFeedCycle) canUnifyTag(task repository.TjuptFeedTask) bool {
	return task.Scope != "" && task.Scope == c.scope && task.AddedOn > 0 && !task.TagCleaned && validTjuptFeedTaskMarker(task.Marker) &&
		(task.State == repository.TjuptFeedStarted || task.State == repository.TjuptFeedStopped || task.State == repository.TjuptFeedResuming)
}

// unifyOwnedTags 先存储可用公共标签的证据，再从同一实例的原任务移除旧标签。
func (c *tjuptFeedCycle) unifyOwnedTags(ctx context.Context, task *repository.TjuptFeedTask) error {
	if !c.canUnifyTag(*task) {
		return nil
	}
	if err := c.repo.RecordTagAttempt(ctx, c.lease, time.Now().Unix(), *task); err != nil {
		return err
	}
	if !task.TagUnified {
		if err := c.downloader.EnsureTjuptFeedDisplayTag(ctx, task.Hash, task.AddedOn, task.Marker); err != nil {
			return tjuptFeedTagWriteError(err)
		}
		if err := c.repo.RecordTagUnified(ctx, c.lease, time.Now().Unix(), *task); err != nil {
			return err
		}
		task.TagUnified = true
	}
	if err := c.downloader.RemoveTjuptFeedLegacyTags(ctx, task.Hash, task.AddedOn, task.Marker); err != nil {
		return tjuptFeedTagWriteError(err)
	}
	if err := c.downloader.PruneEmptyTjuptFeedMarker(ctx, task.Marker); err != nil {
		return tjuptFeedTagWriteError(err)
	}
	if err := c.repo.RecordTagCleaned(ctx, c.lease, time.Now().Unix(), *task); err != nil {
		return err
	}
	task.TagCleaned = true
	return nil
}

// pruneArchivedTags 只检查该下载器已归档任务的独有标记，不处理没有台账的全局标签。
func (c *tjuptFeedCycle) pruneArchivedTags(ctx context.Context) error {
	if c.scope == "" {
		return nil
	}
	tagCtx, cancel := context.WithTimeout(ctx, tjuptFeedTagTimeout)
	defer cancel()
	tasks, err := c.repo.GoneTagMarkers(tagCtx, c.scope, tjuptFeedTagBatch)
	if err != nil {
		if tagCtx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	for _, task := range tasks {
		if tagCtx.Err() != nil {
			break
		}
		if err := c.repo.RecordTagAttempt(tagCtx, c.lease, time.Now().Unix(), task); err != nil {
			if tagCtx.Err() != nil {
				break
			}
			return err
		}
		if !validTjuptFeedTaskMarker(task.Marker) {
			continue
		}
		if err := c.downloader.PruneEmptyTjuptFeedMarker(tagCtx, task.Marker); err != nil {
			if err := tjuptFeedTagWriteError(err); err != nil {
				return err
			}
			continue
		}
		if err := c.repo.RecordTagCleaned(tagCtx, c.lease, time.Now().Unix(), task); err != nil {
			if tagCtx.Err() != nil {
				break
			}
			return err
		}
	}
	return ctx.Err()
}

func tjuptFeedTagWriteError(err error) error {
	if errors.Is(err, repository.ErrTjuptFeedLease) {
		return err
	}
	if !errors.Is(err, downloaderclient.ErrTjuptFeedScope) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		logx.Warnf("北洋园订阅", "标签核对未完成，后续会重新整理")
	}
	return nil
}
