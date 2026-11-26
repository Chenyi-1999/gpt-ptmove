package plugins

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const maxLegacyCacheBytes = maxManifestBytes * maxDirectoryEntries

// restoreLegacy reconstructs the catalog from the last accepted refresh.
// The operator's installation directory is only read by Reload.
func (c *Catalog) restoreLegacy() {
	f, err := os.Open(c.cache)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		c.snapshot.Issues = append(c.snapshot.Issues, Issue{File: "legacy.json", Message: "无法读取插件清单缓存"})
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxLegacyCacheBytes+1))
	var installed []Manifest
	if err != nil || len(raw) > maxLegacyCacheBytes || json.Unmarshal(raw, &installed) != nil || len(installed) > maxDirectoryEntries {
		c.snapshot.Issues = append(c.snapshot.Issues, Issue{File: "legacy.json", Message: "插件清单缓存无效，请手动刷新"})
		return
	}
	seen := make(map[string]bool)
	for _, builtin := range c.builtins {
		seen[builtin.ID] = true
	}
	for _, manifest := range installed {
		if seen[manifest.ID] || validate(manifest) != nil {
			c.snapshot.Issues = append(c.snapshot.Issues, Issue{File: manifest.ID + ".json", Message: "插件清单缓存条目无效"})
			continue
		}
		seen[manifest.ID] = true
		c.snapshot.Items = append(c.snapshot.Items, manifest)
	}
}

func (c *Catalog) saveLegacy(next Snapshot) error {
	items := next.Items[len(c.builtins):]
	if len(items) == 0 {
		if err := os.Remove(c.cache); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	body, err := json.Marshal(items)
	if err != nil {
		return err
	}
	dir := filepath.Dir(c.cache)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".legacy-*.tmp")
	if err != nil {
		return err
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(path, c.cache)
}
