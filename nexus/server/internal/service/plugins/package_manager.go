package plugins

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
)

const (
	maxInstalledPackages = 16
	maxSavedPreferences  = 256
)

type installedPackage struct {
	record   cachedPackage
	compiled *packageModule
}

// PackageManager holds the last applied registry. Only Refresh reads root.
// The immutable module cache and a single atomically replaced state file are
// the source of truth on startup.
type PackageManager struct {
	mu          sync.RWMutex
	root        string
	cache       string
	reserved    map[string]bool
	runtime     *packageRuntime
	active      map[string]*installedPackage
	preferences map[string]packagePreferences
	issues      []Issue
}

func NewPackageManager(ctx context.Context, dataDir string, reserved ...string) (*PackageManager, error) {
	runtime, err := newPackageRuntime(ctx)
	if err != nil {
		return nil, err
	}
	m := &PackageManager{
		root:        filepath.Join(dataDir, "plugins"),
		cache:       filepath.Join(dataDir, "plugin-package-cache"),
		reserved:    make(map[string]bool, len(reserved)),
		runtime:     runtime,
		active:      make(map[string]*installedPackage),
		preferences: make(map[string]packagePreferences),
	}
	for _, id := range reserved {
		m.reserved[id] = true
	}
	m.restore(ctx)
	return m, nil
}

func (m *PackageManager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runtime.close(ctx)
}

func (m *PackageManager) List() PackageSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot()
}

func (m *PackageManager) Get(id string) (PackageInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pkg, ok := m.active[id]
	if !ok {
		return PackageInfo{}, ErrPackageNotFound
	}
	return m.info(pkg), nil
}

func (m *PackageManager) Refresh(ctx context.Context) (PackageSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := manifestEntries(m.root)
	if err != nil {
		return m.snapshot(), err
	}
	sources := make([]string, 0)
	next := make(map[string]*installedPackage)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".zip") {
			sources = append(sources, name)
			id := strings.TrimSuffix(name, ".zip")
			if previous := m.active[id]; previous != nil {
				next[id] = previous
			}
		}
	}

	currentModules := modulesByDigest(m.active)
	candidateModules := maps.Clone(currentModules)
	preferences := maps.Clone(m.preferences)
	var issues []Issue
	useCandidates := false
	defer func() {
		if !useCandidates {
			closeUnusedModules(candidateModules, currentModules)
		}
	}()
	for _, filename := range sources {
		if err := ctx.Err(); err != nil {
			return m.snapshot(), err
		}
		id := strings.TrimSuffix(filename, ".zip")
		pkg, err := m.loadSource(ctx, id, filename, next, preferences, candidateModules)
		if err != nil {
			issues = append(issues, Issue{File: filename, Message: err.Error()})
			continue
		}
		next[id] = pkg
		if _, existed := preferences[id]; !existed {
			preferences[id] = packagePreferences{Enabled: true}
		}
	}
	if err := ctx.Err(); err != nil {
		return m.snapshot(), err
	}
	changed := !maps.Equal(next, m.active)
	// Refresh also restores the saved state from memory if the cache was lost.
	if changed || len(next) != 0 || len(preferences) != 0 {
		if err := m.persist(next, preferences); err != nil {
			return m.snapshot(), err
		}
	}
	if changed {
		m.active, m.preferences = next, preferences
		m.pruneCache()
	}
	m.issues = issues
	useCandidates = true
	closeUnusedModules(candidateModules, modulesByDigest(m.active))
	return m.snapshot(), nil
}

func (m *PackageManager) loadSource(ctx context.Context, id, name string, next map[string]*installedPackage,
	preferences map[string]packagePreferences, modules map[string]*packageModule,
) (*installedPackage, error) {
	if !pluginID.MatchString(id) || m.reserved[id] {
		return nil, errors.New("文件名不受支持或插件 ID 已被占用")
	}
	if next[id] == nil && len(next) >= maxInstalledPackages {
		return nil, errors.New("已达到已装载插件数量上限")
	}
	if _, exists := preferences[id]; !exists && len(preferences) >= maxSavedPreferences {
		return nil, errors.New("已达到插件配置数量上限")
	}

	content, err := readPackageFile(filepath.Join(m.root, name), maxPackageBytes)
	if err != nil {
		return nil, errors.New("无法读取 ZIP，须为不超过 8 MiB 的普通文件")
	}
	zipDigest := packageDigest(content)
	previous := next[id]
	unchanged := previous != nil && previous.record.ZipDigest == zipDigest
	if unchanged {
		if _, err := m.readApprovedCode(previous.record.Digest); err == nil {
			return previous, nil
		}
	}
	manifest, module, err := readPackageZip(content, id)
	if err != nil {
		return nil, err
	}
	digest := packageDigest(module)
	compiled, err := m.compile(ctx, digest, module, modules)
	if err != nil {
		return nil, err
	}
	if err := m.cacheModule(digest, module); err != nil {
		return nil, errors.New("保存已校验的插件版本失败")
	}
	if unchanged && previous.record.Digest == digest {
		return previous, nil
	}
	return &installedPackage{record: cachedPackage{manifest, digest, zipDigest}, compiled: compiled}, nil
}

func (m *PackageManager) SetEnabled(id string, enabled bool) (PackageInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pkg := m.active[id]
	if pkg == nil {
		return PackageInfo{}, ErrPackageNotFound
	}
	preference := m.preferences[id]
	if preference.Enabled != enabled {
		preference.Enabled = enabled
		if err := m.savePreference(id, preference); err != nil {
			return PackageInfo{}, err
		}
	}
	return m.info(pkg), nil
}

func (m *PackageManager) SaveSettings(id string, settings map[string]any) (PackageInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pkg := m.active[id]
	if pkg == nil {
		return PackageInfo{}, ErrPackageNotFound
	}
	fields := make(map[string]Field)
	for _, field := range pkg.record.Manifest.Fields {
		fields[field.Key] = field
	}
	preference := m.preferences[id]
	values := make(map[string]any, len(preference.Settings)+len(fields))
	maps.Copy(values, preference.Settings)
	for key, v := range settings {
		value, ok := normalizeSetting(fields[key], v)
		if !ok {
			return PackageInfo{}, fmt.Errorf("%w：字段 %s", ErrPackageSettings, key)
		}
		values[key] = value
	}
	preference.Settings = values
	if err := m.savePreference(id, preference); err != nil {
		return PackageInfo{}, err
	}
	return m.info(pkg), nil
}

func (m *PackageManager) Invoke(ctx context.Context, id, action string, resources map[string]any) (ActionResult, error) {
	select {
	case m.runtime.slots <- struct{}{}:
		defer func() { <-m.runtime.slots }()
	default:
		return ActionResult{}, fmt.Errorf("%w：达到并发上限", ErrPackageExecution)
	}
	// Keep the selected version and its enabled state stable for the bounded
	// invocation. Removing or disabling waits for already admitted work.
	m.mu.RLock()
	defer m.mu.RUnlock()
	pkg := m.active[id]
	if pkg == nil {
		return ActionResult{}, ErrPackageNotFound
	}
	info := m.info(pkg)
	if !info.Enabled {
		return ActionResult{}, ErrPackageDisabled
	}
	if !slices.ContainsFunc(info.Actions, func(candidate Action) bool { return candidate.ID == action }) {
		return ActionResult{}, ErrPackageAction
	}
	input, err := guestInput(info, action, resources)
	if err != nil {
		return ActionResult{}, fmt.Errorf("%w：输入无法转换", ErrPackageExecution)
	}
	var code []byte
	if m.runtime.executable != "" {
		code, err = m.readApprovedCode(pkg.record.Digest)
		if err != nil {
			return ActionResult{}, fmt.Errorf("%w：%s", ErrPackageExecution, err)
		}
	}
	return m.runtime.execute(ctx, pkg.compiled, code, input)
}

func (m *PackageManager) snapshot() PackageSnapshot {
	items := make([]PackageInfo, 0, len(m.active))
	for _, pkg := range m.active {
		items = append(items, m.info(pkg))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return PackageSnapshot{Items: items, Issues: append([]Issue{}, m.issues...)}
}

func (m *PackageManager) info(pkg *installedPackage) PackageInfo {
	manifest := pkg.record.Manifest
	manifest.Fields = append([]Field{}, manifest.Fields...)
	for i := range manifest.Fields {
		manifest.Fields[i].Options = slices.Clone(manifest.Fields[i].Options)
	}
	manifest.Actions = slices.Clone(manifest.Actions)
	manifest.Capabilities = append([]string{}, manifest.Capabilities...)
	preference, saved := m.preferences[manifest.ID]
	return PackageInfo{
		PackageManifest: manifest,
		Kind:            "wasm",
		Enabled:         !saved || preference.Enabled,
		Settings:        effectiveSettings(manifest, preference.Settings),
	}
}

func (m *PackageManager) savePreference(id string, preference packagePreferences) error {
	preferences := maps.Clone(m.preferences)
	preferences[id] = preference
	if err := m.persist(m.active, preferences); err != nil {
		return err
	}
	m.preferences = preferences
	return nil
}

func modulesByDigest(packages map[string]*installedPackage) map[string]*packageModule {
	result := make(map[string]*packageModule)
	for _, pkg := range packages {
		result[pkg.record.Digest] = pkg.compiled
	}
	return result
}

func closeUnusedModules(known, retained map[string]*packageModule) {
	for digest, module := range known {
		if retained[digest] == nil {
			module.close()
		}
	}
}

func (m *PackageManager) compile(ctx context.Context, digest string, module []byte,
	cache map[string]*packageModule,
) (*packageModule, error) {
	if found := cache[digest]; found != nil {
		return found, nil
	}
	compiled, err := m.runtime.compile(ctx, module)
	if err == nil {
		cache[digest] = compiled
	}
	return compiled, err
}

func (m *PackageManager) persist(packages map[string]*installedPackage, preferences map[string]packagePreferences) error {
	active := make(map[string]cachedPackage, len(packages))
	for id, pkg := range packages {
		active[id] = pkg.record
	}
	if err := writePackageState(m.statePath(), savedPackageState{1, active, preferences}); err != nil {
		return errors.New("无法更新插件缓存，现有插件保持可用")
	}
	return nil
}

func (m *PackageManager) statePath() string {
	return filepath.Join(m.cache, "state.json")
}

func (m *PackageManager) modulePath(digest string) string {
	return filepath.Join(m.cache, digest+".wasm")
}

func (m *PackageManager) cacheModule(digest string, data []byte) error {
	path := m.modulePath(digest)
	if current, err := readPackageFile(path, maxModuleBytes); err == nil && packageDigest(current) == digest {
		return nil
	}
	return atomicPackageWrite(path, data)
}

func (m *PackageManager) restore(ctx context.Context) {
	data, err := readPackageFile(m.statePath(), maxStateBytes)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var state savedPackageState
	if err != nil || decodePackageJSON(data, &state) != nil || state.Version != 1 ||
		len(state.Active) > maxInstalledPackages || len(state.Preferences) > maxSavedPreferences {
		m.issues = append(m.issues, Issue{File: "插件缓存", Message: "上次加载状态无法读取，请手动刷新插件"})
		return
	}
	if state.Preferences != nil {
		m.preferences = state.Preferences
	}
	modules := make(map[string]*packageModule)
	for id, record := range state.Active {
		if err := m.restoreModule(ctx, id, record, modules); err != nil {
			m.issues = append(m.issues, Issue{File: id + ".zip", Message: err.Error()})
		}
	}
	sort.Slice(m.issues, func(i, j int) bool { return m.issues[i].File < m.issues[j].File })
}

func (m *PackageManager) restoreModule(ctx context.Context, id string, record cachedPackage,
	modules map[string]*packageModule,
) error {
	if m.reserved[id] || !validPackageDigest(record.Digest) || !validPackageDigest(record.ZipDigest) ||
		normalizeManifest(&record.Manifest, id) != nil {
		return errors.New("缓存的插件清单无效")
	}
	binary, err := m.readApprovedCode(record.Digest)
	if err != nil {
		return err
	}
	compiled, err := m.compile(ctx, record.Digest, binary, modules)
	if err != nil {
		return err
	}
	m.active[id] = &installedPackage{record: record, compiled: compiled}
	return nil
}

func (m *PackageManager) readApprovedCode(digest string) ([]byte, error) {
	binary, err := readPackageFile(m.modulePath(digest), maxModuleBytes)
	if err != nil || packageDigest(binary) != digest {
		return nil, errors.New("已加载的 WASM 缓存损坏，请手动刷新插件")
	}
	return binary, nil
}

func (m *PackageManager) pruneCache() {
	entries, err := os.ReadDir(m.cache)
	if err != nil {
		return
	}
	used := modulesByDigest(m.active)
	for _, entry := range entries {
		name := entry.Name()
		if digest, ok := strings.CutSuffix(name, ".wasm"); ok && validPackageDigest(digest) &&
			entry.Type().IsRegular() && used[digest] == nil {
			_ = os.Remove(m.modulePath(digest))
		}
	}
}
