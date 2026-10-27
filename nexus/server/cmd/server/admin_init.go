package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// initializeAdmin 从标准输入一次性读取管理员资料，仅在尚无密码哈希时初始化。
// 密码不进入参数、日志或配置明文；已有账号、非法输入和写入失败均拒绝覆盖。
func initializeAdmin(input io.Reader, dataDir string) error {
	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(io.LimitReader(input, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credentials); err != nil {
		return errors.New("需要有界的用户名和密码 JSON")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("输入含多余数据")
	}
	if strings.TrimSpace(credentials.Username) == "" || len(credentials.Username) > 100 || len(credentials.Password) < 12 || len(credentials.Password) > 72 {
		return errors.New("用户名无效，密码长度须为12至72字节")
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return errors.New("无法创建数据目录")
	}
	path := filepath.Join(dataDir, "config.json")
	config := map[string]any{}
	existing, err := os.ReadFile(path)
	if err == nil {
		if json.Unmarshal(existing, &config) != nil {
			return errors.New("已有配置损坏，拒绝覆盖")
		}
	} else if !os.IsNotExist(err) {
		return errors.New("无法读取已有配置")
	}
	if auth, ok := config["auth"].(map[string]any); ok {
		if hash, ok := auth["password_hash"].(string); ok && hash != "" {
			return errors.New("已有管理员账号，拒绝覆盖")
		}
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(credentials.Password), bcrypt.DefaultCost)
	if err != nil {
		return errors.New("密码哈希失败")
	}
	config["auth"] = map[string]any{"username": credentials.Username, "password_hash": string(hashed), "must_change_password": false}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return errors.New("配置编码失败")
	}
	file, err := os.CreateTemp(dataDir, ".admin-init-")
	if err != nil {
		return errors.New("无法保存配置")
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(append(data, '\n'))
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return errors.New("保存配置失败")
	}
	if err = os.Rename(temporary, path); err != nil {
		return errors.New("配置替换失败")
	}
	return nil
}
