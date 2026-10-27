package migrationflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/processing/siteprofile"
	"github.com/pt-nexus/server/internal/service/publish/mapping"
	"gorm.io/gorm"
)

// SiteProfileService 编排声明注册、原生站点和分离凭据；所有验证离线执行。
type SiteProfileService struct {
	store   *repository.Store
	cfg     *config.Manager
	box     *secretBox
	baseDir string
}

// NewSiteProfileService 初始化注册表并恢复映射绑定；错误时必须阻止启动自动化。
func NewSiteProfileService(store *repository.Store, cfg *config.Manager, dataDir string) (*SiteProfileService, error) {
	if err := repository.EnsureMachineSchema(store); err != nil {
		return nil, err
	}
	box, e := openRegistrySecretBox(store, dataDir)
	if e != nil {
		return nil, e
	}
	s := &SiteProfileService{store: store, cfg: cfg, box: box, baseDir: config.ResolveRuntimePaths().BaseDir}
	rows := []repository.SiteProfileRow{}
	if e = store.DB.Find(&rows).Error; e != nil {
		return nil, e
	}
	for _, row := range rows {
		var p siteprofile.Profile
		if json.Unmarshal([]byte(row.JSON), &p) != nil {
			return nil, errors.New("corrupt site registry")
		}
		if p.PTNexusSite != "" {
			if e = s.mappingAvailable(p.PTNexusSite); e != nil {
				return nil, e
			}
			siteprofile.RegisterMapping(p.ID, p.PTNexusSite)
		}
	}
	return s, nil
}
func (s *SiteProfileService) db(ctx context.Context) *gorm.DB {
	if tx := repository.MachineTransaction(ctx); tx != nil {
		return tx
	}
	return s.store.DB
}

// Resolve 接受声明 ID、原生代码或昵称，绝不返回 Cookie。
func (s *SiteProfileService) Resolve(id string) (*siteprofile.Profile, error) {
	return s.resolve(s.store.DB, id)
}
func (s *SiteProfileService) resolve(db *gorm.DB, id string) (*siteprofile.Profile, error) {
	var row repository.SiteProfileRow
	e := db.Where("id = ?", id).Take(&row).Error
	if e == nil {
		var p siteprofile.Profile
		if json.Unmarshal([]byte(row.JSON), &p) != nil {
			return nil, errors.New("corrupt site registry")
		}
		var identity struct {
			Site     string
			Nickname string
			BaseURL  string `gorm:"column:base_url"`
		}
		if e := db.Table("sites").Select("site,nickname,base_url").Where("site = ?", p.ID).Take(&identity).Error; e != nil {
			return nil, e
		}
		if normalizedOrigin(identity.BaseURL) != p.BaseURL {
			return nil, errors.New("native site origin changed; profile requires review")
		}
		p.NativeSiteCode = identity.Site
		p.NativeNickname = identity.Nickname
		return &p, nil
	}
	if !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}
	var native struct {
		Site      string
		Nickname  string
		BaseURL   string `gorm:"column:base_url"`
		Migration int
		Group     string
	}
	res := db.Table("sites").Select("site,nickname,base_url,migration,"+s.store.GroupColumn()).Where("site = ? OR nickname = ?", id, id).Take(&native)
	if res.Error != nil {
		return nil, res.Error
	}
	if id != native.Site {
		return s.resolve(db, native.Site)
	}
	p := siteprofile.Default()
	p.ID = native.Site
	p.Name = native.Nickname
	p.BaseURL = normalizedOrigin(native.BaseURL)
	p.PTNexusSite = native.Site
	p.NativeSiteCode = native.Site
	p.NativeNickname = native.Nickname
	p.Builtin = true
	p.Health = "declared/unverified"
	p.Roles = []string{}
	if native.Migration == 1 || native.Migration == 3 {
		p.Roles = append(p.Roles, "source")
	}
	if native.Migration == 2 || native.Migration == 3 {
		p.Roles = append(p.Roles, "target")
	}
	if native.Group != "" {
		p.OfficialGroups = strings.Split(native.Group, ",")
	}
	p.Revision = 1
	canonical, _ := json.Marshal(p)
	sum := sha256.Sum256(canonical)
	p.Digest = hex.EncodeToString(sum[:])
	return &p, nil
}

// List 返回所有原生与导入站点的公开元数据。
func (s *SiteProfileService) List() ([]siteprofile.Profile, error) {
	var rows []struct{ Site string }
	if e := s.store.DB.Table("sites").Select("site").Order("site").Find(&rows).Error; e != nil {
		return nil, e
	}
	out := []siteprofile.Profile{}
	for _, row := range rows {
		p, e := s.Resolve(row.Site)
		if e != nil {
			return nil, e
		}
		out = append(out, *p)
	}
	return out, nil
}

// Preview 只校验声明与映射兼容性，不写数据库或请求站点。
func (s *SiteProfileService) Preview(raw []byte) (*siteprofile.Profile, error) {
	return s.PreviewContext(context.Background(), raw)
}

// PreviewContext 使用调用方事务执行兼容性读取，保持预览无写入副作用。
func (s *SiteProfileService) PreviewContext(ctx context.Context, raw []byte) (*siteprofile.Profile, error) {
	p, e := siteprofile.Parse(raw)
	if e != nil {
		return nil, e
	}
	if p.PTNexusSite != "" {
		if e = s.mappingAvailableDB(s.db(ctx), p.PTNexusSite); e != nil {
			return nil, e
		}
	}
	return p, nil
}
func (s *SiteProfileService) mappingAvailable(code string) error {
	return s.mappingAvailableDB(s.store.DB, code)
}
func (s *SiteProfileService) mappingAvailableDB(db *gorm.DB, code string) error {
	var native struct{ Site string }
	e := db.Table("sites").Select("site").Where("site = ?", code).Take(&native).Error
	u := native.Site
	if e != nil || u != code {
		return errors.New("capability_unsupported: unknown native mapping")
	}
	path := filepath.Join(s.baseDir, "configs", code+".yaml")
	info, e := os.Stat(path)
	if e != nil || !info.Mode().IsRegular() {
		return errors.New("capability_unsupported: native mapping unavailable")
	}
	return nil
}
func (s *SiteProfileService) resolveNative(code string) (string, error) {
	var row struct{ Site string }
	e := s.store.DB.Table("sites").Select("site").Where("site = ?", code).Take(&row).Error
	return row.Site, e
}

// Import 要求显式更新与匹配版本；ID、来源和原生映射不可变。
func (s *SiteProfileService) Import(raw []byte, update bool, revision int) (*siteprofile.Profile, error) {
	return s.ImportContext(context.Background(), raw, update, revision)
}

// ImportContext 在机器 API 的事务内同时写入声明与原生站点。
func (s *SiteProfileService) ImportContext(ctx context.Context, raw []byte, update bool, revision int) (*siteprofile.Profile, error) {
	p, e := s.PreviewContext(ctx, raw)
	if e != nil {
		return nil, e
	}
	apply := func(tx *gorm.DB) error {
		var old repository.SiteProfileRow
		err := tx.Where("id = ?", p.ID).Take(&old).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if exists {
			var previous siteprofile.Profile
			if json.Unmarshal([]byte(old.JSON), &previous) != nil {
				return errors.New("corrupt site registry")
			}
			if !update || revision != old.Revision {
				return errors.New("revision_conflict: explicit update and current revision required")
			}
			if old.Origin != p.BaseURL || previous.PTNexusSite != p.PTNexusSite {
				return errors.New("immutable profile identity")
			}
			p.Builtin = previous.Builtin
			p.Revision = old.Revision + 1
		} else {
			var native struct {
				Site     string
				Nickname string
				BaseURL  string `gorm:"column:base_url"`
			}
			nativeErr := tx.Table("sites").Select("site,nickname,base_url").Where("site = ?", p.ID).Take(&native).Error
			if nativeErr == nil {
				if !update || revision != 1 {
					return errors.New("builtin identity requires explicit update and revision 1")
				}
				if normalizedOrigin(native.BaseURL) != p.BaseURL || p.PTNexusSite != native.Site {
					return errors.New("immutable builtin identity")
				}
				p.Builtin = true
				p.Revision = 2
				p.NativeNickname = native.Nickname
				p.NativeSiteCode = native.Site
			} else {
				if !errors.Is(nativeErr, gorm.ErrRecordNotFound) {
					return nativeErr
				}
				if update || revision != 0 {
					return errors.New("revision_conflict: profile does not exist")
				}
				p.Revision = 1
				var n int64
				if e := tx.Table("sites").Where("nickname = ? OR nickname = ?", p.ID, p.Name).Count(&n).Error; e != nil {
					return e
				}
				if n != 0 {
					return errors.New("builtin identity cannot be overwritten")
				}
			}
		}
		if p.Builtin {
			var native struct {
				Site      string
				Nickname  string
				Migration int
			}
			if e := tx.Table("sites").Select("site,nickname,migration").Where("site = ?", p.ID).Take(&native).Error; e != nil {
				return e
			}
			if p.HasRole("source") && native.Migration != 1 && native.Migration != 3 || p.HasRole("target") && native.Migration != 2 && native.Migration != 3 {
				return errors.New("capability_unsupported: cannot expand native roles")
			}
			p.NativeNickname = native.Nickname
			p.NativeSiteCode = native.Site
		}
		var sites []struct {
			Site    string
			BaseURL string `gorm:"column:base_url"`
		}
		if e := tx.Table("sites").Select("site,base_url").Find(&sites).Error; e != nil {
			return e
		}
		for _, site := range sites {
			if site.Site != p.ID && normalizedOrigin(site.BaseURL) == p.BaseURL {
				return errors.New("duplicate site origin")
			}
		}
		b, _ := json.Marshal(p)
		row := repository.SiteProfileRow{ID: p.ID, Origin: p.BaseURL, Revision: p.Revision, JSON: string(b)}
		if exists {
			r := tx.Model(&repository.SiteProfileRow{}).Where("id = ? AND revision = ?", p.ID, revision).Updates(map[string]any{"revision": p.Revision, "json": string(b)})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return errors.New("revision_conflict")
			}
		} else {
			if e := tx.Create(&row).Error; e != nil {
				return e
			}
		}
		if p.Builtin {
			return nil
		}
		migration := 0
		if p.HasRole("source") {
			migration++
		}
		if p.HasRole("target") {
			migration += 2
		}
		values := map[string]any{"nickname": p.ID, "base_url": p.BaseURL, "migration": migration, "description": "Imported declarative profile"}
		if exists {
			return tx.Table("sites").Where("site = ?", p.ID).Updates(values).Error
		}
		values["site"] = p.ID
		return tx.Table("sites").Create(values).Error
	}
	if repository.MachineTransaction(ctx) != nil {
		e = apply(s.db(ctx))
	} else {
		e = s.store.DB.Transaction(apply)
	}
	if e != nil {
		return nil, e
	}
	if p.PTNexusSite != "" {
		siteprofile.RegisterMapping(p.ID, p.PTNexusSite)
	}
	return p, nil
}
func normalizedOrigin(raw string) string {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, e := url.Parse(raw)
	if e != nil {
		return ""
	}
	port := u.Port()
	host := strings.ToLower(u.Hostname())
	if port != "" && port != "443" {
		host += ":" + port
	}
	return strings.ToLower(u.Scheme) + "://" + host
}

// Export 返回可再次导入的公开声明，不包含凭据和服务端字段。
func (s *SiteProfileService) Export(id string) (map[string]any, error) {
	p, e := s.Resolve(id)
	if e != nil {
		return nil, e
	}
	b, _ := json.Marshal(p)
	out := map[string]any{}
	json.Unmarshal(b, &out)
	for _, key := range []string{"revision", "digest", "health", "builtin"} {
		delete(out, key)
	}
	return out, nil
}

// Cookie 通过原生兼容存储读取 Cookie；不复制到声明注册表。
func (s *SiteProfileService) Cookie(id string) (string, error) {
	return s.CookieContext(context.Background(), id)
}

// CookieContext 使用相同事务读取原生兼容 Cookie。
func (s *SiteProfileService) CookieContext(ctx context.Context, id string) (string, error) {
	db := s.db(ctx)
	p, e := s.resolve(db, id)
	if e != nil {
		return "", e
	}
	var row struct{ Cookie string }
	e = db.Table("sites").Select("cookie").Where("site = ?", p.NativeCode()).Take(&row).Error
	return row.Cookie, e
}

// RSS 解密独立 RSS 凭据；缺失凭据返回空值。
func (s *SiteProfileService) RSS(id string) (string, error) {
	return s.RSSContext(context.Background(), id)
}

// RSSContext 使用相同事务读取并解密 RSS 凭据。
func (s *SiteProfileService) RSSContext(ctx context.Context, id string) (string, error) {
	db := s.db(ctx)
	p, e := s.resolve(db, id)
	if e != nil {
		return "", e
	}
	var row repository.SiteProfileSecret
	e = db.Where("id = ?", p.ID).Take(&row).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	if len(row.RSS) == 0 {
		return "", nil
	}
	v, e := s.box.open(row.RSS)
	return string(v), e
}

// SetCredentials 省略字段保留，空字符串清除；Cookie 保持原生兼容存储。
func (s *SiteProfileService) SetCredentials(id string, cookie, rss *string) error {
	return s.SetCredentialsContext(context.Background(), id, cookie, rss)
}

// SetCredentialsContext 将凭据变更加入机器事务；RSS 必须保持站点同源。
func (s *SiteProfileService) SetCredentialsContext(ctx context.Context, id string, cookie, rss *string) error {
	db := s.db(ctx)
	p, e := s.resolve(db, id)
	if e != nil {
		return e
	}
	if cookie != nil && (len(*cookie) > 32768 || strings.ContainsAny(*cookie, "\r\n")) {
		return errors.New("invalid cookie")
	}
	var encrypted []byte
	if rss != nil && *rss != "" {
		u, err := url.Parse(*rss)
		if err != nil || u.User != nil || normalizedOrigin(*rss) != p.BaseURL || u.Fragment != "" || len(*rss) > 4096 {
			return errors.New("RSS must use the site HTTPS origin")
		}
		encrypted, e = s.box.seal([]byte(*rss))
		if e != nil {
			return e
		}
	}
	apply := func(tx *gorm.DB) error {
		if cookie != nil {
			r := tx.Table("sites").Where("site = ?", p.NativeCode()).Update("cookie", *cookie)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return fmt.Errorf("site no longer exists")
			}
		}
		if rss != nil {
			return tx.Save(&repository.SiteProfileSecret{ID: p.ID, RSS: encrypted}).Error
		}
		return nil
	}
	if repository.MachineTransaction(ctx) != nil {
		return apply(db)
	}
	return db.Transaction(apply)
}

// RequirePublishMapping 在任何配额预留或下载之前证明目标具备原生字段映射。
func (s *SiteProfileService) RequirePublishMapping(id string) error {
	p, e := s.Resolve(id)
	if e != nil {
		return errors.New("capability_unsupported: site not found")
	}
	cfg, e := mapping.LoadSitePublishConfig(p.NativeCode())
	if e != nil || cfg == nil || len(cfg.FormFields) == 0 || len(cfg.Mappings) == 0 {
		return errors.New("capability_unsupported: publish mapping unavailable")
	}
	return nil
}
