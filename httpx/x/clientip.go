// 客户端真实 IP 解析：面向“部署于可信反向代理之后”的服务。
//
// 与中间件本体无关，属 httpx 通用小工具（收敛于本 x 包），便于复用与
// 按 IP 维度（限流/审计）高频调用。详见 IPChecker / ClientIP 注释。
package x

import (
	"net"
	"net/http"
	"strings"
	"sync"
)

// --- 常见代理头常量 ---

const (
	// HeaderXForwardedFor X-Forwarded-For：反向代理按转发链路追加客户端 IP，
	// 多个值以英文逗号分隔，最左侧为最原始客户端、最右侧为离本服务最近的一跳。
	HeaderXForwardedFor = "X-Forwarded-For"

	// HeaderXRealIP X-Real-IP：单层代理（如 Nginx proxy_set_header X-Real-IP $remote_addr）
	// 直接写入真实客户端 IP。
	HeaderXRealIP = "X-Real-IP"

	// HeaderForwarded RFC 7239 标准化的转发头，形如：
	//   Forwarded: for=203.0.113.7;proto=https;host=example.com
	// for 参数可带引号（for="1.2.3.4"）或 IPv6 中括号形式（for="[2001:db8::1]:4704"）。
	HeaderForwarded = "Forwarded"

	// HeaderCFConnectingIP Cloudflare 回源时写入/覆盖的客户端 IP 头。
	HeaderCFConnectingIP = "CF-Connecting-IP"

	// HeaderTrueClientIP Akamai 等部分 CDN 使用的客户端 IP 头。
	HeaderTrueClientIP = "True-Client-IP"
)

// trustCIDRs 默认视为可信代理的网段：回环、私网、链路本地与唯一本地地址。
// 这些地址不会出现在公网，通常来自本机代理、内网负载均衡或网关回源；
// 流量若经公网 CDN/WAF/云 LB 回源，请用 WithTrustedProxies 追加其出口网段。
var trustCIDRs = sync.OnceValue(func() []*net.IPNet {
	return mustCIDRs([]string{
		"127.0.0.0/8",    // IPv4 回环
		"::1/128",        // IPv6 回环
		"10.0.0.0/8",     // 私网
		"172.16.0.0/12",  // 私网
		"192.168.0.0/16", // 私网
		"169.254.0.0/16", // IPv4 链路本地
		"fe80::/10",      // IPv6 链路本地
		"fc00::/7",       // IPv6 唯一本地
	})
})

// IPChecker 客户端真实 IP 解析器，面向“部署于可信反向代理之后”的服务。
//
// 判定思路：先看直连对端（RemoteAddr）。
//   - 直连对端不可信（公网直连的真实客户端）→ 代理头一律不可信，直接返回对端；
//   - 直连对端可信（位于可信网段内，如本机/内网网关/LB）→ 才解析代理头取真实客户端。
//
// 代理头解析顺序：
//  1. 厂商头（如 CF-Connecting-IP / True-Client-IP，需经 WithVendorHeaders 显式启用）；
//  2. X-Forwarded-For：从最右往左跳过可信代理，取第一个不可信 IP（防伪造前缀）；
//  3. Forwarded（RFC 7239）：取 for= 参数；
//  4. X-Real-IP；
//  5. 回退 RemoteAddr。
type IPChecker struct {
	trusted []*net.IPNet // 额外可信代理网段
	vendor  []string     // 启用的厂商单值头（存在即优先于 X-Forwarded-For）
}

// IPOption 配置 IPChecker 的可选项。
type IPOption func(*IPChecker)

// WithTrustedProxies 追加可信代理网段（CIDR 或纯 IP）。
// 默认已含回环与私网等内网网段；若流量经公网 CDN/WAF/云 LB 回源，
// 需将其出口网段加入，否则解析会止步于这些代理（拿不到更原始客户端 IP）。
func WithTrustedProxies(cidrs ...string) IPOption {
	return func(c *IPChecker) {
		c.trusted = append(c.trusted, mustCIDRs(cidrs)...)
	}
}

// WithVendorHeaders 启用/自定义厂商客户端 IP 头（如 HeaderCFConnectingIP、
// HeaderTrueClientIP）。这些头在直连可信且存在时优先于 X-Forwarded-For 返回。
// 仅当确认流量确实经过对应厂商（如 Cloudflare/Akamai）时才应启用；
// 传入空参可关闭厂商头。
func WithVendorHeaders(headers ...string) IPOption {
	return func(c *IPChecker) {
		c.vendor = headers
	}
}

// NewIPChecker 创建客户端 IP 解析器，可通过选项定制可信网段与厂商头：
//
//	ipc := NewIPChecker(
//		WithTrustedProxies("100.64.0.0/10"),     // 追加可信网关网段
//		WithVendorHeaders(HeaderCFConnectingIP), // 启用 Cloudflare 头
//	)
//	remote := ipc.ClientIP(r)
//
// 默认（不传选项）等同 NewIPChecker()：回环/私网可信，
// 识别 X-Forwarded-For / Forwarded / X-Real-IP，不启用任何厂商头。
// 解析规则预编译（可信网段解析一次），适合按请求高频复用（如按 IP 限流）。
func NewIPChecker(opts ...IPOption) *IPChecker {
	c := &IPChecker{}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ClientIP 解析请求的真实客户端 IP（纯 IP，不含端口）。规则见 IPChecker 说明。
// r 为 nil 或无法确定时返回空字符串。
func (c *IPChecker) ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	direct := remoteIP(r.RemoteAddr)
	if direct == "" {
		return ""
	}
	// 直连对端不可信：视为真实客户端直连，代理头一律不可信。
	if !c.isTrusted(direct) {
		return direct
	}

	// 1. 厂商头（存在即返回，通常比 XFF 更可信且由厂商覆盖）
	for _, h := range c.vendor {
		if ip := strings.TrimSpace(r.Header.Get(h)); ip != "" {
			return ip
		}
	}
	// 2. X-Forwarded-For：从右往左跳过可信代理，取首个不可信 IP
	if ip := c.xffIP(r); ip != "" {
		return ip
	}
	// 3. Forwarded（RFC 7239）
	if ip := forwardedIP(r.Header.Get(HeaderForwarded)); ip != "" {
		return ip
	}
	// 4. X-Real-IP
	if ip := strings.TrimSpace(r.Header.Get(HeaderXRealIP)); ip != "" {
		return ip
	}
	return direct
}

// defaultIPChecker 包级默认解析器：回环/私网可信，识别 XFF/Forwarded/X-Real-IP，无厂商头。
var defaultIPChecker = NewIPChecker()

// ClientIP 获取请求的真实客户端 IP（纯 IP，不含端口）。
// 使用默认可信规则（回环与私网视为可信代理）与安全解析（见 IPChecker 说明），
// 相比“盲信 X-Forwarded-For 最左值”能抵御伪造前缀。
// 需要自定义可信网段/厂商头时请使用 NewIPChecker 构建解析器。
func ClientIP(r *http.Request) string {
	return defaultIPChecker.ClientIP(r)
}

// ClientIPWithTrustedProxies 在默认可信网段基础上追加自定义可信代理网段后解析客户端 IP。
// 适用于流量经公网 CDN/WAF/云 LB 回源（其出口网段不在默认内网范围）的场景。
// 如需复用解析器（避免每次重复解析网段），请改用 NewIPChecker(WithTrustedProxies(...))。
func ClientIPWithTrustedProxies(r *http.Request, trusted ...string) string {
	return NewIPChecker(WithTrustedProxies(trusted...)).ClientIP(r)
}

// isTrusted 判断 ipStr 是否位于可信代理网段（默认可信网段或额外网段）。
func (c *IPChecker) isTrusted(ipStr string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return false
	}
	for _, n := range trustCIDRs() {
		if n.Contains(ip) {
			return true
		}
	}
	for _, n := range c.trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// xffIP 从 X-Forwarded-For 解析真实客户端：
// 自右向左遍历（离本服务最近的一跳），跳过可信代理，第一个不可信 IP 即客户端；
// 整条链都是可信代理时取最左侧（最原始）值。
func (c *IPChecker) xffIP(r *http.Request) string {
	xff := r.Header.Get(HeaderXForwardedFor)
	if xff == "" {
		return ""
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip == "" {
			continue
		}
		if !c.isTrusted(ip) {
			return ip
		}
	}
	// 整条链都是可信代理（或存在不可解析项）：取最左侧非空值（最原始客户端）
	for _, p := range parts {
		if ip := strings.TrimSpace(p); ip != "" {
			return ip
		}
	}
	return ""
}

// forwardedIP 解析 RFC 7239 Forwarded 头中第一个 for= 参数。
// 支持引号与 IPv6 中括号：for=1.2.3.4 / for="[2001:db8::1]:4704" / for=192.0.2.1:8080；
// for=unknown / obfuscated 等非 IP 值返回空。
func forwardedIP(h string) string {
	for _, part := range strings.Split(h, ";") {
		key, val, ok := strings.Cut(part, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "for") {
			continue
		}
		v := strings.Trim(strings.TrimSpace(val), `"`)
		if v == "" || strings.EqualFold(v, "unknown") || strings.EqualFold(v, "obfuscated") {
			continue
		}
		if strings.HasPrefix(v, "[") {
			if end := strings.IndexByte(v, ']'); end > 0 {
				return v[1:end]
			}
		} else if strings.Contains(v, ":") {
			if host, _, err := net.SplitHostPort(v); err == nil {
				return host
			}
		}
		return v
	}
	return ""
}

// mustCIDRs 将 CIDR/IP 文本列表解析为网段集合；纯 IP 按全掩码（/32、/128）处理。
func mustCIDRs(list []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(list))
	for _, s := range list {
		out = append(out, mustCIDR(s))
	}
	return out
}

// mustCIDR 将单个 CIDR/IP 文本解析为网段；无法解析时返回 0.0.0.0/32（永不匹配），
// 避免配置错误破坏调用方。
func mustCIDR(s string) *net.IPNet {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '/'); i >= 0 {
		_, n, err := net.ParseCIDR(s)
		if err == nil {
			return n
		}
	} else if ip := net.ParseIP(s); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return &net.IPNet{IP: ip4, Mask: net.CIDRMask(32, 32)}
		}
		return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
	}
	return &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(32, 32)}
}

// remoteIP 去掉 addr 的端口部分返回纯 IP；addr 不含端口或无法解析时原样返回。
// 支持 "1.2.3.4:5678"、"2001:db8::1:5678" 等带端口形式及不带端口的裸地址。
func remoteIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
