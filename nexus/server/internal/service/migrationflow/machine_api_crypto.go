package migrationflow

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"github.com/pt-nexus/server/internal/repository"
	"io"
	"os"
	"path/filepath"
)

// secretBox 是服务端密钥保护的认证加密容器。
type secretBox struct{ aead cipher.AEAD }

func openSecretBox(dataDir string) (*secretBox, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "machine-registry.key")
	key, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, e
		}
		_, e = f.Write(key)
		closeErr := f.Close()
		if e != nil {
			return nil, e
		}
		if closeErr != nil {
			return nil, closeErr
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid registry encryption key")
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(b)
	return &secretBox{a}, e
}
func (b *secretBox) seal(raw []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, raw, nil), nil
}
func (b *secretBox) open(raw []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(raw) < n {
		return nil, errors.New("invalid encrypted value")
	}
	return b.aead.Open(nil, raw[:n], raw[n:], nil)
}

func openRegistrySecretBox(store *repository.Store, dataDir string) (*secretBox, error) {
	if _, e := os.Stat(filepath.Join(dataDir, "machine-registry.key")); os.IsNotExist(e) {
		var secrets, replays int64
		if e = store.DB.Model(&repository.SiteProfileSecret{}).Where("rss IS NOT NULL").Count(&secrets).Error; e != nil {
			return nil, e
		}
		if e = store.DB.Model(&repository.MachineReplayRow{}).Count(&replays).Error; e != nil {
			return nil, e
		}
		if secrets > 0 || replays > 0 {
			return nil, errors.New("registry encryption key missing; restore the original key before startup")
		}
	}
	return openSecretBox(dataDir)
}
