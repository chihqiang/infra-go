package httpx

import (
	"io/fs"
	"net/http"
	"net/http/pprof"
	"strings"
)

// --- 静态文件路由 ---

// StaticFile 创建一个提供单个静态文件的 Route。
// 自动设置正确的 Content-Type，并支持 Range 请求（断点续传）。
//
//	server.AddRoute(httpx.StaticFile("/favicon.ico", "./static/favicon.ico"))
//
// pattern 为 URL 路径（需以 "/" 开头），filePath 为本地文件路径。
func StaticFile(pattern, filePath string) Route {
	return Route{
		Method: http.MethodGet,
		Path:   pattern,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filePath)
		},
	}
}

// StaticFS 创建一个提供文件系统静态资源的 Route。
// fsys 可以是 os.DirFS、embed.FS 等任意 fs.FS 实现。
//
//	server.AddRoute(httpx.StaticFS("/static/", os.DirFS("./public")))
//	// 访问 /static/app.js 将返回 ./public/app.js
//
// pattern 需以 "/" 结尾，才会匹配该前缀下的所有子路径。
// 也可与 WithPrefix 组合使用，会自动适配加前缀后的 URL。
func StaticFS(pattern string, fsys fs.FS) Route {
	fileServer := http.FileServer(http.FS(fsys))
	return Route{
		Method: http.MethodGet,
		Path:   pattern,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			base := r.Pattern
			if i := strings.IndexByte(base, ' '); i != -1 {
				base = base[i+1:]
			}
			if base == "" {
				base = pattern
			}
			http.StripPrefix(base, fileServer).ServeHTTP(w, r)
		},
	}
}

// --- pprof 性能分析路由 ---

// PprofRoutes 返回标准 pprof 性能分析路由列表，不会自动注册到 Server。
//
// prefix 指定路由前缀，为空时默认使用 /debug/pprof。
// 返回的路由需通过 Server.AddRoutes 手动注册，可附加 RouteOption（如中间件）。
//
//	// 默认前缀 /debug/pprof
//	server.AddRoutes(httpx.PprofRoutes(""))
//
//	// 自定义前缀
//	server.AddRoutes(httpx.PprofRoutes("/admin/pprof"))
//
//	// 带认证中间件（生产环境推荐）
//	server.AddRoutes(httpx.PprofRoutes(""), httpx.WithMiddleware(authMiddleware))
//
// 返回的路由列表（共 11 条）：
//
//   - GET {prefix}/              — 索引页，列出所有可用的 profile
//   - GET {prefix}/cmdline       — 当前进程的命令行参数
//   - GET {prefix}/profile       — CPU 性能分析（通过 seconds 参数指定采样时长）
//   - GET {prefix}/symbol        — 符号表查询
//   - GET {prefix}/trace         — 执行追踪（通过 seconds 参数指定采样时长）
//   - GET {prefix}/allocs        — 所有内存分配样本
//   - GET {prefix}/block         — 阻塞操作堆栈（需先调用 runtime.SetBlockProfileRate）
//   - GET {prefix}/goroutine     — 当前 goroutine 堆栈
//   - GET {prefix}/heap          — 堆内存分配
//   - GET {prefix}/mutex         — 互斥锁竞争（需先调用 runtime.SetMutexProfileFraction）
//   - GET {prefix}/threadcreate  — OS 线程创建
//
// 注意：pprof.Index 处理器内部硬编码了 /debug/pprof/ 路径前缀，
// 使用默认前缀时索引页和子路径完全正常；使用自定义前缀时各 profile 端点仍可正常访问，
// 但索引页内链接仍指向 /debug/pprof/ 路径。
//
// 安全提示：pprof 端点会暴露程序内部信息，生产环境应通过中间件进行访问控制。
func PprofRoutes(prefix string) []Route {
	// 规范化前缀：去除首尾 /，为空时使用默认值，最后确保以 / 开头
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		prefix = "debug/pprof"
	}
	prefix = "/" + prefix
	routes := []Route{
		// 标准处理器
		{Method: http.MethodGet, Path: prefix + "/", Handler: pprof.Index},
		{Method: http.MethodGet, Path: prefix + "/cmdline", Handler: pprof.Cmdline},
		{Method: http.MethodGet, Path: prefix + "/profile", Handler: pprof.Profile},
		{Method: http.MethodGet, Path: prefix + "/symbol", Handler: pprof.Symbol},
		{Method: http.MethodGet, Path: prefix + "/trace", Handler: pprof.Trace},
		// 各 profile 端点（pprof.Handler 返回 http.Handler，取其 ServeHTTP 方法适配）
		{Method: http.MethodGet, Path: prefix + "/allocs", Handler: pprof.Handler("allocs").ServeHTTP},
		{Method: http.MethodGet, Path: prefix + "/block", Handler: pprof.Handler("block").ServeHTTP},
		{Method: http.MethodGet, Path: prefix + "/goroutine", Handler: pprof.Handler("goroutine").ServeHTTP},
		{Method: http.MethodGet, Path: prefix + "/heap", Handler: pprof.Handler("heap").ServeHTTP},
		{Method: http.MethodGet, Path: prefix + "/mutex", Handler: pprof.Handler("mutex").ServeHTTP},
		{Method: http.MethodGet, Path: prefix + "/threadcreate", Handler: pprof.Handler("threadcreate").ServeHTTP},
	}
	return routes
}
