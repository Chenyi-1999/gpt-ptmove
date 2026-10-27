package migrationflow

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MachinePrincipal 表示已经认证的独立机器密钥。
type MachinePrincipal struct {
	ID     string
	Scopes map[string]bool
}

// MachineAPIService 只接受独立 Bearer 密钥；持久事务保证幂等回复与业务状态一起提交。
type MachineAPIService struct {
	store *repository.Store
	box   *secretBox
	mu    sync.Mutex
}

// NewMachineAPIService 初始化只存哈希的密钥表及加密回复存储。
func NewMachineAPIService(store *repository.Store, dataDir string) (*MachineAPIService, error) {
	if e := repository.EnsureMachineSchema(store); e != nil {
		return nil, e
	}
	b, e := openRegistrySecretBox(store, dataDir)
	if e != nil {
		return nil, e
	}
	return &MachineAPIService{store: store, box: b}, nil
}
func randomMachineID() (string, error) {
	raw := make([]byte, 32)
	if _, e := rand.Read(raw); e != nil {
		return "", e
	}
	return hex.EncodeToString(raw), nil
}
func machineHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// CreateKey 默认只读，强制有效期；Bearer 明文仅在创建结果中出现一次。
func (s *MachineAPIService) CreateKey(name string, scopes []string, expires time.Time) (map[string]any, error) {
	if strings.TrimSpace(name) == "" || len(name) > 100 || strings.ContainsAny(name, "\r\n") || !expires.After(time.Now()) || expires.After(time.Now().AddDate(1, 0, 0)) {
		return nil, errors.New("name and expiry within one year required")
	}
	if len(scopes) == 0 {
		scopes = []string{"read"}
	}
	seen := map[string]bool{}
	for _, v := range scopes {
		if v != "read" && v != "write" && v != "publish" || seen[v] {
			return nil, errors.New("invalid scopes")
		}
		seen[v] = true
	}
	if !seen["read"] {
		return nil, errors.New("scopes must include read")
	}
	id, e := randomMachineID()
	if e != nil {
		return nil, e
	}
	secret, e := randomMachineID()
	if e != nil {
		return nil, e
	}
	token := "ptn_" + secret
	b, _ := json.Marshal(scopes)
	row := repository.MachineKeyRow{ID: id[:24], Name: strings.TrimSpace(name), Hash: machineHash(token), Scopes: string(b), ExpiresAt: expires.Unix(), CreatedAt: time.Now().Unix()}
	if e = s.store.DB.Create(&row).Error; e != nil {
		return nil, e
	}
	return map[string]any{"key": machineKeyView(row), "token": token}, nil
}
func machineKeyView(row repository.MachineKeyRow) map[string]any {
	var scopes []string
	json.Unmarshal([]byte(row.Scopes), &scopes)
	return map[string]any{"id": row.ID, "name": row.Name, "scopes": scopes, "expires_at": time.Unix(row.ExpiresAt, 0).UTC().Format(time.RFC3339), "created_at": time.Unix(row.CreatedAt, 0).UTC().Format(time.RFC3339), "revoked": row.Revoked}
}

// ListKeys 返回不含哈希和明文的密钥元数据。
func (s *MachineAPIService) ListKeys() ([]map[string]any, error) {
	var rows []repository.MachineKeyRow
	if e := s.store.DB.Order("created_at DESC").Find(&rows).Error; e != nil {
		return nil, e
	}
	out := []map[string]any{}
	for _, row := range rows {
		out = append(out, machineKeyView(row))
	}
	return out, nil
}

// RevokeKey 立即撤销密钥，包含已缓存幂等响应的重放权限。
func (s *MachineAPIService) RevokeKey(id string) error {
	r := s.store.DB.Model(&repository.MachineKeyRow{}).Where("id = ?", id).Update("revoked", true)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return errors.New("key not found")
	}
	return nil
}

// Authenticate 只匹配机器 token 哈希，不检查 Cookie、NAS key 或管理员 JWT。
func (s *MachineAPIService) Authenticate(token string) (*MachinePrincipal, error) {
	if !strings.HasPrefix(token, "ptn_") || len(token) != 68 {
		return nil, errors.New("invalid machine key")
	}
	var row repository.MachineKeyRow
	e := s.store.DB.Where("hash = ?", machineHash(token)).Take(&row).Error
	if e != nil || row.Revoked || row.ExpiresAt <= time.Now().Unix() {
		return nil, errors.New("invalid or expired machine key")
	}
	return principal(row), nil
}
func principal(row repository.MachineKeyRow) *MachinePrincipal {
	var scopes []string
	json.Unmarshal([]byte(row.Scopes), &scopes)
	p := &MachinePrincipal{ID: row.ID, Scopes: map[string]bool{}}
	for _, v := range scopes {
		p.Scopes[v] = true
	}
	return p
}

// Allows 检查独立授权范围；发布不隐含读写能力。
func (p *MachinePrincipal) Allows(scope string) bool { return p.Scopes[scope] }

var machineIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{8,128}$`)
var errCommandRollback = errors.New("machine command rejected")

// Execute 在同一事务中重新授权、执行业务和加密缓存；失败回复不提交业务变更。
func (s *MachineAPIService) Execute(ctx context.Context, p *MachinePrincipal, key, method, path string, raw []byte, required string, run func(context.Context) (map[string]any, int)) (map[string]any, int) {
	return s.ExecuteWithPolicy(ctx, p, key, method, path, raw, func(context.Context) string { return required }, run)
}

// ExecuteWithPolicy 在同一事务中按当前自动化状态再次判断授权，缓存重放也不会跳过。
func (s *MachineAPIService) ExecuteWithPolicy(ctx context.Context, p *MachinePrincipal, key, method, path string, raw []byte, authorize func(context.Context) string, run func(context.Context) (map[string]any, int)) (map[string]any, int) {
	hash, err := machineCommandHash(key, method, path, raw)
	if err != nil {
		return map[string]any{"error": err.Error()}, 400
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var response map[string]any
	status := 500
	e := s.store.DB.Transaction(func(tx *gorm.DB) error {
		var current repository.MachineKeyRow
		if e := tx.Where("id = ?", p.ID).Take(&current).Error; e != nil || current.Revoked || current.ExpiresAt <= time.Now().Unix() {
			status = 401
			response = map[string]any{"error": "key expired or revoked"}
			return errCommandRollback
		}
		required := authorize(repository.WithMachineTransaction(ctx, tx))
		if !allowsMachineRequirements(principal(current), required) {
			status = 403
			response = map[string]any{"error": "insufficient scope"}
			return errCommandRollback
		}
		var replay repository.MachineReplayRow
		e := tx.Where("key_id = ? AND token = ?", p.ID, key).Take(&replay).Error
		if e == nil {
			if replay.Hash != hash {
				status = 409
				response = map[string]any{"error": "idempotency key conflict"}
				return errCommandRollback
			}
			decoded, e := s.box.open(replay.Response)
			if e != nil {
				return e
			}
			if e = json.Unmarshal(decoded, &response); e != nil {
				return e
			}
			status = replay.Status
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		response, status = run(repository.WithMachineTransaction(ctx, tx))
		if status >= 400 {
			return errCommandRollback
		}
		body, e := json.Marshal(response)
		if e != nil {
			return e
		}
		encrypted, e := s.box.seal(body)
		if e != nil {
			return e
		}
		return tx.Create(&repository.MachineReplayRow{KeyID: p.ID, Token: key, Hash: hash, Response: encrypted, Status: status}).Error
	})
	if e != nil && !errors.Is(e, errCommandRollback) {
		return map[string]any{"error": "command transaction failed"}, 500
	}
	return response, status
}

// allowsMachineRequirements 要求内部策略列出的每个独立权限，发布能力不替代写入。
func allowsMachineRequirements(p *MachinePrincipal, required string) bool {
	for _, scope := range strings.Split(required, "+") {
		if !p.Allows(scope) {
			return false
		}
	}
	return true
}

func machineCommandHash(key, method, path string, raw []byte) (string, error) {
	if !machineIdempotencyPattern.MatchString(key) {
		return "", errors.New("Idempotency-Key requires 8..128 safe ASCII characters")
	}
	var value any
	canonical := []byte("{}")
	if len(raw) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
			return "", errors.New("invalid JSON")
		}
		canonical, _ = json.Marshal(value)
	}
	return machineHash(method + "\n" + path + "\n" + string(canonical)), nil
}

// ExecuteReadCommand 只用于无业务写入的预览：两段短事务重授权和缓存，中间不持锁联网。
// 并发相同请求可重复只读计算，但只缓存并返回第一个成功回复；失败不写缓存。
func (s *MachineAPIService) ExecuteReadCommand(ctx context.Context, p *MachinePrincipal, key, method, path string, raw []byte, required string, run func(context.Context) (map[string]any, int)) (map[string]any, int) {
	hash, err := machineCommandHash(key, method, path, raw)
	if err != nil {
		return map[string]any{"error": err.Error()}, 400
	}
	var response map[string]any
	status := 500
	phase := func(finalize bool) (bool, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		resolved := false
		err := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current repository.MachineKeyRow
			if err := tx.Where("id = ?", p.ID).Take(&current).Error; err != nil || current.Revoked || current.ExpiresAt <= time.Now().Unix() {
				response = map[string]any{"error": "key expired or revoked"}
				status = 401
				resolved = true
				return nil
			}
			if !allowsMachineRequirements(principal(current), required) {
				response = map[string]any{"error": "insufficient scope"}
				status = 403
				resolved = true
				return nil
			}
			loadReplay := func() (bool, error) {
				var replay repository.MachineReplayRow
				err := tx.Where("key_id = ? AND token = ?", p.ID, key).Take(&replay).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return false, nil
				}
				if err != nil {
					return false, err
				}
				if replay.Hash != hash {
					response = map[string]any{"error": "idempotency key conflict"}
					status = 409
					return true, nil
				}
				decoded, err := s.box.open(replay.Response)
				if err != nil {
					return false, err
				}
				if err = json.Unmarshal(decoded, &response); err != nil {
					return false, err
				}
				status = replay.Status
				return true, nil
			}
			hit, err := loadReplay()
			if err != nil {
				return err
			}
			if hit {
				resolved = true
				return nil
			}
			if !finalize {
				return nil
			}
			resolved = true
			if status >= 400 {
				return nil
			}
			body, err := json.Marshal(response)
			if err != nil {
				return err
			}
			encrypted, err := s.box.seal(body)
			if err != nil {
				return err
			}
			saved := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&repository.MachineReplayRow{KeyID: p.ID, Token: key, Hash: hash, Response: encrypted, Status: status})
			if saved.Error != nil {
				return saved.Error
			}
			if saved.RowsAffected == 0 {
				_, err = loadReplay()
				return err
			}
			return nil
		})
		return resolved, err
	}
	hit, err := phase(false)
	if err != nil {
		return map[string]any{"error": "command transaction failed"}, 500
	}
	if hit {
		return response, status
	}
	// 清除上层事务标记，保持取消/截止时间，预览中的普通读取可及时释放数据库连接。
	response, status = run(repository.WithMachineTransaction(ctx, nil))
	if _, err = phase(true); err != nil {
		return map[string]any{"error": "command transaction failed"}, 500
	}
	return response, status
}
