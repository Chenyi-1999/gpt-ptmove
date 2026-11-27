package plugins

import (
	"context"
	"errors"
	"testing"
)

func fixtureTjuptFeedManifest() PackageManifest {
	return PackageManifest{
		SchemaVersion: 2,
		ID:            TjuptFeedPluginID,
		Name:          "北洋园刷流",
		Version:       "1.0.0",
		Capabilities:  []string{TjuptFeedCapability},
		Actions: []Action{
			{ID: "preview", Label: "预览"},
			{ID: "status", Label: "H&R 状态"},
			{ID: "trial", Label: "试运行"},
			{ID: "start", Label: "开始"},
			{ID: "stop", Label: "停止"},
		},
	}
}

func TestTjuptFeedManifestRequiresExactHostContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*PackageManifest)
	}{
		{"other identity", func(m *PackageManifest) { m.ID = "override" }},
		{"missing capability", func(m *PackageManifest) { m.Capabilities = nil }},
		{"different capability", func(m *PackageManifest) { m.Capabilities = []string{DownloaderSummaryCapability} }},
		{"extra capability", func(m *PackageManifest) { m.Capabilities = append(m.Capabilities, DownloaderSummaryCapability) }},
		{"guest settings", func(m *PackageManifest) { m.Fields = []Field{{Key: "token", Label: "Token", Type: "string"}} }},
		{"missing action", func(m *PackageManifest) { m.Actions = m.Actions[:4] }},
		{"unrecognized action", func(m *PackageManifest) { m.Actions[4].ID = "delete" }},
		{"duplicate action", func(m *PackageManifest) { m.Actions[4] = m.Actions[0] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fixtureTjuptFeedManifest()
			tc.mutate(&manifest)
			if err := normalizeManifest(&manifest, manifest.ID); err == nil {
				t.Fatal("invalid 北洋园 action contract was accepted")
			}
		})
	}
}

func TestTjuptFeedHostActionBoundToPluginAndRequest(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		id     string
		output string
		want   bool
	}{
		{"matching action", TjuptFeedPluginID, `{"host_action":"preview","blocks":[]}`, true},
		{"different action", TjuptFeedPluginID, `{"host_action":"trial"}`, false},
		{"missing action", TjuptFeedPluginID, `{"message":"preview"}`, false},
		{"raw endpoint", TjuptFeedPluginID, `{"host_action":"/api/plugins/tjupt/feed/trial"}`, false},
		{"unprivileged plugin", "ordinary", `{"host_action":"preview","message":"report"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			manifest := fixtureTjuptFeedManifest()
			if tc.id != TjuptFeedPluginID {
				manifest = fixturePackageManifest(tc.id, "1")
				manifest.Actions[0].ID = "preview"
			}
			writeTestPackage(t, dataDir, manifest, fixtureWASM(tc.output, nil), "plugin.wasm")
			manager := testPackageManager(t, dataDir)
			snapshot, err := manager.Refresh(ctx)
			if err != nil || len(snapshot.Items) != 1 || len(snapshot.Issues) != 0 {
				t.Fatalf("refresh: %+v, %v", snapshot, err)
			}
			for _, source := range []*PackageManager{manager, testPackageManager(t, dataDir)} {
				got, err := source.Invoke(ctx, tc.id, "preview", nil)
				if tc.want {
					if err != nil || got.HostAction != "preview" {
						t.Fatalf("invoke: %+v, %v", got, err)
					}
				} else if !errors.Is(err, ErrPackageExecution) || got.HostAction != "" {
					t.Fatalf("unauthorized action was returned: %+v, %v", got, err)
				}
			}
		})
	}
}

func TestTjuptFeedRestoreRejectsInvalidHostContract(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	manifest := fixtureTjuptFeedManifest()
	writeTestPackage(t, dataDir, manifest, fixtureWASM(`{"host_action":"preview"}`, nil), "plugin.wasm")
	manager := testPackageManager(t, dataDir)
	if snapshot, err := manager.Refresh(ctx); err != nil || len(snapshot.Items) != 1 {
		t.Fatalf("refresh: %+v, %v", snapshot, err)
	}
	record := manager.active[manifest.ID].record
	record.Manifest.Actions[0].ID = "delete"
	if err := writePackageState(manager.statePath(), savedPackageState{
		Version:     1,
		Active:      map[string]cachedPackage{manifest.ID: record},
		Preferences: manager.preferences,
	}); err != nil {
		t.Fatal(err)
	}
	if snapshot := testPackageManager(t, dataDir).List(); len(snapshot.Items) != 0 || len(snapshot.Issues) != 1 {
		t.Fatalf("invalid cached contract was accepted: %+v", snapshot)
	}
}
