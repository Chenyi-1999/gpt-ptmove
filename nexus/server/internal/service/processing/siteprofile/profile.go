package siteprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Paths 是同源、无凭据的声明式 URL 模板。
type Paths struct {
	Listing  string `json:"listing"`
	Detail   string `json:"detail"`
	Download string `json:"download"`
	Search   string `json:"search"`
}

// Selectors 是有界 CSS；限制和空结果必须提供肯定证据。
type Selectors struct {
	Table              string `json:"table"`
	DetailLink         string `json:"detail_link"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	DownloadLink       string `json:"download_link"`
	Tags               string `json:"tags"`
	Peers              string `json:"peers"`
	RestrictionAllowed string `json:"restriction_allowed,omitempty"`
	RestrictionDenied  string `json:"restriction_denied,omitempty"`
	SearchEmpty        string `json:"search_empty,omitempty"`
	SearchResults      string `json:"search_results,omitempty"`
	Seeders            string `json:"seeders,omitempty"`
	Official           string `json:"official,omitempty"`
	Leechers           string `json:"leechers,omitempty"`
}

// Profile 是公开站点声明；原生身份只在进程内部使用。
type Profile struct {
	SchemaVersion  int       `json:"schema_version"`
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	BaseURL        string    `json:"base_url"`
	Roles          []string  `json:"roles"`
	Engine         string    `json:"engine"`
	PTNexusSite    string    `json:"ptnexus_site,omitempty"`
	OfficialGroups []string  `json:"official_groups"`
	AnnounceHosts  []string  `json:"announce_hosts"`
	Paths          Paths     `json:"paths"`
	Selectors      Selectors `json:"selectors"`
	Revision       int       `json:"revision,omitempty"`
	Digest         string    `json:"digest,omitempty"`
	Health         string    `json:"health,omitempty"`
	Builtin        bool      `json:"builtin,omitempty"`
	NativeNickname string    `json:"-"`
	NativeSiteCode string    `json:"-"`
}

// NativeName 返回原生迁移服务使用的站点昵称。
func (p *Profile) NativeName() string {
	if p.NativeNickname != "" {
		return p.NativeNickname
	}
	return p.ID
}

// NativeCode 返回稳定的原生站点代码。
func (p *Profile) NativeCode() string {
	if p.NativeSiteCode != "" {
		return p.NativeSiteCode
	}
	return p.ID
}

// HasRole 检查显式能力。
func (p *Profile) HasRole(role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

var slug = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var label = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var secretQuery = regexp.MustCompile(`(?i)passkey|token|password|cookie|secret|auth|key`)

// Default 返回兼容 0.2 格式的无副作用模板。
func Default() Profile {
	return Profile{SchemaVersion: 1, Engine: "nexusphp", Roles: []string{"source"}, OfficialGroups: []string{}, AnnounceHosts: []string{}, Paths: Paths{"/torrents.php", "/details.php?id={id}", "/download.php?id={id}", "/torrents.php?search={query}"}, Selectors: Selectors{Table: "table.torrents, table#torrenttable", DetailLink: `a[href*="details.php?id="]`, Title: "h1, #top, .torrent-title", Description: "#kdescr, #description, .description", DownloadLink: `a[href*="download.php"]`, Tags: ".tag, .tags, .torrent-tag, .label", Peers: "#peercount, #peers, .peers"}}
}

// PublicHostname 只做离线校验；连接时仍须校验 DNS。
func PublicHostname(host string) bool {
	if host == "" || host != strings.ToLower(host) || len(host) > 253 || !strings.Contains(host, ".") || net.ParseIP(host) != nil {
		return false
	}
	for _, s := range []string{".local", ".localhost", ".internal", ".lan", ".home", ".invalid"} {
		if strings.HasSuffix(host, s) {
			return false
		}
	}
	numeric := true
	for _, c := range host {
		if c != '.' && (c < '0' || c > '9') {
			numeric = false
		}
	}
	if numeric || alternateIP(host) {
		return false
	}
	for _, l := range strings.Split(host, ".") {
		if !label.MatchString(l) {
			return false
		}
	}
	return true
}

// Parse 严格解析至多 64KiB 的声明，拒绝未知字段与服务端元数据。
func Parse(raw []byte) (*Profile, error) {
	if len(raw) == 0 || len(raw) > 65536 {
		return nil, errors.New("profile must be 1..65536 bytes")
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil || keys == nil {
		return nil, errors.New("invalid JSON object")
	}
	for _, required := range []string{"id", "name", "base_url", "roles"} {
		if _, ok := keys[required]; !ok {
			return nil, errors.New("required profile field missing")
		}
	}
	for key, value := range keys {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) && key != "ptnexus_site" {
			return nil, errors.New("null profile field forbidden")
		}
		if key == "paths" || key == "selectors" {
			var fields map[string]json.RawMessage
			if json.Unmarshal(value, &fields) != nil || fields == nil {
				return nil, errors.New("profile section must be an object")
			}
			for _, field := range fields {
				if bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
					return nil, errors.New("null selector or path forbidden")
				}
			}
		}
	}
	for _, k := range []string{"revision", "digest", "health", "builtin"} {
		if _, ok := keys[k]; ok {
			return nil, errors.New("server-owned metadata")
		}
	}
	p := Default()
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return nil, errors.New("invalid or unknown profile fields")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	if err := Validate(&p); err != nil {
		return nil, err
	}
	canonical, _ := json.Marshal(p)
	sum := sha256.Sum256(canonical)
	p.Digest = hex.EncodeToString(sum[:])
	p.Health = "declared/unverified"
	return &p, nil
}

// Validate 离线验证身份、路径和选择器；不发请求或写入状态。
func Validate(p *Profile) error {
	if p.SchemaVersion != 1 || !slug.MatchString(p.ID) || p.Engine != "nexusphp" || !clean(p.Name, 100) || (p.PTNexusSite != "" && !slug.MatchString(p.PTNexusSite)) {
		return errors.New("invalid schema, identity, name or engine")
	}
	u, e := url.Parse(p.BaseURL)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Port() != "" && u.Port() != "443") || !PublicHostname(u.Hostname()) || len(p.BaseURL) > 512 || (u.Host != u.Hostname() && u.Host != u.Hostname()+":443") {
		return errors.New("base_url must be a public HTTPS origin")
	}
	p.BaseURL = "https://" + u.Hostname()
	seen := map[string]bool{}
	if len(p.Roles) < 1 || len(p.Roles) > 2 {
		return errors.New("invalid roles")
	}
	for _, r := range p.Roles {
		if (r != "source" && r != "target") || seen[r] {
			return errors.New("invalid roles")
		}
		seen[r] = true
	}
	if len(p.OfficialGroups) > 64 || len(p.AnnounceHosts) > 32 {
		return errors.New("too many groups or hosts")
	}
	for _, g := range p.OfficialGroups {
		if !clean(g, 100) {
			return errors.New("invalid group")
		}
	}
	for _, h := range p.AnnounceHosts {
		if !PublicHostname(h) {
			return errors.New("invalid announce host")
		}
	}
	for k, v := range map[string]string{"listing": p.Paths.Listing, "detail": p.Paths.Detail, "download": p.Paths.Download, "search": p.Paths.Search} {
		if e := safePath(k, v); e != nil {
			return e
		}
	}
	b, _ := json.Marshal(p.Selectors)
	ss := map[string]string{}
	json.Unmarshal(b, &ss)
	for _, v := range ss {
		if !clean(v, 512) || strings.Count(v, ",") > 15 || strings.Count(v, ":has(") > 2 || strings.ContainsAny(v, "{}\\") || strings.Contains(strings.ToLower(v), "javascript:") {
			return errors.New("invalid CSS selector")
		}
		if e := selector(v); e != nil {
			return e
		}
	}
	return nil
}
func clean(s string, max int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= max && !strings.ContainsFunc(s, unicode.IsControl)
}
func safePath(k, v string) error {
	if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || len(v) > 1024 || strings.ContainsAny(v, "\\\r\n\t") {
		return errors.New("unsafe relative path")
	}
	want := ""
	if k == "detail" || k == "download" {
		want = "{id}"
	}
	if k == "search" {
		want = "{query}"
	}
	if want != "" && strings.Count(v, want) != 1 {
		return errors.New("missing path placeholder")
	}
	rest := strings.ReplaceAll(v, want, "")
	if strings.ContainsAny(rest, "{}") {
		return errors.New("invalid path template")
	}
	u, e := url.Parse(v)
	if e != nil || u.Host != "" || u.Scheme != "" || u.Fragment != "" {
		return errors.New("unsafe relative path")
	}
	d, e := url.PathUnescape(u.Path)
	if e != nil || strings.Contains(d, "\\") || strings.HasPrefix(d, "//") {
		return errors.New("unsafe relative path")
	}
	for _, p := range strings.Split(d, "/") {
		if p == ".." || p == "." {
			return errors.New("unsafe relative path")
		}
	}
	for key := range u.Query() {
		if secretQuery.MatchString(key) {
			return errors.New("credentials forbidden in profile")
		}
	}
	return nil
}
func selector(v string) error {
	var q rune
	stack := []rune{}
	for _, r := range v {
		if q != 0 {
			if r == q {
				q = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			q = r
			continue
		}
		if r == '[' || r == '(' {
			stack = append(stack, r)
			continue
		}
		if r == ']' || r == ')' {
			if len(stack) == 0 {
				return errors.New("invalid CSS selector")
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if open == '[' && r != ']' || open == '(' && r != ')' {
				return errors.New("invalid CSS selector")
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(" .#:_-*=^$~|>,+", r) {
			return errors.New("invalid CSS selector")
		}
	}
	if q != 0 || len(stack) != 0 {
		return errors.New("invalid CSS selector")
	}
	return nil
}

var aliases sync.Map

// RegisterMapping 绑定已验证的原生模板代码。
func RegisterMapping(id, code string) { aliases.Store(id, code) }

// MappingCode 返回实际原生映射代码。
func MappingCode(id string) string {
	if code, ok := aliases.Load(id); ok {
		return code.(string)
	}
	return id
}

// URL 使用编码参数构造同源地址。
func (p *Profile) URL(kind, value string) (string, error) {
	var t string
	switch kind {
	case "listing":
		t = p.Paths.Listing
	case "detail":
		t = p.Paths.Detail
	case "download":
		t = p.Paths.Download
	case "search":
		t = p.Paths.Search
	default:
		return "", errors.New("unsupported path")
	}
	t = strings.ReplaceAll(t, "{id}", url.QueryEscape(value))
	t = strings.ReplaceAll(t, "{query}", url.QueryEscape(value))
	return p.BaseURL + t, nil
}

func alternateIP(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) > 4 {
		return false
	}
	for _, part := range parts {
		base := 10
		value := part
		if strings.HasPrefix(part, "0x") {
			base = 16
			value = part[2:]
		}
		if value == "" {
			return false
		}
		if _, e := strconv.ParseUint(value, base, 32); e != nil {
			return false
		}
	}
	return true
}
