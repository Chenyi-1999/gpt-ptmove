package repository

import (
	"context"
	"errors"
	"gorm.io/gorm"
)

// SiteProfileRow 持久保存公开声明；唯一来源与乐观版本防止身份替换。
type SiteProfileRow struct {
	ID       string `gorm:"primaryKey;size:64"`
	Origin   string `gorm:"uniqueIndex;size:512"`
	Revision int
	JSON     string `gorm:"type:text"`
}

// SiteProfileSecret 持久保存加密 RSS，原生 Cookie 仍使用兼容站点表。
type SiteProfileSecret struct {
	ID  string `gorm:"primaryKey;size:64"`
	RSS []byte
}

// MachineKeyRow 只保存密钥哈希，永远不保存 Bearer 明文。
type MachineKeyRow struct {
	ID        string `gorm:"primaryKey;size:64" json:"id"`
	Name      string `json:"name"`
	Hash      string `gorm:"uniqueIndex;size:64" json:"-"`
	Scopes    string `json:"-"`
	ExpiresAt int64  `json:"expires_at"`
	Revoked   bool   `json:"revoked"`
	CreatedAt int64  `json:"created_at"`
}

// MachineReplayRow 与业务写入共用数据库事务；响应内容加密。
type MachineReplayRow struct {
	KeyID    string `gorm:"primaryKey;size:64"`
	Token    string `gorm:"primaryKey;size:128"`
	Hash     string `gorm:"size:64"`
	Response []byte
	Status   int
}
type machineTxKey struct{}

// MachineTransaction 返回机器 API 的同一事务；普通请求返回 nil。
func MachineTransaction(ctx context.Context) *gorm.DB {
	if ctx == nil {
		return nil
	}
	db, _ := ctx.Value(machineTxKey{}).(*gorm.DB)
	return db
}

// WithMachineTransaction 将事务显式传递到业务仓库，禁止跨事务写入。
func WithMachineTransaction(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, machineTxKey{}, tx)
}

// EnsureMachineSchema 创建独立注册表，不修改原生种子数据。
func EnsureMachineSchema(store *Store) error {
	if store == nil || store.DB == nil {
		return errors.New("registry database unavailable")
	}
	return store.DB.AutoMigrate(&SiteProfileRow{}, &SiteProfileSecret{}, &MachineKeyRow{}, &MachineReplayRow{})
}
