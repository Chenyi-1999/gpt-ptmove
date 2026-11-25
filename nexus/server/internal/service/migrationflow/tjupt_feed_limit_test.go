package migrationflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/acquire/tjupt"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

func TestTjuptFeedTaskLimitSavePreservesOmittedValue(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	input := configureTjuptFeedTest(t, feed)
	for _, test := range []struct {
		field string
		want  int
	}{
		{field: `"active_limit":75`, want: 75},
		{field: `"active_limit":null`, want: 75},
		{field: `"category":"RSS"`, want: 75},
		{field: `"active_limit":0`, want: 0},
		{field: `"active_limit":-1`, want: -1},
	} {
		var setting TjuptFeedInput
		if err := json.Unmarshal([]byte("{"+test.field+"}"), &setting); err != nil {
			t.Fatal(err)
		}
		input.ActiveLimit = setting.ActiveLimit
		err := feed.Save(ctx, input)
		if test.want < 0 {
			if !errors.Is(err, ErrTjuptFeedConfig) {
				t.Fatalf("negative limit returned %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		view, err := feed.View(ctx)
		if err != nil || view.Settings.ActiveLimit != test.want {
			t.Fatalf("%s yielded %d, expected %d: %v", test.field, view.Settings.ActiveLimit, test.want, err)
		}
	}
}

func TestTjuptFeedCountAndVolumeLimitsAboveFifty(t *testing.T) {
	for _, tc := range []struct {
		name     string
		limit    int
		totalGiB float64
		added    int
		message  string
	}{
		{name: "unlimited count", totalGiB: 60, added: 2},
		{name: "configured count above fifty", limit: 52, totalGiB: 60, added: 1, message: "已达到在管任务数上限"},
		{name: "unlimited count retains volume limit", totalGiB: 52, added: 1, message: "已达到在管总容量上限"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, plugin, dir := newTjuptFeedTest(t)
			feed, err := NewTjuptFeed(db, plugin, dir)
			if err != nil {
				t.Fatal(err)
			}
			input := configureTjuptFeedTest(t, feed)
			input.ActiveLimit, input.TotalLimitGiB = &tc.limit, tc.totalGiB
			if err := feed.Save(ctx, input); err != nil {
				t.Fatal(err)
			}

			qb := &tjuptFeedFakeQB{rows: make(map[string]downloaderclient.TaggedQBTorrent)}
			receipts := make([]repository.TjuptFeedTask, 0, 51)
			const addedOn = 100
			scope, err := tjuptFeedTestScope(plugin)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 51; i++ {
				hash := fmt.Sprintf("%040x", i+1)
				marker := fmt.Sprintf("%s%024x", tjuptFeedMarkerPrefix, i)
				receipts = append(receipts, repository.TjuptFeedTask{
					Hash: hash, SourceID: fmt.Sprint(100 + i), Scope: scope,
					AddedOn: addedOn, State: repository.TjuptFeedStarted, Marker: marker, SizeBytes: tjuptGiB,
				})
				qb.rows[hash] = downloaderclient.TaggedQBTorrent{Hash: hash, AddedOn: addedOn, State: "uploading", Tags: tjuptTag + "," + marker}
			}
			if err := db.Create(&receipts).Error; err != nil {
				t.Fatal(err)
			}
			site := &tjuptFeedFakeSite{items: []tjupt.Torrent{
				{Hash: strings.Repeat("a", 40), SizeBytes: tjuptGiB, Data: []byte{1}},
				{Hash: strings.Repeat("b", 40), SizeBytes: tjuptGiB, Data: []byte{1}},
			}}
			attachTjuptFeedTest(feed, site, qb)
			if err := feed.SetEnabled(ctx, true); err != nil {
				t.Fatal(err)
			}
			result, err := feed.run(ctx, false)
			if err != nil || result.Added != tc.added || len(qb.adds) != tc.added || !strings.Contains(result.Message, tc.message) {
				t.Fatalf("unexpected run %#v: %v", result, err)
			}
			if len(qb.lookupCalls) != 1 || len(qb.lookupCalls[0]) != 51 {
				t.Fatalf("reconciliation did not use a bulk snapshot: %#v", qb.lookupCalls)
			}
		})
	}
}

func TestTjuptFeedBulkLookupErrorDoesNotReleaseCapacity(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	configureTjuptFeedTest(t, feed)
	scope, err := tjuptFeedTestScope(plugin)
	if err != nil {
		t.Fatal(err)
	}
	task := repository.TjuptFeedTask{
		Hash: strings.Repeat("a", 40), SourceID: "123", Scope: scope, State: repository.TjuptFeedStarted,
		AddedOn: 100, SizeBytes: tjuptGiB,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	site := &tjuptFeedFakeSite{items: []tjupt.Torrent{
		{Hash: strings.Repeat("b", 40), SizeBytes: tjuptGiB, Data: []byte{1}},
	}}
	lookupErr := errors.New("partial qB snapshot")
	qb := &tjuptFeedFakeQB{rows: make(map[string]downloaderclient.TaggedQBTorrent), lookupErr: lookupErr}
	attachTjuptFeedTest(feed, site, qb)
	if result, err := feed.Trial(ctx); !errors.Is(err, lookupErr) || result.Added != 0 || len(qb.adds) != 0 {
		t.Fatalf("failed qB snapshot was handled as absence: %#v, %v", result, err)
	}
	var current repository.TjuptFeedTask
	if err := db.First(&current, "hash = ?", task.Hash).Error; err != nil || current.State != repository.TjuptFeedStarted {
		t.Fatalf("failed qB snapshot modified original task: %#v, %v", current, err)
	}
	qb.lookupErr = nil
	if result, err := feed.Trial(ctx); err != nil || result.Added != 1 || len(qb.adds) != 1 {
		t.Fatalf("complete snapshot with proven absent task did not release capacity: %#v, %v", result, err)
	}
}

func TestTjuptFeedAtomicVolumeReserveWithNoCountLimit(t *testing.T) {
	ctx := context.Background()
	db, plugin, dir := newTjuptFeedTest(t)
	feed, err := NewTjuptFeed(db, plugin, dir)
	if err != nil {
		t.Fatal(err)
	}
	control, err := feed.repo.Control(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	const lease = "test-unlimited-lease"
	if claimed, err := feed.repo.Claim(ctx, control, lease, true, true, false, now, now+60); err != nil || !claimed {
		t.Fatalf("claim: %t, %v", claimed, err)
	}
	type reservation struct {
		allowed bool
		err     error
	}
	pending := make(chan reservation, 2)
	for i := 0; i < 2; i++ {
		task := repository.TjuptFeedTask{
			Hash: fmt.Sprintf("%040x", i+1), SourceID: fmt.Sprint(i + 1), Scope: "test", State: repository.TjuptFeedAdding, SizeBytes: 2,
		}
		go func() {
			allowed, err := feed.repo.Reserve(ctx, lease, now, task, 3, 0)
			pending <- reservation{allowed: allowed, err: err}
		}()
	}
	allowedCount := 0
	for i := 0; i < 2; i++ {
		result := <-pending
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.allowed {
			allowedCount++
		}
	}
	if allowedCount != 1 {
		t.Fatalf("the atomic volume limit allowed %d reservations, expected one", allowedCount)
	}
}

func tjuptFeedTestScope(plugin *TjuptRSSService) (string, error) {
	d, err := plugin.downloader("qb")
	if err != nil {
		return "", err
	}
	return tjuptFeedDigest(d.ID, d.Host, d.Username), nil
}
