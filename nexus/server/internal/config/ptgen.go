package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	ptgenSourcesFile = "ptgen-sources.json"
	maxPTGenSources  = 12
	maxPTGenFileSize = 64 << 10
)

var (
	ErrPTGenSettingsUnavailable = errors.New("PTGen 来源配置暂不可读")
	ErrPTGenSettingsWrite       = errors.New("无法保存 PTGen 来源配置")
	ptgenSourceID               = regexp.MustCompile(`^[a-f0-9]{32}$`)
	ptgenDNSName                = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)
	protectedPTGenAddr          = netip.MustParseAddr("81.70.24.5")
	privatePTGenPrefixes        = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("2001::/23"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("3fff::/20"),
	}
	globalPTGenIPv6 = netip.MustParsePrefix("2000::/3")
)

// PTGenSource is a saved and validated provider. The access token is kept in the
// dedicated configuration file and excluded from responses at the settings API.
type PTGenSource struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	URL   string `json:"url"`
	Token string `json:"token,omitempty"`
}

type PTGenSourceInput struct {
	ID                 string `json:"id,omitempty"`
	Type               string `json:"type"`
	URL                string `json:"url"`
	Token              string `json:"token,omitempty"`
	ClearToken         bool   `json:"clear_token,omitempty"`
	UseLegacyCSPTToken bool   `json:"use_legacy_cspt_token,omitempty"`
}

type PTGenSourceView struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	ResolvedType    string `json:"resolved_type"`
	URL             string `json:"url"`
	TokenConfigured bool   `json:"token_configured"`
}

type PTGenSettings struct {
	Sources              []PTGenSourceView `json:"sources"`
	LegacyCSPTConfigured bool              `json:"legacy_cspt_configured"`
}

// IsPTGenPublicAddress rejects local addresses, the protected host and address
// forms that can route to a non-public IPv4 network.
func IsPTGenPublicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr == protectedPTGenAddr ||
		(addr.Is6() && !globalPTGenIPv6.Contains(addr)) {
		return false
	}
	for _, prefix := range privatePTGenPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func normalizePTGenEndpoint(rawURL string) (string, *url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	endpoint, err := url.Parse(rawURL)
	if len(rawURL) > 2048 || err != nil || endpoint.Scheme != "https" || endpoint.User != nil ||
		endpoint.Hostname() == "" || endpoint.Opaque != "" || endpoint.ForceQuery ||
		endpoint.RawQuery != "" || strings.Contains(rawURL, "#") || strings.ContainsAny(rawURL, "\r\n\t") {
		return "", nil, errors.New("请填写完整 HTTPS 地址，查询参数和令牌分开填写")
	}
	host := strings.ToLower(strings.TrimSuffix(endpoint.Hostname(), "."))
	if addr, err := netip.ParseAddr(host); err == nil {
		if !IsPTGenPublicAddress(addr) || addr.Zone() != "" {
			return "", nil, errors.New("PTGen 地址需要是可公开访问的地址")
		}
	} else if !strings.Contains(host, ".") || !ptgenDNSName.MatchString(host) ||
		strings.HasSuffix(host, ".localhost") || strings.Contains(host, "..") {
		return "", nil, errors.New("PTGen 地址需要是有效的公网域名")
	}
	if port := endpoint.Port(); port != "" && port != "443" {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return "", nil, errors.New("PTGen 地址端口无效")
		}
		endpoint.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		endpoint.Host = "[" + host + "]"
	} else {
		endpoint.Host = host
	}
	return endpoint.String(), endpoint, nil
}

// ResolvePTGenType recognizes protocol-specific paths without sending a
// network request or exposing the token. Ambiguous endpoints need a type.
func ResolvePTGenType(kind, rawURL string) (string, error) {
	_, u, err := normalizePTGenEndpoint(rawURL)
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(u.Path, "/")
	switch kind {
	case "auto":
		switch {
		case u.Host == "cspt.top":
			kind = "cspt"
		case strings.HasSuffix(path, "/api/v2/info"):
			kind = "universal"
		case strings.HasSuffix(path, "/infogen"),
			u.Host == "ourbits.github.io" && strings.EqualFold(path, "/PtGen"),
			u.Host == "cdn.ourhelp.club" && strings.EqualFold(path, "/ptgen"):
			kind = "ourbits"
		default:
			return "", errors.New("无法从地址识别接口类型，请手动选择")
		}
	case "cspt", "universal", "ourbits", "generic":
	default:
		return "", errors.New("不支持的 PTGen 接口类型")
	}
	if kind == "cspt" && (u.Host != "cspt.top" || (path != "" && path != "/api/ptgen/query")) {
		return "", errors.New("财神请选择 cspt.top 官方地址")
	}
	return kind, nil
}

func validPTGenToken(value string) bool {
	if len(value) > 2000 {
		return false
	}
	for _, ch := range value {
		if ch < 33 || ch > 126 {
			return false
		}
	}
	return true
}

func (m *Manager) legacyCSPTToken() string {
	crossSeed, _ := asMap(m.config["cross_seed"])
	token, _ := crossSeed["cspt_ptgen_token"].(string)
	return strings.TrimSpace(token)
}

func (m *Manager) PTGenSources() []PTGenSource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.ptgenSources)
}

func (m *Manager) GetPTGenSettings() (PTGenSettings, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.ptgenLoadFailed {
		return PTGenSettings{}, ErrPTGenSettingsUnavailable
	}
	return m.ptgenSettingsView(), nil
}

func (m *Manager) ptgenSettingsView() PTGenSettings {
	view := PTGenSettings{
		Sources:              make([]PTGenSourceView, 0, len(m.ptgenSources)),
		LegacyCSPTConfigured: m.legacyCSPTToken() != "",
	}
	for _, source := range m.ptgenSources {
		kind, _ := ResolvePTGenType(source.Type, source.URL)
		view.Sources = append(view.Sources, PTGenSourceView{
			ID: source.ID, Type: source.Type, URL: source.URL, ResolvedType: kind, TokenConfigured: source.Token != "",
		})
	}
	return view
}

func (m *Manager) SavePTGenSettings(rows []PTGenSourceInput) (PTGenSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ptgenLoadFailed {
		return PTGenSettings{}, ErrPTGenSettingsUnavailable
	}
	if len(rows) > maxPTGenSources {
		return PTGenSettings{}, fmt.Errorf("最多可以配置 %d 条 PTGen 来源", maxPTGenSources)
	}

	existing := make(map[string]PTGenSource, len(m.ptgenSources))
	for _, source := range m.ptgenSources {
		existing[source.ID] = source
	}
	next := make([]PTGenSource, 0, len(rows))
	for idx, row := range rows {
		old, ok := existing[row.ID]
		if row.ID != "" && !ok {
			return PTGenSettings{}, fmt.Errorf("第 %d 条来源已变动，请重新载入设置", idx+1)
		}
		source, err := buildPTGenSource(row, old, m.legacyCSPTToken())
		if err != nil {
			return PTGenSettings{}, fmt.Errorf("第 %d 条来源：%w", idx+1, err)
		}
		delete(existing, row.ID)
		next = append(next, source)
	}
	if err := m.persistPTGenSources(next); err != nil {
		return PTGenSettings{}, ErrPTGenSettingsWrite
	}
	m.ptgenSources = next
	return m.ptgenSettingsView(), nil
}

func buildPTGenSource(row PTGenSourceInput, old PTGenSource, legacy string) (PTGenSource, error) {
	endpoint, _, err := normalizePTGenEndpoint(row.URL)
	if err != nil {
		return PTGenSource{}, err
	}
	kind := strings.ToLower(strings.TrimSpace(row.Type))
	resolved, err := ResolvePTGenType(kind, endpoint)
	if err != nil {
		return PTGenSource{}, err
	}
	token := strings.TrimSpace(row.Token)
	switch {
	case row.ClearToken && (token != "" || row.UseLegacyCSPTToken):
		return PTGenSource{}, errors.New("请单独选择设定或清除令牌")
	case row.ClearToken:
		token = ""
	case row.UseLegacyCSPTToken:
		if kind != "cspt" || legacy == "" || token != "" {
			return PTGenSource{}, errors.New("请显式选择财神类型并核对已保存的令牌")
		}
		token = legacy
	case token == "" && old.Token != "":
		oldKind, _ := ResolvePTGenType(old.Type, old.URL)
		if old.URL != endpoint || oldKind != resolved {
			return PTGenSource{}, errors.New("地址或协议已变更，请重新填写或清除该行令牌")
		}
		token = old.Token
	}
	if !validPTGenToken(token) {
		return PTGenSource{}, errors.New("令牌格式无效")
	}
	if resolved == "cspt" {
		if token == "" {
			return PTGenSource{}, errors.New("财神接口需要令牌")
		}
		if token == "." || token == ".." || strings.ContainsAny(token, `/\`) {
			return PTGenSource{}, errors.New("财神令牌格式无效")
		}
	}
	id := row.ID
	if id == "" {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return PTGenSource{}, ErrPTGenSettingsWrite
		}
		id = hex.EncodeToString(raw[:])
	}
	return PTGenSource{ID: id, Type: kind, URL: endpoint, Token: token}, nil
}

func (m *Manager) ptgenSourcesPath() string {
	return filepath.Join(filepath.Dir(m.path), ptgenSourcesFile)
}

func (m *Manager) loadPTGenSources() {
	file, err := os.Open(m.ptgenSourcesPath())
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		m.ptgenLoadFailed = true
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxPTGenFileSize+1))
	var rows []PTGenSource
	if err != nil || len(data) > maxPTGenFileSize || json.Unmarshal(data, &rows) != nil ||
		rows == nil || len(rows) > maxPTGenSources {
		m.ptgenLoadFailed = true
		return
	}
	seen := make(map[string]bool, len(rows))
	for i, source := range rows {
		if !ptgenSourceID.MatchString(source.ID) || seen[source.ID] {
			m.ptgenLoadFailed = true
			return
		}
		normalized, validateErr := buildPTGenSource(PTGenSourceInput{
			ID: source.ID, Type: source.Type, URL: source.URL, Token: source.Token,
		}, PTGenSource{}, "")
		if validateErr != nil {
			m.ptgenLoadFailed = true
			return
		}
		seen[source.ID] = true
		rows[i] = normalized
	}
	m.ptgenSources = rows
}

func (m *Manager) persistPTGenSources(rows []PTGenSource) error {
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > maxPTGenFileSize {
		return errors.New("PTGen 来源配置超过大小限制")
	}
	file, err := os.CreateTemp(filepath.Dir(m.path), ".ptgen-sources-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), m.ptgenSourcesPath())
}
