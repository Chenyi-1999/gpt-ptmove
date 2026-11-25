package netproxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pt-nexus/server/internal/config"
	"golang.org/x/net/dns/dnsmessage"
)

// 本测试仅使用本机DNS双桩；受保护IP只作为离线解析数据，不建立到该地址的连接。
func TestProtectedTargetRejectsAllDNSAnswers(t *testing.T) {
	socket, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, peer, err := socket.ReadFrom(buffer)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buffer[:n]) != nil {
				continue
			}
			reply := dnsmessage.Message{Header: dnsmessage.Header{ID: query.Header.ID, Response: true, RecursionAvailable: true}, Questions: query.Questions}
			for _, question := range query.Questions {
				if question.Type == dnsmessage.TypeA {
					for _, ip := range [][4]byte{{127, 0, 0, 1}, {81, 70, 24, 5}} {
						reply.Answers = append(reply.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 1}, Body: &dnsmessage.AResource{A: ip}})
					}
				}
			}
			encoded, _ := reply.Pack()
			socket.WriteTo(encoded, peer)
		}
	}()
	original := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", socket.LocalAddr().String())
	}}
	defer func() { net.DefaultResolver = original }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	if err := ValidateTargetURL(ctx, "https://blocked.test/"); err == nil {
		t.Fatal("mixed DNS answer bypassed protected target")
	}
	for _, raw := range []string{"http://81.70.24.5/", "http://[::ffff:81.70.24.5]/"} {
		if ValidateTargetURL(ctx, raw) == nil {
			t.Fatal("protected literal accepted")
		}
	}
}

func TestProtectedRedirectNeverDialsDeniedTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://81.70.24.5/", http.StatusFound)
	}))
	defer server.Close()
	client := &http.Client{Transport: ConfigureTransport(&http.Transport{}), Timeout: time.Second}
	if response, err := client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("redirect reached a protected target")
	}
}

func TestShortLivedTransportKeepsProtectionWithoutRegistration(t *testing.T) {
	originalDefault, originalManager := http.DefaultTransport, globalManager
	t.Cleanup(func() {
		http.DefaultTransport, globalManager = originalDefault, originalManager
	})
	if err := Install(config.NetworkProxyConfig{}); err != nil {
		t.Fatal(err)
	}
	start := len(globalManager.transports)
	transport := ConfigureShortLivedTransport(&http.Transport{})
	if len(globalManager.transports) != start {
		t.Fatal("单次操作的 transport 被长期登记")
	}
	req, err := http.NewRequest(http.MethodGet, "http://81.70.24.5/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.Proxy(req); err == nil {
		t.Fatal("单次操作 transport 未阻止受保护地址")
	}
}
