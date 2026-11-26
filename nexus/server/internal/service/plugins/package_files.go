package plugins

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const (
	maxPackageBytes = 8 << 20
	maxModuleBytes  = 8 << 20
	maxStateBytes   = 4 << 20
)

type cachedPackage struct {
	Manifest  PackageManifest `json:"manifest"`
	Digest    string          `json:"digest"`
	ZipDigest string          `json:"zip_digest"`
}

type packagePreferences struct {
	Enabled  bool           `json:"enabled"`
	Settings map[string]any `json:"settings,omitempty"`
}

type savedPackageState struct {
	Version     int                           `json:"version"`
	Active      map[string]cachedPackage      `json:"active"`
	Preferences map[string]packagePreferences `json:"preferences"`
}

func packageDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validPackageDigest(digest string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(digest)
	return err == nil && hex.EncodeToString(decoded) == digest
}

// Regular file and inode checks keep the loader from following symlinks or
// reading a replacement path after lstat.
func readPackageFile(path string, limit int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() > limit {
		return nil, errors.New("文件不是普通文件或超过大小限制")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() || opened.Size() > limit {
		return nil, errors.New("打开文件时内容已更改")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) != opened.Size() {
		return nil, errors.New("读取文件时内容已更改")
	}
	after, err := f.Stat()
	if err != nil || after.Size() != opened.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return nil, errors.New("读取文件时内容已更改")
	}
	return data, nil
}

func readPackageZip(content []byte, id string) (PackageManifest, []byte, error) {
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil || len(archive.File) != 2 {
		return PackageManifest{}, nil, errors.New("ZIP 须且仅包含 plugin.json 和 plugin.wasm")
	}
	files := make(map[string][]byte, 2)
	for _, entry := range archive.File {
		limit := uint64(maxManifestBytes)
		switch entry.Name {
		case "plugin.json":
		case "plugin.wasm":
			limit = maxModuleBytes
		default:
			return PackageManifest{}, nil, errors.New("ZIP 包含未知路径")
		}
		if files[entry.Name] != nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 ||
			entry.UncompressedSize64 > limit || (entry.Method != zip.Store && entry.Method != zip.Deflate) {
			return PackageManifest{}, nil, errors.New("ZIP 内容重复、文件格式无效或超出大小限制")
		}
		blob, err := readZipMember(entry, limit)
		if err != nil {
			return PackageManifest{}, nil, err
		}
		files[entry.Name] = blob
	}
	var manifest PackageManifest
	raw := bytes.TrimPrefix(files["plugin.json"], []byte{0xef, 0xbb, 0xbf})
	if err := decodePackageJSON(raw, &manifest); err != nil {
		return PackageManifest{}, nil, err
	}
	if err := normalizeManifest(&manifest, id); err != nil {
		return PackageManifest{}, nil, err
	}
	return manifest, files["plugin.wasm"], nil
}

func readZipMember(entry *zip.File, limit uint64) ([]byte, error) {
	member, err := entry.Open()
	if err != nil {
		return nil, errors.New("无法打开 ZIP 内容")
	}
	defer member.Close()
	blob, err := io.ReadAll(io.LimitReader(member, int64(limit)+1))
	if err != nil || uint64(len(blob)) != entry.UncompressedSize64 || uint64(len(blob)) > limit {
		return nil, errors.New("ZIP 内容损坏或超过大小限制")
	}
	return blob, nil
}

func writePackageState(path string, state savedPackageState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(data) > maxStateBytes {
		return errors.New("插件配置缓存超过大小限制")
	}
	if current, err := readPackageFile(path, maxStateBytes); err == nil && bytes.Equal(current, data) {
		return nil
	}
	return atomicPackageWrite(path, data)
}

func atomicPackageWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".temporary-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
