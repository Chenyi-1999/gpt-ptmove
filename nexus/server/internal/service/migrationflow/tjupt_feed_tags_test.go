package migrationflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

func readTjuptTagFixture(t *testing.T, fixture *tjuptCleanupFixture) repository.TjuptFeedTask {
	t.Helper()
	tasks, err := fixture.feed.repo.LiveTasks(context.Background(), fixture.task.Scope)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("cannot read label ledger: %+v, %v", tasks, err)
	}
	return tasks[0]
}

func TestTjuptFeedCommitsSharedLabelBeforeRemovingUniqueProof(t *testing.T) {
	f := newTjuptCleanupFixture(t)
	ctx := context.Background()
	cycle := tjuptFeedCycle{repo: f.feed.repo, lease: f.lease, scope: f.task.Scope, downloader: f.qb}
	row := f.qb.rows[f.task.Hash]
	row.Tags += ", custom"
	f.qb.rows[f.task.Hash] = row
	checked := false
	f.qb.beforeTagRemove = func() {
		stored := readTjuptTagFixture(t, f)
		if !stored.TagUnified || stored.TagCleaned || stored.Marker != f.task.Marker || stored.AddedOn != f.task.AddedOn {
			t.Fatalf("old proof removed before durable new ownership: %+v", stored)
		}
		checked = true
	}
	f.qb.tagRemoveErr = downloaderclient.ErrTjuptFeedUnconfirmed
	if err := cycle.reconcile(ctx); err != nil || !checked {
		t.Fatalf("could not verify first label cutover: %v", err)
	}
	if stored := readTjuptTagFixture(t, f); !stored.TagUnified || stored.TagCleaned || len(f.qb.pruned) != 0 {
		t.Fatalf("uncertain marker removal was treated as cleaned: %+v, %v", stored, f.qb.pruned)
	}

	// 模拟 qB 已应用清理，但工作进程在确认清理结果之前退出。
	row.Tags = downloaderclient.TjuptFeedDisplayTag + ",custom"
	f.qb.rows[f.task.Hash] = row
	f.qb.tagRemoveErr = nil
	if err := cycle.reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if stored := readTjuptTagFixture(t, f); !stored.TagUnified || !stored.TagCleaned ||
		!reflect.DeepEqual(f.qb.tagAttempts, []string{f.task.Hash}) || !reflect.DeepEqual(f.qb.pruned, []string{f.task.Marker}) ||
		f.qb.rows[f.task.Hash].Tags != downloaderclient.TjuptFeedDisplayTag+",custom" {
		t.Fatalf("could not reconcile the durable label cutover: %+v, %+v", stored, f.qb)
	}
}

func TestTjuptFeedUnificationLeaseLossPreservesOldMarker(t *testing.T) {
	f := newTjuptCleanupFixture(t)
	ctx := context.Background()
	cycle := tjuptFeedCycle{repo: f.feed.repo, lease: f.lease, scope: f.task.Scope, downloader: f.qb}
	f.qb.afterEnsure = func() {
		if err := f.feed.repo.Finish(ctx, f.lease, "lease revoked"); err != nil {
			t.Fatal(err)
		}
	}
	f.qb.beforeTagRemove = func() { t.Fatal("expired lease was able to remove the unique marker") }
	if err := cycle.reconcile(ctx); !errors.Is(err, repository.ErrTjuptFeedLease) {
		t.Fatalf("expected lease-fenced persistence failure, got %v", err)
	}
	stored := readTjuptTagFixture(t, f)
	current := f.qb.rows[f.task.Hash]
	if stored.TagUnified || stored.TagCleaned || !tjuptHasTag(current.Tags, f.task.Marker) || !tjuptHasTag(current.Tags, downloaderclient.TjuptFeedDisplayTag) {
		t.Fatalf("ownership became ambiguous after lease loss: %+v, %q", stored, current.Tags)
	}
}

func TestTjuptFeedDoesNotAdoptManuallyLabeledTasks(t *testing.T) {
	f := newTjuptCleanupFixture(t)
	cycle := tjuptFeedCycle{repo: f.feed.repo, lease: f.lease, scope: f.task.Scope, downloader: f.qb}
	row := f.qb.rows[f.task.Hash]
	row.Tags = downloaderclient.TjuptFeedDisplayTag
	f.qb.rows[f.task.Hash] = row
	if err := cycle.reconcile(context.Background()); err != nil || cycle.held != 1 {
		t.Fatalf("unproven public label was not held: %v, %d", err, cycle.held)
	}
	if stored := readTjuptTagFixture(t, f); stored.TagUnified || stored.TagCleaned || len(f.qb.tagAttempts) != 0 || len(f.qb.removes) != 0 {
		t.Fatalf("manual label was treated as a confirmed original task: %+v, %+v", stored, f.qb)
	}
	for _, task := range []repository.TjuptFeedTask{
		{Hash: f.task.Hash, Scope: f.task.Scope, Marker: f.task.Marker},
		{Hash: f.task.Hash, Scope: f.task.Scope, Marker: f.task.Marker, TagUnified: true},
		{Hash: f.task.Hash, Marker: f.task.Marker, TagUnified: true, AddedOn: row.AddedOn},
		{Hash: f.task.Hash, Scope: f.task.Scope, Marker: f.task.Marker, TagUnified: true, AddedOn: row.AddedOn + 1},
	} {
		if tjuptFeedReceiptMatches(task, &row) {
			t.Fatalf("shared-label-only receipt adopted without exact durable scope: %+v", task)
		}
	}
	if tjuptFeedOwnershipTag(repository.TjuptFeedTask{Marker: downloaderclient.TjuptFeedDisplayTag}) != "" {
		t.Fatal("unconfirmed configured marker was allowed to impersonate the shared-label sentinel")
	}
}

func TestTjuptFeedCleanupUsesLedgerBackedSharedLabel(t *testing.T) {
	f := newTjuptCleanupFixture(t)
	cycle := tjuptFeedCycle{repo: f.feed.repo, lease: f.lease, scope: f.task.Scope, downloader: f.qb}
	if err := cycle.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if row := f.qb.rows[f.task.Hash]; row.Tags != downloaderclient.TjuptFeedDisplayTag {
		t.Fatalf("auto cleanup fixture was not normalized: %q", row.Tags)
	}
	f.sample(t, 16, 1)
	if f.calls != 2 || !reflect.DeepEqual(f.qb.removeMarkers, []string{downloaderclient.TjuptFeedDisplayTag}) {
		t.Fatalf("cleanup lost its H&R fence or ledger ownership, checks=%d, markers=%v", f.calls, f.qb.removeMarkers)
	}
}

func TestTjuptFeedPrunesOnlyEmptyArchivedLedgerMarkers(t *testing.T) {
	f := newTjuptCleanupFixture(t)
	ctx := context.Background()
	cycle := tjuptFeedCycle{repo: f.feed.repo, lease: f.lease, scope: f.task.Scope, downloader: f.qb}
	archive := f.task
	archive.State = repository.TjuptFeedGone
	if err := cycle.saveTask(ctx, archive); err != nil {
		t.Fatal(err)
	}
	foreign := tjuptFeedMarkerPrefix + strings.Repeat("7", 24)
	row := f.qb.rows[f.task.Hash]
	row.Tags = f.task.Marker + "," + foreign
	f.qb.rows[f.task.Hash] = row
	if err := cycle.reconcile(ctx); err != nil || len(f.qb.pruned) != 0 {
		t.Fatalf("still used marker was pruned: %v, %v", err, f.qb.pruned)
	}
	row.Tags = foreign
	f.qb.rows[f.task.Hash] = row
	for range 2 {
		if err := cycle.reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(f.qb.pruned, []string{f.task.Marker}) || f.qb.rows[f.task.Hash].Tags != foreign {
		t.Fatalf("expected only one safe prune of the former ledger marker: %v", f.qb.pruned)
	}
}

func TestTjuptFeedTagMigrationUsesBoundedFairBatches(t *testing.T) {
	f := newTjuptCleanupFixture(t)
	ctx := context.Background()
	cycle := tjuptFeedCycle{repo: f.feed.repo, lease: f.lease, scope: f.task.Scope, downloader: f.qb}
	for i := 1; i <= tjuptFeedTagBatch; i++ {
		task := f.task
		task.Hash, task.Marker, task.SourceID = fmt.Sprintf("%040x", i), fmt.Sprintf("%s%024x", tjuptFeedMarkerPrefix, i), fmt.Sprint(i)
		if ok, err := cycle.repo.Reserve(ctx, cycle.lease, time.Now().Unix(), task, 20*tjuptGiB, 0); err != nil || !ok {
			t.Fatalf("could not seed task %d: %t, %v", i, ok, err)
		}
		f.qb.rows[task.Hash] = downloaderclient.TaggedQBTorrent{Hash: task.Hash, AddedOn: task.AddedOn, State: "stalledUP", Tags: tjuptTag + "," + task.Marker}
	}
	f.qb.tagRemoveErr = downloaderclient.ErrTjuptFeedUnconfirmed
	if err := cycle.reconcile(ctx); err != nil || len(f.qb.tagAttempts) != tjuptFeedTagBatch {
		t.Fatalf("tag reconciliation exceeded batch capacity: %v, %d", err, len(f.qb.tagAttempts))
	}
	if err := cycle.reconcile(ctx); err != nil || len(f.qb.tagAttempts) != tjuptFeedTagBatch+1 || f.qb.tagAttempts[tjuptFeedTagBatch] != f.task.Hash {
		t.Fatalf("unconfirmed removals starved the later migration: %v, %v", err, f.qb.tagAttempts)
	}
}

func TestTjuptFeedResumesConfirmedLegacyTrialWithSharedLabel(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	configureTjuptFeedTest(t, feed)
	ctrl, err := feed.repo.Control(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := feed.runConfig(ctrl)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := tjuptFeedTestScope(plugin)
	if err != nil {
		t.Fatal(err)
	}
	oldScope := tjuptFeedDigest(scope, cfg.rss, "starpt-tjupt, seed, personal", cfg.Category, cfg.SavePath,
		fmt.Sprintf("%g/%g/%g/%d", cfg.MinGiB, cfg.MaxGiB, cfg.TotalLimitGiB, cfg.ActiveLimit))
	task := repository.TjuptFeedTask{Hash: strings.Repeat("a", 40), Scope: scope, TrialScope: oldScope, SourceID: "55",
		State: repository.TjuptFeedStopped, Marker: tjuptFeedMarkerPrefix + strings.Repeat("0", 24), AddedOn: 100, SizeBytes: tjuptGiB}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	progress := float64(0)
	qb := &tjuptFeedFakeQB{rows: map[string]downloaderclient.TaggedQBTorrent{task.Hash: {
		Hash: task.Hash, AddedOn: task.AddedOn, State: "stoppedDL", Category: cfg.Category, Progress: &progress,
		Tags: "starpt-tjupt, personal, " + task.Marker,
	}}}
	attachTjuptFeedTest(feed, &tjuptFeedFakeSite{}, qb)
	if err := feed.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, err := feed.run(ctx, false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(qb.starts, []string{task.Hash}) || qb.rows[task.Hash].Tags != "personal,"+downloaderclient.TjuptFeedDisplayTag {
		t.Fatalf("old confirmed trial did not keep valid ownership across label cutover: %+v, %v", qb.rows[task.Hash], qb.starts)
	}
	stored, err := feed.repo.LiveTasks(ctx, scope)
	if err != nil || len(stored) != 1 || !stored[0].TagCleaned || stored[0].State != repository.TjuptFeedStarted {
		t.Fatalf("could not reconcile old trial after restart: %+v, %v", stored, err)
	}
}
