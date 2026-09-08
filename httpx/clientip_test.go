package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- 客户端 IP（httpx 主包便捷入口，底层转发 httpx/x） ---

func TestClientIP_RemoteAddrFallback(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	assert.Equal(t, "1.2.3.4", ClientIP(r))
}

func TestClientIP_ProxyHeaders(t *testing.T) {
	// 直连为可信代理（回环）时解析代理头
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.9")
	// 防伪造：跳过可信右侧，取真实客户端
	assert.Equal(t, "203.0.113.9", ClientIP(r))

	r.Header.Set("X-Real-IP", "8.8.8.8")
	assert.Equal(t, "203.0.113.9", ClientIP(r))
}

func TestClientIP_Nil(t *testing.T) {
	assert.Equal(t, "", ClientIP(nil))
}

func TestClientIPWithTrustedProxies(t *testing.T) {
	// 流量经公网 CDN（203.0.113.0/24）回源：追加可信网段后取更原始客户端
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.10:8080"
	r.Header.Set("X-Forwarded-For", "8.8.8.8, 203.0.113.5")

	assert.Equal(t, "203.0.113.10", ClientIP(r)) // 未追加：CDN 出口视为直连
	assert.Equal(t, "8.8.8.8", ClientIPWithTrustedProxies(r, "203.0.113.0/24"))
}
