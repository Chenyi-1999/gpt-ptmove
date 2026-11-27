package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

//go:embed admin.html
var agentAdminPage string

const agentAdminRequestLimit = 2 << 20

type agentAdminLinks interface {
	currentConfig() agentAdminConfig
	saveAndTest(context.Context, agentLinkUpdate) (agentAdminConfig, error)
	testCurrent(context.Context) (agentAdminConfig, error)
}

type agentAdminResponse struct {
	OK      bool              `json:"ok"`
	Message string            `json:"message"`
	Config  *agentAdminConfig `json:"config,omitempty"`
}

type agentAdminAccess struct {
	lan       netip.Prefix
	authority string
}

var agentAdminPrivateNetworks = [...]netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

func parseAgentAdminAccess(address, cidr string) (agentAdminAccess, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || !validAdminPort(port) {
		return agentAdminAccess{}, errors.New("PTMOVE_PULL_ADMIN_LISTEN must be an IPv4 address and valid port")
	}
	if host == "127.0.0.1" && cidr == "" {
		return agentAdminAccess{}, nil
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() || !ip.IsPrivate() {
		return agentAdminAccess{}, errors.New("PTMOVE_PULL_ADMIN_LISTEN must be 127.0.0.1 or a specific private IPv4 address")
	}
	lan, err := netip.ParsePrefix(cidr)
	if err == nil && lan.Addr().Is4() && lan.Contains(ip) {
		lan = lan.Masked()
		for _, private := range agentAdminPrivateNetworks {
			if lan.Bits() >= private.Bits() && private.Contains(lan.Addr()) {
				return agentAdminAccess{lan: lan, authority: address}, nil
			}
		}
	}
	return agentAdminAccess{}, errors.New("PTMOVE_PULL_ADMIN_LAN_CIDR must be a private IPv4 subnet containing the listen address")
}

func (access agentAdminAccess) allows(request *http.Request) bool {
	peer, err := netip.ParseAddrPort(request.RemoteAddr)
	if err != nil {
		return false
	}
	if access.lan.IsValid() {
		// Accept only the published literal authority. Forwarded addresses
		// cannot bypass this check or the peer subnet check.
		return access.lan.Contains(peer.Addr().Unmap()) && request.Host == access.authority
	}
	return peer.Addr().IsLoopback() && agentAdminHostAllowed(request.Host)
}

// Management uses loopback by default. A LAN listener requires an explicit
// private address and a trusted subnet.
func startAgentAdmin(ctx context.Context, address, password string, links *agentLinkManager) (func(), error) {
	access, err := parseAgentAdminAccess(address, os.Getenv("PTMOVE_PULL_ADMIN_LAN_CIDR"))
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, err
	}

	server := &http.Server{
		Handler:                      newAgentAdminHandler(password, links, access),
		ReadHeaderTimeout:            4 * time.Second,
		ReadTimeout:                  10 * time.Second,
		WriteTimeout:                 25 * time.Second,
		IdleTimeout:                  30 * time.Second,
		MaxHeaderBytes:               8 << 10,
		DisableGeneralOptionsHandler: true,
		BaseContext:                  func(net.Listener) context.Context { return ctx },
	}
	go func() { _ = server.Serve(listener) }()
	return func() { _ = server.Close() }, nil
}

func newAgentAdminHandler(password string, links agentAdminLinks, access agentAdminAccess) http.Handler {
	expected := sha256.Sum256([]byte(password))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")

		if !access.allows(r) {
			http.Error(w, "当前访问地址不在管理页允许的范围内", http.StatusForbidden)
			return
		}
		user, provided, ok := r.BasicAuth()
		actual := sha256.Sum256([]byte(provided))
		if !ok || user != "admin" || password == "" || subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="STARPT Media Client", charset="UTF-8"`)
			http.Error(w, "请使用媒体客户端管理密码登录", http.StatusUnauthorized)
			return
		}
		if r.URL.RawQuery != "" || r.URL.EscapedPath() != r.URL.Path {
			http.NotFound(w, r)
			return
		}

		switch {
		case r.URL.Path == "/" && r.Method == http.MethodGet:
			serveAgentAdminPage(w)
		case r.URL.Path == "/api/config" && r.Method == http.MethodGet:
			writeAgentAdminJSON(w, http.StatusOK, links.currentConfig())
		case r.Method == http.MethodPost && (r.URL.Path == "/api/config" || r.URL.Path == "/api/test"):
			if r.Header.Get("Origin") != "http://"+r.Host ||
				r.Header.Get("X-STARPT-Request") != "1" ||
				(r.Header.Get("Sec-Fetch-Site") != "" && r.Header.Get("Sec-Fetch-Site") != "same-origin") {
				writeAgentAdminJSON(w, http.StatusForbidden, agentAdminResponse{Message: "请在媒体客户端页面提交设置"})
				return
			}
			updateAgentAdmin(w, r, links)
		case r.URL.Path == "/" || r.URL.Path == "/api/config" || r.URL.Path == "/api/test":
			allowed := http.MethodGet
			if r.URL.Path == "/api/config" {
				allowed = "GET, POST"
			} else if r.URL.Path == "/api/test" {
				allowed = http.MethodPost
			}
			w.Header().Set("Allow", allowed)
			http.Error(w, "此操作不支持该请求方法", http.StatusMethodNotAllowed)
		default:
			http.NotFound(w, r)
		}
	})
}

func validAdminPort(port string) bool {
	number, err := strconv.Atoi(port)
	return err == nil && number > 0 && number < 65536
}

func agentAdminHostAllowed(address string) bool {
	host := address
	if strings.Contains(address, ":") {
		var port string
		var err error
		host, port, err = net.SplitHostPort(address)
		if err != nil || !validAdminPort(port) {
			return false
		}
	}
	return host == "127.0.0.1" || strings.EqualFold(host, "localhost")
}

func serveAgentAdminPage(w http.ResponseWriter) {
	var random [18]byte
	if _, err := rand.Read(random[:]); err != nil {
		http.Error(w, "暂时无法加载设置页", http.StatusInternalServerError)
		return
	}
	nonce := base64.RawStdEncoding.EncodeToString(random[:])
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+
			"'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(strings.ReplaceAll(agentAdminPage, "{{nonce}}", nonce)))
}

func updateAgentAdmin(w http.ResponseWriter, r *http.Request, links agentAdminLinks) {
	var config agentAdminConfig
	var err error
	message := "连接正常，服务端已验证客户端令牌。"
	if r.URL.Path == "/api/config" {
		var update agentLinkUpdate
		if parseErr := decodeBounded(w, r, agentAdminRequestLimit, &update); parseErr != nil {
			rejectAgentAdminBody(w, parseErr)
			return
		}
		config, err = links.saveAndTest(r.Context(), update)
		message = "连接验证通过，设置已保存。新任务将使用新地址。"
	} else {
		var request struct{}
		if parseErr := decodeBounded(w, r, 1024, &request); parseErr != nil {
			rejectAgentAdminBody(w, parseErr)
			return
		}
		config, err = links.testCurrent(r.Context())
	}
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errAgentLinkProbe) {
			status = http.StatusBadGateway
		} else if errors.Is(err, errAgentLinkStore) {
			status = http.StatusInternalServerError
		}
		writeAgentAdminJSON(w, status, agentAdminResponse{Message: err.Error()})
		return
	}
	writeAgentAdminJSON(w, http.StatusOK, agentAdminResponse{OK: true, Message: message, Config: &config})
}

func rejectAgentAdminBody(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeAgentAdminJSON(w, http.StatusRequestEntityTooLarge, agentAdminResponse{Message: "请求过大，请使用不超过 1 MiB 的证书。"})
		return
	}
	writeAgentAdminJSON(w, http.StatusBadRequest, agentAdminResponse{Message: "提交内容格式有误，请重新加载设置页。"})
}

func writeAgentAdminJSON(w http.ResponseWriter, status int, response any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
