package httpx

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
)

// --- 业务码常量 ---

const (
	// CodeOK 成功业务码。
	CodeOK = 0
	// MsgOK 成功业务消息。
	MsgOK = "ok"
	// CodeDefaultError 默认错误业务码。
	CodeDefaultError = -1
)

// --- HTTP 状态码常量 ---

const (
	// CodeBadRequest 请求参数错误。
	CodeBadRequest = 400
	// CodeUnauthorized 未认证。
	CodeUnauthorized = 401
	// CodeForbidden 无权限。
	CodeForbidden = 403
	// CodeNotFound 资源不存在。
	CodeNotFound = 404
	// CodeRequestEntityTooLarge 请求体过大。
	CodeRequestEntityTooLarge = 413
	// CodeInternalError 服务器内部错误。
	CodeInternalError = 500
	// CodeNotImplemented 未实现。
	CodeNotImplemented = 501
	// CodeServiceUnavailable 服务不可用。
	CodeServiceUnavailable = 503
	// CodeTimeout 请求超时。
	CodeTimeout = 504
)

// --- Content-Type 常量 ---

const (
	// ContentTypeJSON JSON 内容类型。
	ContentTypeJSON = "application/json; charset=utf-8"
	// ContentTypeXML XML 内容类型。
	ContentTypeXML = "application/xml; charset=utf-8"
	// ContentTypeHTML HTML 内容类型。
	ContentTypeHTML = "text/html; charset=utf-8"

	xmlVersion  = "1.0"
	xmlEncoding = "UTF-8"
)

// --- 响应结构 ---

// Response 统一响应结构，data 字段使用泛型支持任意类型。
//
// 用法：
//
//	type User struct { Name string `json:"name"` }
//	resp := httpx.Response[User]{
//	    Code: httpx.CodeOK,
//	    Msg:  httpx.MsgOK,
//	    Data: User{Name: "Alice"},
//	}
type Response[T any] struct {
	// Code 业务状态码，0 表示成功。
	Code int `json:"code" xml:"code"`
	// Msg 提示信息。
	Msg string `json:"msg" xml:"msg"`
	// Data 响应数据。
	Data T `json:"data,omitempty" xml:"data,omitempty"`
}

// xmlResponse 带 XML 声明的响应结构。
type xmlResponse[T any] struct {
	XMLName  xml.Name `xml:"xml"`
	Version  string   `xml:"version,attr"`
	Encoding string   `xml:"encoding,attr"`
	Response[T]
}

// --- CodeError ---

// CodeError 携带业务状态码的错误。
// 实现 error 接口，可用于统一错误传递。
type CodeError struct {
	// Code 业务状态码。
	Code int
	// Msg 错误信息。
	Msg string
	// Cause 原始错误。
	Cause error
}

// Error 返回错误信息。
func (e *CodeError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Cause)
	}
	return e.Msg
}

// Unwrap 返回原始错误，支持 errors.Is / errors.As。
func (e *CodeError) Unwrap() error {
	return e.Cause
}

// NewCodeError 创建一个 CodeError。
func NewCodeError(code int, msg string) *CodeError {
	return &CodeError{Code: code, Msg: msg}
}

// NewCodeErrorWithCause 创建一个带原始错误的 CodeError。
func NewCodeErrorWithCause(code int, msg string, cause error) *CodeError {
	return &CodeError{Code: code, Msg: msg, Cause: cause}
}

// --- 智能包装 ---

// wrapResponse 根据传入值的类型自动包装为统一响应。
//
// 类型推断规则：
//   - *CodeError / CodeError → 使用其 Code 和 Msg
//   - error                  → Code = CodeError, Msg = error.Error()
//   - 其他                   → Code = CodeOK, Msg = MsgOK, Data = v
func wrapResponse(v any) Response[any] {
	var resp Response[any]
	switch data := v.(type) {
	case *CodeError:
		resp.Code = data.Code
		resp.Msg = data.Msg
	case CodeError:
		resp.Code = data.Code
		resp.Msg = data.Msg
	case error:
		resp.Code = CodeDefaultError
		resp.Msg = data.Error()
	default:
		resp.Code = CodeOK
		resp.Msg = MsgOK
		resp.Data = v
	}
	return resp
}

// wrapXMLResponse 将 v 包装为带 XML 声明的响应结构。
func wrapXMLResponse(v any) xmlResponse[any] {
	return xmlResponse[any]{
		Version:   xmlVersion,
		Encoding:  xmlEncoding,
		Response:  wrapResponse(v),
	}
}

// --- JSON 响应 ---

// WriteJSON 以 JSON 格式写入 HTTP 响应。
// 这是一个低级函数，不会对 v 做任何包装，直接序列化写入。
func WriteJSON(w http.ResponseWriter, status int, v any) {
	if err := writeJSON(w, status, v); err != nil {
		log.Printf("write json response failed: %v", err)
	}
}

// WriteJSONCtx 同 WriteJSON，带有 context。
func WriteJSONCtx(ctx context.Context, w http.ResponseWriter, status int, v any) {
	WriteJSON(w, status, v)
}

// OkJSON 智能包装 v 并以 JSON 格式写入响应（HTTP 200）。
//
// 如果 v 是 *CodeError、CodeError 或 error，自动设置对应的错误码和消息；
// 否则设置 Code=0, Msg="ok", Data=v。
func OkJSON(w http.ResponseWriter, v any) {
	WriteJSON(w, http.StatusOK, wrapResponse(v))
}

// OkJSONCtx 同 OkJSON，带有 context。
func OkJSONCtx(ctx context.Context, w http.ResponseWriter, v any) {
	OkJSON(w, v)
}

// writeJSON 实际执行 JSON 序列化和写入。
func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// --- XML 响应 ---

// WriteXML 以 XML 格式写入 HTTP 响应。
func WriteXML(w http.ResponseWriter, status int, v any) {
	if err := writeXML(w, status, v); err != nil {
		log.Printf("write xml response failed: %v", err)
	}
}

// WriteXMLCtx 同 WriteXML，带有 context。
func WriteXMLCtx(ctx context.Context, w http.ResponseWriter, status int, v any) {
	WriteXML(w, status, v)
}

// OkXML 智能包装 v 并以 XML 格式写入响应（HTTP 200）。
func OkXML(w http.ResponseWriter, v any) {
	WriteXML(w, http.StatusOK, wrapXMLResponse(v))
}

// OkXMLCtx 同 OkXML，带有 context。
func OkXMLCtx(ctx context.Context, w http.ResponseWriter, v any) {
	OkXML(w, v)
}

// writeXML 实际执行 XML 序列化和写入。
func writeXML(w http.ResponseWriter, status int, v any) error {
	bs, err := xml.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return fmt.Errorf("marshal xml failed, error: %w", err)
	}

	w.Header().Set("Content-Type", ContentTypeXML)
	w.WriteHeader(status)

	if n, err := w.Write(bs); err != nil {
		// http.ErrHandlerTimeout 已由 http.TimeoutHandler 处理，此处忽略。
		if err != http.ErrHandlerTimeout {
			return fmt.Errorf("write response failed, error: %w", err)
		}
	} else if n < len(bs) {
		return fmt.Errorf("actual bytes: %d, written bytes: %d", len(bs), n)
	}

	return nil
}

// --- HTML 响应 ---

// WriteHTML 以 HTML 格式写入 HTTP 响应。
func WriteHTML(w http.ResponseWriter, status int, v string) {
	if err := writeHTML(w, status, v); err != nil {
		log.Printf("write html response failed: %v", err)
	}
}

// WriteHTMLCtx 同 WriteHTML，带有 context。
func WriteHTMLCtx(ctx context.Context, w http.ResponseWriter, status int, v string) {
	WriteHTML(w, status, v)
}

// OkHTML 以 HTML 格式写入响应（HTTP 200）。
func OkHTML(w http.ResponseWriter, v string) {
	WriteHTML(w, http.StatusOK, v)
}

// OkHTMLCtx 同 OkHTML，带有 context。
func OkHTMLCtx(ctx context.Context, w http.ResponseWriter, v string) {
	OkHTML(w, v)
}

// writeHTML 实际执行 HTML 写入。
func writeHTML(w http.ResponseWriter, status int, v string) error {
	w.Header().Set("Content-Type", ContentTypeHTML)
	w.WriteHeader(status)

	bs := []byte(v)
	if n, err := w.Write(bs); err != nil {
		// http.ErrHandlerTimeout 已由 http.TimeoutHandler 处理，此处忽略。
		if err != http.ErrHandlerTimeout {
			return fmt.Errorf("write response failed, error: %w", err)
		}
	} else if n < len(bs) {
		return fmt.Errorf("actual bytes: %d, written bytes: %d", len(bs), n)
	}

	return nil
}

// --- 错误响应辅助 ---

// WriteHTTPError 写入 HTTP 错误响应。
// 同时设置 HTTP 状态码和业务码为 status，用于 HTTP 层面的错误（如 400、404 等）。
func WriteHTTPError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, Response[any]{
		Code: status,
		Msg:  msg,
	})
}
