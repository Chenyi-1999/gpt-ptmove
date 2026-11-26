package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReloadAddsAndRemovesInstalledPlugins(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "plugins")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	catalog := New(root)
	name := filepath.Join(directory, "ops.json")
	const valid = `{"schema_version":1,"id":"ops","name":"运维工具","version":"1.0","blocks":[
		{"kind":"link","title":"任务记录","target":"/publish-logs"},
		{"kind":"metric","title":"转种资料","metric":"transfer_drafts"}]}`
	if err := os.WriteFile(name, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(directory, "bad.json")
	if err := os.WriteFile(invalid, []byte(`{"schema_version":1,"id":"bad","name":"bad","version":"1","blocks":[{"kind":"link","title":"请求","target":"//81.70.24.5/"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := catalog.Reload()
	if err != nil || len(s.Items) != 1 || s.Items[0].ID != "ops" || len(s.Issues) != 1 {
		t.Fatalf("unexpected plugin catalog: %+v %v", s, err)
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if s, err := catalog.Reload(); err != nil || len(s.Items) != 0 || len(s.Issues) != 1 {
		t.Fatalf("removed plugin stayed loaded: %+v %v", s, err)
	}
}

func TestManifestRejectsExecutableFieldAndSymlink(t *testing.T) {
	head := t.TempDir()
	source := filepath.Join(head, "plugin.json")
	raw := []byte(`{"schema_version":1,"id":"demo","name":"demo","version":"1","script":"command","blocks":[{"kind":"text","title":"demo","text":"示例"}]}`)
	if err := os.WriteFile(source, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest(source); err == nil {
		t.Fatal("plugin accepted an executable field")
	}
	link := filepath.Join(head, "link.json")
	if err := os.Symlink(source, link); err == nil {
		if _, err := readManifest(link); err == nil {
			t.Fatal("plugin catalog followed a symlink")
		}
	}
}

func TestCatalogRestartsFromApprovedCacheUntilManualRefresh(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "plugins")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "ops.json")
	raw := []byte(`{"schema_version":1,"id":"ops","name":"Installed","version":"1","blocks":[{"kind":"text","title":"Data","text":"Result"}]}`)
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	first := New(root)
	if got := first.List().Items; len(got) != 0 {
		t.Fatalf("construction scanned the plugin directory: %+v", got)
	}
	if got, err := first.Reload(); err != nil || len(got.Items) != 1 {
		t.Fatalf("manual refresh failed: %+v %v", got, err)
	}
	if err := os.WriteFile(file, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	restarted := New(root)
	if got := restarted.List().Items; len(got) != 1 || got[0].Name != "Installed" {
		t.Fatalf("restart failed to restore cached plugin: %+v", got)
	}
	if got, err := restarted.Reload(); err != nil || len(got.Items) != 1 || len(got.Issues) != 1 {
		t.Fatalf("invalid update replaced the cached plugin: %+v %v", got, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.Reload(); err != nil || len(got.Items) != 0 {
		t.Fatalf("remove and refresh failed: %+v %v", got, err)
	}
	if got := New(root).List().Items; len(got) != 0 {
		t.Fatalf("removed plugin was restored: %+v", got)
	}
}
