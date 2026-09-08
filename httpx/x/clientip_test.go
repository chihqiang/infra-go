package x

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// trustProxy 构造一个直连为可信代理（127.0.0.1）的请求。
// 直连不可信时代理头会被忽略，因此涉及“代理头解析”的用例必须先落到可信对端。
func trustProxy(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = "127.0.0.1:12345"
	return r
}

// --- ClientIP：直连（无代理头 / 直连不可信） ---

func TestClientIP_RemoteAddr(t *testing.T) {
	// 无代理头时回退 RemoteAddr 并去掉端口
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	assert.Equal(t, "1.2.3.4", ClientIP(r))

	// IPv6 带端口
	r.RemoteAddr = "[2001:db8::1]:5678"
	assert.Equal(t, "2001:db8::1", ClientIP(r))

	// RemoteAddr 无端口：原样返回
	r.RemoteAddr = "1.2.3.4"
	assert.Equal(t, "1.2.3.4", ClientIP(r))
}

func TestClientIP_UntrustedDirectIgnoresProxyHeaders(t *testing.T) {
	// 直连对端是公网（不可信），视为真实客户端直连：
	// 即便携带伪造的代理头也必须忽略，只信 RemoteAddr。
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	r.Header.Set(HeaderXForwardedFor, "5.5.5.5, 6.6.6.6")
	r.Header.Set(HeaderXRealIP, "7.7.7.7")
	assert.Equal(t, "1.2.3.4", ClientIP(r))
}

func TestClientIP_EmptyAndNil(t *testing.T) {
	// 无可信来源
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = ""
	assert.Equal(t, "", ClientIP(r))

	assert.Equal(t, "", ClientIP(nil))
}

// --- ClientIP：可信代理后（默认解析器） ---

func TestClientIP_XRealIP(t *testing.T) {
	r := trustProxy(http.MethodGet, "/")
	r.Header.Set(HeaderXRealIP, "5.5.5.5")
	assert.Equal(t, "5.5.5.5", ClientIP(r))
}

func TestClientIP_XForwardedFor_FromRight(t *testing.T) {
	// 可信代理后，X-Forwarded-For 从最右往左跳过可信代理，取首个不可信 IP。
	r := trustProxy(http.MethodGet, "/")
	r.Header.Set(HeaderXForwardedFor, "203.0.113.9, 70.41.3.18, 150.172.238.178")
	assert.Equal(t, "150.172.238.178", ClientIP(r))

	// X-Forwarded-For 优先于 X-Real-IP
	r.Header.Set(HeaderXRealIP, "5.5.5.5")
	assert.Equal(t, "150.172.238.178", ClientIP(r))
}

func TestClientIP_XForwardedFor_Antiforgery(t *testing.T) {
	// 恶意客户端伪造最左前缀（1.1.1.1 是伪造的）：安全解析应取真实客户端 203.0.113.9，
	// 而非最左的伪造值。
	r := trustProxy(http.MethodGet, "/")
	r.Header.Set(HeaderXForwardedFor, "1.1.1.1, 203.0.113.9")
	assert.Equal(t, "203.0.113.9", ClientIP(r))
}

func TestClientIP_XForwardedFor_SkipTrustedHops(t *testing.T) {
	// XFF 链中夹带可信内网跳点（10.0.0.5 被可信代理追加），从右往左跳过可信项。
	r := trustProxy(http.MethodGet, "/")
	r.Header.Set(HeaderXForwardedFor, "203.0.113.9, 10.0.0.5")
	assert.Equal(t, "203.0.113.9", ClientIP(r))

	// 整条链都是可信代理：取最左侧（最原始）值。
	r.Header.Set(HeaderXForwardedFor, "10.0.0.5, 10.0.0.6")
	assert.Equal(t, "10.0.0.5", ClientIP(r))
}

func TestClientIP_Forwarded(t *testing.T) {
	r := trustProxy(http.MethodGet, "/")
	r.Header.Set(HeaderForwarded, "for=203.0.113.60;proto=https;host=example.com")
	assert.Equal(t, "203.0.113.60", ClientIP(r))

	// 带引号与 IPv6 中括号
	r.Header.Set(HeaderForwarded, `for="[2001:db8::1]:4704";proto=https`)
	assert.Equal(t, "2001:db8::1", ClientIP(r))

	// for=unknown 等非 IP 值被忽略，回退 RemoteAddr 的可信对端
	r.Header.Set(HeaderForwarded, "for=unknown;proto=http")
	assert.Equal(t, "127.0.0.1", ClientIP(r))
}

// --- ClientIPWithTrustedProxies：追加可信网段 ---

func TestClientIPWithTrustedProxies(t *testing.T) {
	// 流量经公网 CDN（203.0.113.0/24）回源：把 CDN 出口网段追加为可信后，
	// 从右往左跳过该网段，取更原始的真实客户端。
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.10:8080" // CDN 出口
	r.Header.Set(HeaderXForwardedFor, "8.8.8.8, 203.0.113.5")
	// 未追加可信网段时：203.0.113.10 视为直连客户端
	assert.Equal(t, "203.0.113.10", ClientIP(r))
	// 追加 CDN 网段后：识别出更原始客户端
	assert.Equal(t, "8.8.8.8", ClientIPWithTrustedProxies(r, "203.0.113.0/24"))
}

// --- IPChecker：自定义厂商头 ---

func TestIPChecker_VendorHeaders(t *testing.T) {
	cf := NewIPChecker(WithVendorHeaders(HeaderCFConnectingIP))

	r := trustProxy(http.MethodGet, "/")
	r.Header.Set(HeaderCFConnectingIP, "7.7.7.7")
	r.Header.Set(HeaderXForwardedFor, "9.9.9.9")
	// 启用 CF 头：优先返回 CF-Connecting-IP
	assert.Equal(t, "7.7.7.7", cf.ClientIP(r))
	// 默认解析器不启用厂商头：忽略 CF-Connecting-IP，走 XFF
	assert.Equal(t, "9.9.9.9", ClientIP(r))

	// 直连不可信时厂商头同样不可信
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "8.8.8.8:80"
	r2.Header.Set(HeaderCFConnectingIP, "7.7.7.7")
	assert.Equal(t, "8.8.8.8", cf.ClientIP(r2))
}
