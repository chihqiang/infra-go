// Package match 提供轻量的模式匹配器。
//
// 目前提供 HTTP 请求路径匹配器，供 httpx/middleware 各中间件复用同一套
// 路径忽略规则（如 WithLogger / WithCryption 的 skipPaths、WithTracing 的
// ignorePaths），避免各自重复实现。
package match

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
