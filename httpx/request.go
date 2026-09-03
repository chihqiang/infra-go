package httpx

import (
	"net/http"

	"github.com/chihqiang/infra-go/cast"
)

// 本文件提供从请求中按 key 读取单个值的泛型便捷函数，
// 覆盖 URL Query、路径参数（PathValue）、Header 三种来源。
//
// 每个来源对应一个泛型函数，按目标类型 T 自动完成类型转换
// （底层复用 cast.ToE，支持 string、int/uint/float 各宽度、bool、time.Duration、time.Time）：
//
//	QueryValue(r, "page", 2)          // int，缺失/非法 → 2
//	QueryValue[string](r, "tag")      // string，缺失 → ""
//	PathValue(r, "id", int64(0))      // 路径参数 {id}
//	HeaderValue(r, "X-Token", "fb")   // 请求头
//
// def 为可选默认值：key 缺失、值为空或转换失败时返回 def；未提供 def 时返回 T 的零值。
//
// 注意：便捷函数适合"少量 key 直接读取"；字段较多、需要校验/默认值时，
// 推荐使用 BindQuery/BindURI/BindHeader 绑定到结构体。
// 请求体（JSON/表单 body）请走 Bind*/MustBind* 绑定，本文件不做 body 单值读取。

// valueOf 将原始字符串按类型 T 转换；为空或转换失败时返回 def。
func valueOf[T any](raw string, def T) T {
	if raw == "" {
		return def
	}
	v, err := cast.ToE[T](raw)
	if err != nil {
		return def
	}
	return v
}

// defValue 取出变参默认值；未提供时返回 T 的零值。
func defValue[T any](def []T) T {
	var zero T
	if len(def) > 0 {
		return def[0]
	}
	return zero
}

// --- URL Query 参数 ---

// QueryValue 从 URL query 中读取 key 并转换为类型 T。
// 例如请求 /users?tag=a，QueryValue[string](r, "tag") 返回 "a"。
func QueryValue[T any](r *http.Request, key string, def ...T) T {
	var raw string
	if r != nil {
		raw = r.URL.Query().Get(key)
	}
	return valueOf(raw, defValue(def))
}

// --- 路径参数 ---

// PathValue 从路径参数中读取 key 并转换为类型 T。
// 需要路由使用 Go 1.22 的 {key} 模式，如 "/users/{id}"。
func PathValue[T any](r *http.Request, key string, def ...T) T {
	var raw string
	if r != nil {
		raw = r.PathValue(key)
	}
	return valueOf(raw, defValue(def))
}

// --- Header 请求头 ---

// HeaderValue 从请求头中读取 key 并转换为类型 T。
// 请求头名不区分大小写，如 HeaderValue(r, "X-Token", "")。
func HeaderValue[T any](r *http.Request, key string, def ...T) T {
	var raw string
	if r != nil {
		raw = r.Header.Get(key)
	}
	return valueOf(raw, defValue(def))
}
