package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// --- 绑定函数 ---

// Bind 根据请求的 Method 和 Content-Type 自动选择绑定器。
// GET 请求使用 Form 绑定（query 参数），其他请求根据 Content-Type 选择。
func Bind(r *http.Request, obj any) error {
	return Default(r.Method, r.Header.Get("Content-Type")).Bind(r, obj)
}

// BindJSON 将请求 body 作为 JSON 绑定到 obj。
func BindJSON(r *http.Request, obj any) error {
	return JSON.Bind(r, obj)
}

// BindXML 将请求 body 作为 XML 绑定到 obj。
func BindXML(r *http.Request, obj any) error {
	return XML.Bind(r, obj)
}

// BindQuery 将 URL query 参数绑定到 obj。
// 使用 `form` 标签匹配字段名。
func BindQuery(r *http.Request, obj any) error {
	return Query.Bind(r, obj)
}

// BindForm 将表单数据（query + post form）绑定到 obj。
// 使用 `form` 标签匹配字段名。
func BindForm(r *http.Request, obj any) error {
	return Form.Bind(r, obj)
}

// BindHeader 将 HTTP header 绑定到 obj。
// 使用 `header` 标签匹配字段名。
func BindHeader(r *http.Request, obj any) error {
	return Header.Bind(r, obj)
}

// BindURI 将 URI 路径参数绑定到 obj。
// params 通常来自路由解析的路径参数，如 {"id": "123"}。
// 使用 `uri` 标签匹配字段名。
func BindURI(params map[string]string, obj any) error {
	m := make(map[string][]string, len(params))
	for k, v := range params {
		m[k] = []string{v}
	}
	return Uri.BindUri(m, obj)
}

// BindURIWithValues 将 map[string][]string 格式的路径参数绑定到 obj。
func BindURIWithValues(params map[string][]string, obj any) error {
	return Uri.BindUri(params, obj)
}

// --- MustBind 系列（绑定 + 自动写入错误响应） ---

// MustBind 绑定并验证请求数据，出错时写入 HTTP 错误响应。
// 成功返回 nil，失败返回错误并自动写入响应。
func MustBind(w http.ResponseWriter, r *http.Request, obj any) error {
	if err := Bind(r, obj); err != nil {
		writeBindError(w, err)
		return err
	}
	return nil
}

// MustBindJSON 绑定 JSON 并验证，出错时写入 HTTP 错误响应。
func MustBindJSON(w http.ResponseWriter, r *http.Request, obj any) error {
	if err := BindJSON(r, obj); err != nil {
		writeBindError(w, err)
		return err
	}
	return nil
}

// MustBindQuery 绑定 Query 参数并验证，出错时写入 HTTP 错误响应。
func MustBindQuery(w http.ResponseWriter, r *http.Request, obj any) error {
	if err := BindQuery(r, obj); err != nil {
		writeBindError(w, err)
		return err
	}
	return nil
}

// MustBindForm 绑定表单并验证，出错时写入 HTTP 错误响应。
func MustBindForm(w http.ResponseWriter, r *http.Request, obj any) error {
	if err := BindForm(r, obj); err != nil {
		writeBindError(w, err)
		return err
	}
	return nil
}

// MustBindURI 绑定路径参数并验证，出错时写入 HTTP 错误响应。
func MustBindURI(w http.ResponseWriter, params map[string]string, obj any) error {
	if err := BindURI(params, obj); err != nil {
		writeBindError(w, err)
		return err
	}
	return nil
}

// --- JSON 解析 ---

// ParseJSON 将请求 body 解析为 JSON 到 obj 中。
func ParseJSON(r *http.Request, obj any) error {
	if r == nil || r.Body == nil {
		return errors.New("invalid request")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(obj); err != nil {
		return fmt.Errorf("failed to decode json: %w", err)
	}
	return Validate(obj)
}

// ParseJSONWithLimit 将请求 body 解析为 JSON，限制最大字节数。
func ParseJSONWithLimit(r *http.Request, obj any, maxBytes int64) error {
	if r == nil || r.Body == nil {
		return errors.New("invalid request")
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(obj); err != nil {
		return fmt.Errorf("failed to decode json: %w", err)
	}
	return Validate(obj)
}

// --- 内部辅助 ---

// writeBindError 根据绑定错误类型写入对应的 HTTP 响应。
func writeBindError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytesErr):
		WriteHTTPError(w, http.StatusRequestEntityTooLarge, err.Error())
	default:
		WriteHTTPError(w, http.StatusBadRequest, err.Error())
	}
}
