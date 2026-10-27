package bootstrap

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ensureInternalSecret 在后台工作者启动前加载实例级内部密钥，禁止公开的默认值。
// 密钥仅写入本实例数据目录；现有环境密钥保持不变，过短或损坏时阻止启动。
func ensureInternalSecret(dataDir string) error {
	if value := os.Getenv("INTERNAL_SECRET"); value != "" {
		if len(value) < 32 {
			return errors.New("内部服务密钥长度不足")
		}
		return nil
	}
	path := filepath.Join(dataDir, "internal-api.key")
	secret, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		raw := make([]byte, 32)
		if _, err = rand.Read(raw); err != nil {
			return errors.New("内部服务密钥生成失败")
		}
		secret = []byte(hex.EncodeToString(raw))
		file, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr != nil {
			return errors.New("内部服务密钥保存失败")
		}
		_, writeErr := file.Write(secret)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return errors.New("内部服务密钥保存失败")
		}
	} else if err != nil {
		return errors.New("内部服务密钥读取失败")
	}
	if len(strings.TrimSpace(string(secret))) < 32 {
		return errors.New("内部服务密钥损坏")
	}
	return os.Setenv("INTERNAL_SECRET", strings.TrimSpace(string(secret)))
}
