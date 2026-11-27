package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	agentLinkCertificateLimit = 1 << 20
	agentLinkStateLimit       = 2 << 20
	agentLinkProbeTimeout     = 12 * time.Second
)

var (
	errAgentLinkInvalid = errors.New("invalid link settings")
	errAgentLinkProbe   = errors.New("link connection test failed")
	errAgentLinkStore   = errors.New("link settings could not be stored")
)

type agentLinkError struct {
	kind    error
	message string
}

func (e *agentLinkError) Error() string { return e.message }

func (e *agentLinkError) Unwrap() error { return e.kind }

type agentAdminConfig struct {
	Address                string `json:"address"`
	TokenConfigured        bool   `json:"token_configured"`
	CertificateConfigured  bool   `json:"certificate_configured"`
	CertificateFingerprint string `json:"certificate_fingerprint"`
	CertificateExpires     string `json:"certificate_expires"`
}

type agentLinkUpdate struct {
	Address        string `json:"address"`
	Token          string `json:"token,omitempty"`
	CertificatePEM string `json:"certificate_pem,omitempty"`
	UseSystemCAs   bool   `json:"use_system_cas,omitempty"`
}

type agentLinkSettings struct {
	Address        string `json:"address"`
	Token          string `json:"token"`
	CertificatePEM string `json:"certificate_pem"`
}

type agentLink struct {
	settings agentLinkSettings
	config   agentAdminConfig
	agent    *pullAgent
}

type agentLinkManager struct {
	statePath string
	template  *pullAgent
	current   atomic.Pointer[agentLink]
	save      sync.Mutex
}

// Load the persisted settings without requiring the old address, token or
// certificate. Those read-only bootstrap values are used only on first start.
func newAgentLinkManager(statePath string, template *pullAgent) (*agentLinkManager, error) {
	if !filepath.IsAbs(statePath) || filepath.Clean(statePath) == filepath.Dir(filepath.Clean(statePath)) {
		return nil, errors.New("PTMOVE_PULL_STATE_FILE must be an absolute file path")
	}
	settings, err := readAgentLinkSettings(statePath)
	if errors.Is(err, os.ErrNotExist) {
		settings.Address = os.Getenv("PTMOVE_PULL_URL")
		settings.Token, err = secret("PTMOVE_PULL_TOKEN")
		if err != nil {
			return nil, err
		}
		if file := os.Getenv("PTMOVE_PULL_CA"); file != "" {
			settings.CertificatePEM, err = readAgentLinkCertificate(file)
		}
	}
	if err != nil {
		return nil, err
	}
	link, err := newAgentLink(settings, template)
	if err != nil {
		return nil, err
	}
	manager := &agentLinkManager{statePath: statePath, template: template}
	manager.current.Store(link)
	return manager, nil
}

func readAgentLinkSettings(filename string) (agentLinkSettings, error) {
	var settings agentLinkSettings
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return settings, os.ErrNotExist
	}
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return settings, errors.New("领单连接配置文件不可读或权限不安全")
	}
	file, err := os.Open(filename)
	if err != nil {
		return settings, errors.New("无法读取已保存的领单连接配置")
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, agentLinkStateLimit+1))
	if err != nil || len(content) > agentLinkStateLimit {
		return settings, errors.New("无法读取已保存的领单连接配置")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&settings) != nil || decoder.Decode(new(json.RawMessage)) != io.EOF {
		return settings, errors.New("已保存的领单连接配置格式无效")
	}
	return settings, nil
}

func readAgentLinkCertificate(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", errors.New("无法读取初始服务端证书")
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, agentLinkCertificateLimit+1))
	if err != nil || len(content) > agentLinkCertificateLimit {
		return "", errors.New("无法读取初始服务端证书")
	}
	return string(content), nil
}

func (m *agentLinkManager) currentAgent() *pullAgent {
	return m.current.Load().agent
}

func (m *agentLinkManager) currentConfig() agentAdminConfig {
	return m.current.Load().config
}

func (m *agentLinkManager) testCurrent(ctx context.Context) (agentAdminConfig, error) {
	link := m.current.Load()
	return link.config, link.probe(ctx)
}

func (m *agentLinkManager) saveAndTest(ctx context.Context, update agentLinkUpdate) (agentAdminConfig, error) {
	m.save.Lock()
	defer m.save.Unlock()

	old := m.current.Load()
	if ctx.Err() != nil {
		return old.config, &agentLinkError{errAgentLinkProbe, "连接测试已取消"}
	}
	address, err := managedAgentOrigin(update.Address)
	if err != nil {
		return old.config, err
	}
	settings := old.settings
	settings.Address = address
	if strings.TrimSpace(update.Token) != "" {
		settings.Token = update.Token
	} else if address != old.settings.Address {
		return old.config, &agentLinkError{errAgentLinkInvalid, "变更服务端地址时，请输入新服务端的领单令牌"}
	}
	switch {
	case update.UseSystemCAs && strings.TrimSpace(update.CertificatePEM) != "":
		return old.config, &agentLinkError{errAgentLinkInvalid, "请在上传证书和使用系统信任证书之间选择一项"}
	case update.UseSystemCAs:
		settings.CertificatePEM = ""
	case strings.TrimSpace(update.CertificatePEM) != "":
		settings.CertificatePEM = update.CertificatePEM
	}
	link, err := newAgentLink(settings, m.template)
	if err != nil {
		return old.config, err
	}
	applied := false
	defer func() {
		if !applied {
			link.agent.coordinator.CloseIdleConnections()
		}
	}()
	if err = link.probe(ctx); err != nil {
		return old.config, err
	}
	if err = saveAgentLinkSettings(ctx, m.statePath, link.settings); err != nil {
		return old.config, err
	}
	m.current.Store(link)
	applied = true
	old.agent.coordinator.CloseIdleConnections()
	return link.config, nil
}

// The on-disk rename happens before the new connection is exposed to the
// workers. It never touches the bootstrap credential files.
func saveAgentLinkSettings(ctx context.Context, filename string, settings agentLinkSettings) error {
	cannotSave := &agentLinkError{errAgentLinkStore, "无法保存领单连接配置，请检查客户端配置目录是否可写"}
	content, err := json.Marshal(settings)
	if err != nil || len(content) > agentLinkStateLimit {
		return &agentLinkError{errAgentLinkInvalid, "领单配置超过大小限制"}
	}
	dir, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return cannotSave
	}
	defer dir.Close()
	file, err := os.CreateTemp(filepath.Dir(filename), ".link-settings-*")
	if err != nil {
		return cannotSave
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return cannotSave
	}
	if ctx.Err() != nil {
		return &agentLinkError{errAgentLinkProbe, "连接配置保存已取消"}
	}
	if err = os.Rename(file.Name(), filename); err != nil {
		return cannotSave
	}
	// A successful rename has committed the file even if the filesystem does
	// not support syncing directories. Apply the same settings in memory.
	_ = dir.Sync()
	return nil
}

func managedAgentOrigin(raw string) (string, error) {
	value, err := agentCoordinatorOrigin(raw)
	if err != nil {
		return "", &agentLinkError{errAgentLinkInvalid, "请输入有效的 HTTPS 服务端地址"}
	}
	origin, _ := url.Parse(value)
	host := strings.ToLower(origin.Hostname())
	ip := net.ParseIP(host)
	if ip.Equal(net.IPv4(81, 70, 24, 5)) {
		return "", &agentLinkError{errAgentLinkInvalid, "该服务端地址受保护"}
	}
	if ip == nil && !validAgentHostname(host) {
		return "", &agentLinkError{errAgentLinkInvalid, "请输入有效的服务端 IP 或域名"}
	}
	if ip != nil {
		host = ip.String()
	}
	port := origin.Port()
	if port != "" {
		number, _ := strconv.Atoi(port)
		port = strconv.Itoa(number)
	}
	switch {
	case port != "" && port != "443":
		origin.Host = net.JoinHostPort(host, port)
	case ip != nil && ip.To4() == nil:
		origin.Host = "[" + host + "]"
	default:
		origin.Host = host
	}
	return origin.String(), nil
}

func validAgentHostname(host string) bool {
	host = strings.TrimSuffix(host, ".")
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func newAgentLink(settings agentLinkSettings, template *pullAgent) (*agentLink, error) {
	address, err := managedAgentOrigin(settings.Address)
	if err != nil {
		return nil, err
	}
	settings.Address = address
	settings.Token = strings.TrimSpace(settings.Token)
	if len(settings.Token) < 16 || len(settings.Token) > 1024 || strings.ContainsFunc(settings.Token, func(char rune) bool { return char < 33 || char > 126 }) {
		return nil, &agentLinkError{errAgentLinkInvalid, "请输入有效的领单令牌（16 至 1024 个非空格 ASCII 字符）"}
	}
	settings.CertificatePEM = strings.TrimSpace(settings.CertificatePEM)
	roots, first, err := parseAgentLinkCertificates(settings.CertificatePEM)
	if err != nil {
		return nil, err
	}
	config := agentAdminConfig{
		Address:               address,
		TokenConfigured:       true,
		CertificateConfigured: first != nil,
	}
	if first != nil {
		config.CertificateFingerprint = fmt.Sprintf("%X", sha256.Sum256(first.Raw))
		config.CertificateExpires = first.NotAfter.UTC().Format(time.RFC3339)
	}
	agent := *template
	agent.origin = address
	agent.token = settings.Token
	agent.coordinator = newAgentClient(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, false)
	return &agentLink{settings: settings, config: config, agent: &agent}, nil
}

func parseAgentLinkCertificates(text string) (*x509.CertPool, *x509.Certificate, error) {
	if len(text) == 0 {
		return nil, nil, nil
	}
	invalid := &agentLinkError{errAgentLinkInvalid, "请上传 PEM 证书，文件不得包含私钥或其他内容"}
	if len(text) > agentLinkCertificateLimit {
		return nil, nil, &agentLinkError{errAgentLinkInvalid, "证书文件超过 1 MiB"}
	}
	roots := x509.NewCertPool()
	var first *x509.Certificate
	for remaining := []byte(text); len(remaining) > 0; {
		// pem.Decode can silently skip a malformed block and unrelated text.
		// Require each piece to begin with exactly one complete certificate.
		const begin = "-----BEGIN CERTIFICATE-----"
		const end = "-----END CERTIFICATE-----"
		if !bytes.HasPrefix(remaining, []byte(begin)) {
			return nil, nil, invalid
		}
		finish := bytes.Index(remaining, []byte(end))
		if finish < len(begin) || bytes.Contains(remaining[len(begin):finish], []byte(begin)) {
			return nil, nil, invalid
		}
		block, tail := pem.Decode(remaining[:finish+len(end)])
		if block == nil || len(tail) != 0 || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, nil, invalid
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, nil, invalid
		}
		roots.AddCert(certificate)
		if first == nil {
			first = certificate
		}
		remaining = bytes.TrimSpace(remaining[finish+len(end):])
	}
	return roots, first, nil
}

// An authenticated claim with no lane is rejected before any queue access.
// Matching that exact reply proves TLS, token and protocol on existing hubs.
func (link *agentLink) probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, agentLinkProbeTimeout)
	defer cancel()
	resp, err := link.agent.post(ctx, "/v1/claim", []byte(`{}`))
	if err != nil {
		var certificateErr *tls.CertificateVerificationError
		var networkErr net.Error
		switch {
		case errors.As(err, &certificateErr):
			return &agentLinkError{errAgentLinkProbe, "证书验证失败，请核对证书及其覆盖的 IP 或域名"}
		case errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &networkErr) && networkErr.Timeout()):
			return &agentLinkError{errAgentLinkProbe, "连接测试超时，请核对服务端地址和端口"}
		default:
			return &agentLinkError{errAgentLinkProbe, "无法连接领单服务，请核对地址、端口和 HTTPS 配置"}
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &agentLinkError{errAgentLinkProbe, "服务端未接受领单令牌，请核对后重试"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil || resp.StatusCode != http.StatusBadRequest || string(body) != "unknown lane\n" {
		return &agentLinkError{errAgentLinkProbe, "该端口未返回预期的领单服务响应"}
	}
	return nil
}
