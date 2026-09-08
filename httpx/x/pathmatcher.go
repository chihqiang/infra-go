// Package x 汇集 httpx 下与中间件本体无关的通用 HTTP 小工具，供本库各模块复用。
//
// 目前包含：
//   - 路径匹配器（PathMatcher）：httpx/middleware 各中间件的 skipPaths / ignorePaths
//     忽略规则统一实现，避免各自重复造轮子；
//   - 客户端 IP 解析（IPChecker / ClientIP）：获取经反向代理转发后的真实客户端 IP。
//
// 命名 x 表示这类零散工具按需收敛于此集合，避免为单一函数各自建包；
// 后续新增同类的通用小工具时放入本包即可。
package x

import (
	"path"
	"strings"
)

// PathMatcher 判断请求路径是否命中一组忽略/跳过规则。
//
// 每条规则 pattern 支持三种形式：
//   - 精确匹配：如 "/health"，仅命中该路径；
//   - 前缀通配：以 "*" 结尾，如 "/health*"，命中以该前缀开头的路径（含跨目录子路径）；
//   - glob 通配：如 "/metrics/*"（* 不跨目录）或 "/api/v?/x"，基于 path.Match 语义。
//
// 注意：以 "*" 结尾的规则会先按前缀匹配（可跨目录），因此 "/metrics/*" 也会命中
// "/metrics/a/b"。若需只匹配一级子路径，应使用不含尾 "*" 的 glob 规则
// （* 不跨目录），例如 "/metrics/?"。
//
// 空字符串规则会被忽略。
type PathMatcher struct {
	patterns []string
}

// NewPathMatcher 根据 patterns 构建路径匹配器，返回 nil 安全（未传入规则时不命中任何路径）。
func NewPathMatcher(patterns []string) *PathMatcher {
	return &PathMatcher{patterns: patterns}
}

// Match 返回 reqPath 是否命中任意一条规则。
func (m *PathMatcher) Match(reqPath string) bool {
	for _, p := range m.patterns {
		if p == "" {
			continue
		}
		if p == reqPath {
			return true
		}
		// 以 * 结尾：前缀匹配（可跨目录），如 /health* 命中 /healthz、/health/live
		if strings.HasSuffix(p, "*") &&
			strings.HasPrefix(reqPath, strings.TrimSuffix(p, "*")) {
			return true
		}
		// glob 匹配（* 不跨 /），如 /metrics/* 命中 /metrics/foo
		if ok, _ := path.Match(p, reqPath); ok {
			return true
		}
	}
	return false
}
