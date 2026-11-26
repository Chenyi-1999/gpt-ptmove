package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testPackageManager(t *testing.T, dataDir string) *PackageManager {
	t.Helper()
	manager, err := NewPackageManager(context.Background(), dataDir, "builtin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return manager
}

func TestPackageManualRefreshAndRestore(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	first := testPackageManager(t, dataDir)
	plugin := fixturePackageManifest("sample", "1.0")
	path := writeTestPackage(t, dataDir, plugin, fixtureWASM(`{"message":"first","blocks":[{"kind":"metric","title":"Active","value":"2"}]}`, nil), "plugin.wasm")
	before := testPackageManager(t, dataDir)
	if _, err := before.Get("sample"); !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("startup should not scan source: %v", err)
	}
	if _, err := first.Get("sample"); !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("Get should not scan source: %v", err)
	}
	snapshot, err := first.Refresh(ctx)
	if err != nil || len(snapshot.Issues) != 0 || len(snapshot.Items) != 1 {
		t.Fatalf("refresh: %+v, %v", snapshot, err)
	}
	if info := snapshot.Items[0]; !info.Enabled || info.Kind != "wasm" || info.Settings["limit"] != int64(2) {
		t.Fatalf("unexpected defaults: %+v", info)
	}
	original := first.active["sample"]
	if _, err := first.Refresh(ctx); err != nil || first.active["sample"] != original {
		t.Fatalf("unchanged package recompiled: %v", err)
	}
	out, err := first.Invoke(ctx, "sample", "report", nil)
	if err != nil || out.Message != "first" || len(out.Blocks) != 1 || out.Blocks[0].Value != "2" {
		t.Fatalf("invoke: %+v, %v", out, err)
	}
	if _, err := first.Invoke(ctx, "sample", "unknown", nil); !errors.Is(err, ErrPackageAction) {
		t.Fatalf("undeclared action: %v", err)
	}
	for _, update := range []map[string]any{{"limit": 1.5}, {"missing": true}, {"style": "invalid"}} {
		if _, err := first.SaveSettings("sample", update); !errors.Is(err, ErrPackageSettings) {
			t.Fatalf("invalid settings accepted: %v", err)
		}
	}
	if _, err := first.SaveSettings("sample", map[string]any{"limit": json.Number("9"), "style": "compact"}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.SetEnabled("sample", false); err != nil {
		t.Fatal(err)
	}
	plugin.Version = "2.0"
	writeTestPackage(t, dataDir, plugin, []byte("invalid WASM"), "plugin.wasm")
	snapshot, err = first.Refresh(ctx)
	if err != nil || len(snapshot.Issues) != 1 || snapshot.Items[0].Version != "1.0" {
		t.Fatalf("invalid replacement: %+v, %v", snapshot, err)
	}
	restarted := testPackageManager(t, dataDir)
	info, err := restarted.Get("sample")
	if err != nil || info.Version != "1.0" || info.Enabled || info.Settings["limit"] != int64(9) || info.Settings["style"] != "compact" {
		t.Fatalf("restore: %+v, %v", info, err)
	}
	if _, err := restarted.Invoke(ctx, "sample", "report", nil); !errors.Is(err, ErrPackageDisabled) {
		t.Fatalf("disabled plugin ran: %v", err)
	}
	writeTestPackage(t, dataDir, plugin, fixtureWASM(`{"message":"second"}`, nil), "plugin.wasm")
	if _, err := restarted.SetEnabled("sample", true); err != nil {
		t.Fatal(err)
	}
	out, err = restarted.Invoke(ctx, "sample", "report", nil)
	if err != nil || out.Message != "first" {
		t.Fatalf("replacement ran before explicit refresh: %+v, %v", out, err)
	}
	if snapshot, err = restarted.Refresh(ctx); err != nil || len(snapshot.Issues) != 0 || snapshot.Items[0].Version != "2.0" || snapshot.Items[0].Settings["limit"] != int64(9) {
		t.Fatalf("valid replacement: %+v, %v", snapshot, err)
	}
	out, err = restarted.Invoke(ctx, "sample", "report", nil)
	if err != nil || out.Message != "second" {
		t.Fatalf("replacement invoke: %+v, %v", out, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	unrefreshed := testPackageManager(t, dataDir)
	if info, err := unrefreshed.Get("sample"); err != nil || info.Version != "2.0" {
		t.Fatalf("removed before refresh: %+v, %v", info, err)
	}
	if snapshot, err = restarted.Refresh(ctx); err != nil || len(snapshot.Items) != 0 || len(snapshot.Issues) != 0 {
		t.Fatalf("remove refresh: %+v, %v", snapshot, err)
	}
	reinstalled := testPackageManager(t, dataDir)
	writeTestPackage(t, dataDir, plugin, fixtureWASM(`{"message":"second"}`, nil), "plugin.wasm")
	if snapshot, err = reinstalled.Refresh(ctx); err != nil || len(snapshot.Items) != 1 || snapshot.Items[0].Settings["limit"] != int64(9) {
		t.Fatalf("reinstallation configuration: %+v, %v", snapshot, err)
	}
}

func TestPackageRefreshRepairsCacheFromUnchangedZIP(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	previous := isolatedExecutable.Swap(&executable)
	t.Cleanup(func() { isolatedExecutable.Store(previous) })
	manager := testPackageManager(t, dataDir)
	wasm := fixtureWASM(`{"message":"recovered"}`, nil)
	for _, id := range []string{"active", "disabled"} {
		writeTestPackage(t, dataDir, fixturePackageManifest(id, "1"), wasm, "plugin.wasm")
	}
	if snapshot, err := manager.Refresh(ctx); err != nil || len(snapshot.Issues) != 0 || len(snapshot.Items) != 2 {
		t.Fatalf("initial refresh: %+v, %v", snapshot, err)
	}
	if _, err := manager.SaveSettings("disabled", map[string]any{"limit": 9, "style": "compact"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SetEnabled("disabled", false); err != nil {
		t.Fatal(err)
	}
	assertPreferences := func(t *testing.T, source *PackageManager) {
		t.Helper()
		info, err := source.Get("disabled")
		if err != nil || info.Enabled || info.Settings["limit"] != int64(9) || info.Settings["style"] != "compact" {
			t.Fatalf("disabled plugin preferences: %+v, %v", info, err)
		}
		if _, err := source.Invoke(ctx, "disabled", "report", nil); !errors.Is(err, ErrPackageDisabled) {
			t.Fatalf("disabled plugin should remain disabled: %v", err)
		}
	}
	digest := packageDigest(wasm)
	modulePath := manager.modulePath(digest)
	for _, name := range []string{"missing", "corrupt"} {
		t.Run(name, func(t *testing.T) {
			for _, path := range []string{modulePath, manager.statePath()} {
				if name == "missing" {
					err = os.Remove(path)
				} else {
					err = os.WriteFile(path, []byte("damaged cache"), 0o600)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := manager.Invoke(ctx, "active", "report", nil); !errors.Is(err, ErrPackageExecution) {
				t.Fatalf("worker accepted invalid module cache: %v", err)
			}
			if snapshot, err := manager.Refresh(ctx); err != nil || len(snapshot.Issues) != 0 || len(snapshot.Items) != 2 {
				t.Fatalf("repair refresh: %+v, %v", snapshot, err)
			}
			if result, err := manager.Invoke(ctx, "active", "report", nil); err != nil || result.Message != "recovered" {
				t.Fatalf("worker could not run repaired module: %+v, %v", result, err)
			}
			assertPreferences(t, manager)
			assertPreferences(t, testPackageManager(t, dataDir))
		})
	}
	if err := os.Remove(modulePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(modulePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := manager.Refresh(ctx); err != nil || len(snapshot.Issues) != 2 || len(snapshot.Items) != 2 {
		t.Fatalf("cache write failure should retain loaded entries: %+v, %v", snapshot, err)
	}
	assertPreferences(t, manager)
}

func TestPackageRejectsUnsafeEntriesAndBoundsGuestCalls(t *testing.T) {
	dataDir := t.TempDir()
	manager := testPackageManager(t, dataDir)
	ctx := context.Background()
	writeTestPackage(t, dataDir, fixturePackageManifest("traversal", "1"), fixtureWASM(`{"message":"fixture"}`, nil), "../plugin.wasm")
	writeTestPackage(t, dataDir, fixturePackageManifest("builtin", "1"), fixtureWASM(`{"message":"fixture"}`, nil), "plugin.wasm")
	loop := []byte{0x03, 0x40, 0x0c, 0x00, 0x0b} // loop { br 0 }
	writeTestPackage(t, dataDir, fixturePackageManifest("loop", "1"), fixtureWASM("", loop), "plugin.wasm")
	writeTestPackage(t, dataDir, fixturePackageManifest("oversize", "1"), fixtureWASM(strings.Repeat("x", maxGuestOutput+1), nil), "plugin.wasm")
	writeTestPackage(t, dataDir, fixturePackageManifest("bad-output", "1"), fixtureWASM(`{"blocks":[{"kind":"html","title":"example","text":"test"}]}`, nil), "plugin.wasm")
	snapshot, err := manager.Refresh(ctx)
	if err != nil || len(snapshot.Issues) != 2 || len(snapshot.Items) != 3 {
		t.Fatalf("refresh must reject traversal/reserved and not execute infinite loop: %+v, %v", snapshot, err)
	}
	short, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
	defer cancel()
	if _, err := manager.Invoke(short, "loop", "report", nil); !errors.Is(err, ErrPackageExecution) || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("infinite loop: %v", err)
	}
	if _, err := manager.Invoke(ctx, "oversize", "report", nil); !errors.Is(err, ErrPackageExecution) || !strings.Contains(err.Error(), "64 KiB") {
		t.Fatalf("oversize output: %v", err)
	}
	if _, err := manager.Invoke(ctx, "bad-output", "report", nil); !errors.Is(err, ErrPackageExecution) {
		t.Fatalf("unsupported HTML view: %v", err)
	}
}

func TestGuestInputOnlyContainsExplicitCapabilities(t *testing.T) {
	plugin := fixturePackageManifest("sample", "1")
	plugin.Capabilities = []string{DownloaderSummaryCapability}
	input, err := guestInput(PackageInfo{PackageManifest: plugin}, "report", map[string]any{
		DownloaderSummaryCapability: []string{"downloader"},
		"some-other-capability":     "fixture-secret",
	})
	if err != nil || !bytes.Contains(input, []byte(DownloaderSummaryCapability)) || bytes.Contains(input, []byte("fixture-secret")) {
		t.Fatalf("guest input: %s, %v", input, err)
	}
}

func TestPackagedDownloaderExample(t *testing.T) {
	sample := os.Getenv("STARPT_PLUGIN_SMOKE_ZIP")
	if sample == "" {
		t.Skip("set STARPT_PLUGIN_SMOKE_ZIP to the built downloader-summary.zip")
	}
	ctx := context.Background()
	dataDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dataDir, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := readPackageFile(sample, maxPackageBytes)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dataDir, "plugins", "downloader-summary.zip")
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
	manager := testPackageManager(t, dataDir)
	loaded, err := manager.Refresh(ctx)
	if err != nil || len(loaded.Issues) != 0 || len(loaded.Items) != 1 {
		t.Fatalf("load official Go WASI sample: %+v, %v", loaded, err)
	}
	v, err := manager.Invoke(ctx, "downloader-summary", "summary", map[string]any{
		DownloaderSummaryCapability: []map[string]any{
			{"name": "Fixture", "enabled": true, "uploaded_bytes": 1073741824, "downloaded_bytes": 0},
			{"name": "Disabled fixture", "enabled": false},
		},
	})
	if err != nil || len(v.Blocks) != 2 || v.Blocks[0].Value != "1" ||
		len(v.Blocks[1].Rows) != 1 || v.Blocks[1].Rows[0][2] != "1.00 GiB" {
		t.Fatalf("run official Go WASI sample: %+v, %v", v, err)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	previous := isolatedExecutable.Swap(&executable)
	defer isolatedExecutable.Store(previous)
	isolated := testPackageManager(t, dataDir)
	if isolated.runtime.engine != nil {
		t.Fatal("process-isolated app still loaded an in-process WASM engine")
	}
	v, err = isolated.Invoke(ctx, "downloader-summary", "summary", map[string]any{
		DownloaderSummaryCapability: []map[string]any{{"name": "Fixture worker", "enabled": true}},
	})
	if err != nil || len(v.Blocks) != 2 || v.Blocks[0].Value != "1" ||
		len(v.Blocks[1].Rows) != 1 || v.Blocks[1].Rows[0][0] != "Fixture worker" {
		t.Fatalf("run official Go WASI sample in worker: %+v, %v", v, err)
	}
}

func fixturePackageManifest(id, version string) PackageManifest {
	return PackageManifest{
		SchemaVersion: 2,
		ID:            id,
		Name:          "Fixture",
		Version:       version,
		Fields: []Field{
			{Key: "limit", Label: "Limit", Type: "integer", Default: 2},
			{Key: "style", Label: "Style", Type: "string", Options: []string{"brief", "compact"}},
		},
		Actions: []Action{{ID: "report", Label: "Report"}},
	}
}

func writeTestPackage(t *testing.T, dataDir string, manifest PackageManifest, wasm []byte, wasmName string) string {
	t.Helper()
	var content bytes.Buffer
	archive := zip.NewWriter(&content)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		name string
		data []byte
	}{{"plugin.json", data}, {wasmName, wasm}} {
		out, err := archive.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := out.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dataDir, "plugins")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, manifest.ID+".zip")
	if err := os.WriteFile(path, content.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// fixtureWASM builds a tiny WASI command. Its default _start calls fd_write
// once with an embedded response. Specific tests can replace its instructions.
func fixtureWASM(response string, instructions []byte) []byte {
	wasm := []byte{0, 'a', 's', 'm', 1, 0, 0, 0}
	section := func(id byte, payload []byte) {
		wasm = append(wasm, id)
		wasm = binary.AppendUvarint(wasm, uint64(len(payload)))
		wasm = append(wasm, payload...)
	}
	addName := func(out []byte, name string) []byte {
		return append(binary.AppendUvarint(out, uint64(len(name))), name...)
	}
	// Type 0: fd_write(i32,i32,i32,i32)->i32. Type 1: _start()->().
	section(1, []byte{2, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f, 0x60, 0, 0})
	imports := addName([]byte{1}, "wasi_snapshot_preview1")
	imports = addName(imports, "fd_write")
	section(2, append(imports, 0, 0))
	section(3, []byte{1, 1})
	section(5, []byte{1, 0, 2})
	export := append(addName([]byte{2}, "memory"), 2, 0)
	section(7, append(addName(export, "_start"), 0, 1))
	if instructions == nil {
		instructions = []byte{0x41, 1, 0x41, 0, 0x41, 1, 0x41, 8, 0x10, 0, 0x1a}
	}
	body := append(append([]byte{0}, instructions...), 0x0b)
	section(10, append(binary.AppendUvarint([]byte{1}, uint64(len(body))), body...))
	memory := make([]byte, 16)
	binary.LittleEndian.PutUint32(memory[:4], 16)
	binary.LittleEndian.PutUint32(memory[4:8], uint32(len(response)))
	memory = append(memory, response...)
	segment := binary.AppendUvarint([]byte{1, 0, 0x41, 0, 0x0b}, uint64(len(memory)))
	section(11, append(segment, memory...))
	return wasm
}
