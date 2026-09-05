package httpx

import (
	"errors"
	"net/http"

	"github.com/chihqiang/infra-go/cast"
	"github.com/chihqiang/infra-go/httpx/binding"
)

// 本文件汇集 HTTP 请求侧的便捷 API：
//   - 结构体绑定：Bind*/MustBind*（将请求数据映射到结构体并校验）
//   - 单值读取：QueryValue/PathValue/HeaderValue（按 key 读取并转换类型）

// --- 绑定函数 ---

// Bind 根据请求的 Method 和 Content-Type 自动选择绑定器。
// GET 请求使用 Form 绑定（query 参数），其他请求根据 Content-Type 选择。
func Bind(r *http.Request, obj any) error {
	return binding.Default(r.Method, r.Header.Get("Content-Type")).Bind(r, obj)
}

// BindJSON 将请求 body 作为 JSON 绑定到 obj。
func BindJSON(r *http.Request, obj any) error {
	return binding.JSON.Bind(r, obj)
}

// BindXML 将请求 body 作为 XML 绑定到 obj。
func BindXML(r *http.Request, obj any) error {
	return binding.XML.Bind(r, obj)
}

// BindQuery 将 URL query 参数绑定到 obj。
// 使用 `form` 标签匹配字段名。
func BindQuery(r *http.Request, obj any) error {
	return binding.Query.Bind(r, obj)
}

// BindForm 将表单数据（query + post form）绑定到 obj。
// 使用 `form` 标签匹配字段名。
func BindForm(r *http.Request, obj any) error {
	return binding.Form.Bind(r, obj)
}

// BindHeader 将 HTTP header 绑定到 obj。
// 使用 `header` 标签匹配字段名。
func BindHeader(r *http.Request, obj any) error {
	return binding.Header.Bind(r, obj)
}

// BindURI 将 URI 路径参数绑定到 obj。
// params 通常来自路由解析的路径参数，如 {"id": "123"}。
// 使用 `uri` 标签匹配字段名。
func BindURI(params map[string]string, obj any) error {
	m := make(map[string][]string, len(params))
	for k, v := range params {
		m[k] = []string{v}
	}
	return binding.Uri.BindUri(m, obj)
}

// BindURIWithValues 将 map[string][]string 格式的路径参数绑定到 obj。
func BindURIWithValues(params map[string][]string, obj any) error {
	return binding.Uri.BindUri(params, obj)
}

// --- MustBind 系列（绑定 + 自动写入错误响应） ---

// MustBind 绑定并验证请求数据，出错时写入 HTTP 错误响应。
// 成功返回 nil，失败返回错误并自动写入响应。
func MustBind(w http.ResponseWriter, r *http.Request, obj any) error {
	if err := Bind(r, obj); err != nil {
		writeBindError(w, r, err)
		return err
	}
	return nil
}

// MustBindJSON 绑定 JSON 并验证，出错时写入 HTTP 错误响应。
func MustBindJSON(w http.ResponseWriter, r *http.Request, obj any) error {
	if err := BindJSON(r, obj); err != nil {
		writeBindError(w, r, err)
		return err
	}
	return nil
}

// MustBindQuery 绑定 Query 参数并验证，出错时写入 HTTP 错误响应。
func MustBindQuery(w http.ResponseWriter, r *http.Request, obj any) error {
	if err := BindQuery(r, obj); err != nil {
		writeBindError(w, r, err)
		return err
	}
	return nil
}

// MustBindForm 绑定表单并验证，出错时写入 HTTP 错误响应。
func MustBindForm(w http.ResponseWriter, r *http.Request, obj any) error {
	if err := BindForm(r, obj); err != nil {
		writeBindError(w, r, err)
		return err
	}
	return nil
}

// --- 内部辅助 ---

// writeBindError 根据绑定错误类型写入对应的 HTTP 响应。
// 使用 WriteHTTPErrorCtx，使响应携带请求上下文中的 request_id。
func writeBindError(w http.ResponseWriter, r *http.Request, err error) {
	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytesErr):
		WriteHTTPErrorCtx(r.Context(), w, http.StatusRequestEntityTooLarge, err.Error())
	default:
		WriteHTTPErrorCtx(r.Context(), w, http.StatusBadRequest, err.Error())
	}
}

// --- 单值读取便捷函数 ---
//
// 从请求中按 key 读取单个值的泛型便捷函数，适合“少量 key 直接读取”的场景；
// 字段较多、需要校验/默认值时，请改用上方的 Bind* / MustBind* 绑定到结构体。

// valueOf 将原始字符串按类型 T 转换，底层复用 cast.ToE，支持 string、
// 各宽度 int/uint/float、bool、time.Duration、time.Time；
// raw 为空或转换失败时返回 def。
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

// defValue 从可选默认值变参中取出第一个；未提供 def 时返回类型 T 的零值。
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
// key 缺失、值为空或转换失败时返回 def；未提供 def 时返回 T 的零值。
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
// key 缺失、值为空或转换失败时返回 def；未提供 def 时返回 T 的零值。
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
// key 缺失、值为空或转换失败时返回 def；未提供 def 时返回 T 的零值。
func HeaderValue[T any](r *http.Request, key string, def ...T) T {
	var raw string
	if r != nil {
		raw = r.Header.Get(key)
	}
	return valueOf(raw, defValue(def))
}
