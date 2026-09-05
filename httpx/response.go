package httpx

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/chihqiang/infra-go/logger"
)

// 本文件实现 HTTP 统一响应层：
//   - Response[T] 统一响应结构、CodeError 业务错误与业务/HTTP 状态码常量
//   - JSON / XML / HTML / SSE 响应输出（Ok*/Write* 系列）
//   - 错误响应（WriteHTTPError*）与重定向（Redirect*）

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
	// ContentTypeSSE Server-Sent Events 内容类型。
	ContentTypeSSE = "text/event-stream; charset=utf-8"

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
	// RequestID 请求 ID（可选），从 context 提取，无则省略。
	RequestID string `json:"request_id,omitempty" xml:"request_id,omitempty"`
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

// NewCodeError 创建携带业务码 code 与消息 msg 的 CodeError。
func NewCodeError(code int, msg string) *CodeError {
	return &CodeError{Code: code, Msg: msg}
}

// NewCodeErrorWithCause 创建带原始错误 cause 的 CodeError，便于 errors.Is/As 追溯。
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
//
// 若 ctx 中含有 request_id，会一并写入响应。
func wrapResponse(ctx context.Context, v any) Response[any] {
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
	if rid := RequestIDFromContext(ctx); rid != "" {
		resp.RequestID = rid
	}
	return resp
}

// wrapXMLResponse 将 v 包装为带 XML 声明的响应结构。
func wrapXMLResponse(ctx context.Context, v any) xmlResponse[any] {
	return xmlResponse[any]{
		Version:  xmlVersion,
		Encoding: xmlEncoding,
		Response: wrapResponse(ctx, v),
	}
}

// --- JSON 响应 ---

// WriteJSON 以 JSON 格式写入 HTTP 响应。
// 这是一个低级函数，不会对 v 做任何包装，直接序列化写入。
func WriteJSON(w http.ResponseWriter, status int, v any) {
	if err := writeJSON(w, status, v); err != nil {
		logger.Error("write json response failed", logger.Err(err))
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
	WriteJSON(w, http.StatusOK, wrapResponse(context.Background(), v))
}

// OkJSONCtx 同 OkJSON，带有 context。
// 若 context 中含有 request_id（通过 ContextWithRequestID / WithRequestID 中间件注入），
// 会将其一并写入响应。
func OkJSONCtx(ctx context.Context, w http.ResponseWriter, v any) {
	WriteJSON(w, http.StatusOK, wrapResponse(ctx, v))
}

// writeJSON 实际执行 JSON 序列化和写入。
func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// --- XML 响应 ---

// WriteXML 以 XML 格式写入 HTTP 响应。
// 与 WriteJSON 一样是低级函数：不会对 v 做任何包装，直接序列化写入；
// 需要智能包装（统一 code/msg/data）时使用 OkXML。
func WriteXML(w http.ResponseWriter, status int, v any) {
	if err := writeXML(w, status, v); err != nil {
		logger.Error("write xml response failed", logger.Err(err))
	}
}

// WriteXMLCtx 同 WriteXML，带有 context。
func WriteXMLCtx(ctx context.Context, w http.ResponseWriter, status int, v any) {
	WriteXML(w, status, v)
}

// OkXML 智能包装 v 并以 XML 格式写入响应（HTTP 200）。
// 包装规则同 OkJSON：若 v 是 *CodeError、CodeError 或 error，自动使用其错误码与消息；
// 否则 Code=0, Msg="ok", Data=v；并带上 XML 声明。
func OkXML(w http.ResponseWriter, v any) {
	WriteXML(w, http.StatusOK, wrapXMLResponse(context.Background(), v))
}

// OkXMLCtx 同 OkXML，带有 context。
// 若 context 中含有 request_id，会将其一并写入响应。
func OkXMLCtx(ctx context.Context, w http.ResponseWriter, v any) {
	WriteXML(w, http.StatusOK, wrapXMLResponse(ctx, v))
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
// v 为原始 HTML 字符串，不做转义或包装，按文本原样输出。
func WriteHTML(w http.ResponseWriter, status int, v string) {
	if err := writeHTML(w, status, v); err != nil {
		logger.Error("write html response failed", logger.Err(err))
	}
}

// WriteHTMLCtx 同 WriteHTML，带有 context。
func WriteHTMLCtx(ctx context.Context, w http.ResponseWriter, status int, v string) {
	WriteHTML(w, status, v)
}

// OkHTML 以 HTML 格式写入响应（HTTP 200），内容为 v 原样输出。
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

// --- SSE (Server-Sent Events) 响应 ---

// SSEWriter 用于向客户端推送 Server-Sent Events（SSE）流。
//
// 创建后即可连续写入事件帧，每次写入会自动 Flush，保证事件实时到达客户端；
// 当事件方法返回 error（通常是客户端已断开连接）时应停止推送并退出 Handler。
//
// 用法：
//
//	func StreamHandler(w http.ResponseWriter, r *http.Request) {
//	    sse := httpx.NewSSEWriter(w)
//	    for i := 0; i < 10; i++ {
//	        if err := sse.JSONEvent("ping", fmt.Sprintf("tick %d", i)); err != nil {
//	            return // 客户端已断开
//	        }
//	    }
//	}
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter 创建 SSE 写入器，自动设置 SSE 所需响应头：
// Content-Type: text/event-stream、Cache-Control: no-cache、Connection: keep-alive，
// 并关闭反向代理缓冲（X-Accel-Buffering: no），避免事件被 nginx 等缓冲延迟推送。
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	h := w.Header()
	h.Set("Content-Type", ContentTypeSSE)
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")

	s := &SSEWriter{w: w}
	if f, ok := w.(http.Flusher); ok {
		s.flusher = f
	}
	return s
}

// Event 写入一个指定类型的事件帧。
// event 为空时表示默认的 message 事件；data 为多行文本时按 SSE 规范拆成多行 data: 字段。
func (s *SSEWriter) Event(event, data string) error {
	var b strings.Builder
	if event != "" {
		s.writeField(&b, "event", event)
	}
	s.writeField(&b, "data", data)
	b.WriteByte('\n')
	return s.write(b.String())
}

// Data 写入一个默认事件类型（message）的数据帧。
func (s *SSEWriter) Data(data string) error {
	return s.Event("", data)
}

// JSONEvent 将 v 序列化为 JSON 后作为事件数据写入。
// 客户端可使用 JSON.parse 解析收到的 data 字段。
func (s *SSEWriter) JSONEvent(event string, v any) error {
	bs, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal sse event data failed, error: %w", err)
	}
	return s.Event(event, string(bs))
}

// Comment 写入一条注释帧（以冒号开头的行），客户端会忽略其内容，常用于心跳保活。
func (s *SSEWriter) Comment(text string) error {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		b.WriteByte(':')
		if line != "" {
			b.WriteByte(' ')
			b.WriteString(line)
		}
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	return s.write(b.String())
}

// Retry 告知浏览器断线后的重连间隔（毫秒）。
func (s *SSEWriter) Retry(ms int) error {
	var b strings.Builder
	b.WriteString("retry: ")
	b.WriteString(strconv.Itoa(ms))
	b.WriteString("\n\n")
	return s.write(b.String())
}

// writeField 按 SSE 规范把字段的多行值拆成多行 "field: value" 写入。
func (s *SSEWriter) writeField(b *strings.Builder, field, value string) {
	for _, line := range strings.Split(value, "\n") {
		b.WriteString(field)
		b.WriteString(": ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

// write 写入数据后立即 Flush，确保事件实时推送到客户端。
func (s *SSEWriter) write(payload string) error {
	if _, err := s.w.Write([]byte(payload)); err != nil {
		return err
	}
	s.Flush()
	return nil
}

// Flush 将缓冲的数据立即推送给客户端。
// 若底层 ResponseWriter 未实现 http.Flusher，则为空操作。
func (s *SSEWriter) Flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// --- 错误响应辅助 ---

// WriteHTTPError 写入 HTTP 错误响应。
// 同时设置 HTTP 状态码和业务码为 status，用于 HTTP 层面的错误（如 400、404 等）。
// 等价于 WriteHTTPErrorWithCode(w, status, status, msg)。
func WriteHTTPError(w http.ResponseWriter, status int, msg string) {
	WriteHTTPErrorWithCode(w, status, status, msg)
}

// WriteHTTPErrorCtx 同 WriteHTTPError，带有 context。
// 若 context 中含有 request_id，会将其一并写入响应。
// 等价于 WriteHTTPErrorWithCodeCtx(ctx, w, status, status, msg)。
func WriteHTTPErrorCtx(ctx context.Context, w http.ResponseWriter, status int, msg string) {
	WriteHTTPErrorWithCodeCtx(ctx, w, status, status, msg)
}

// WriteHTTPErrorWithCode 写入 HTTP 错误响应，支持分离 HTTP 状态码与业务码。
//
// 在 RESTful API 中，HTTP 状态码反映传输层状态（如 400 Bad Request），
// 而业务码反映业务语义（如 10001 表示"用户名已存在"）。
// 此函数允许二者独立设置，适用于需要细粒度业务错误码的场景。
//
// 用法：
//
//	// HTTP 400，业务码 10001
//	httpx.WriteHTTPErrorWithCode(w, http.StatusBadRequest, 10001, "username already exists")
func WriteHTTPErrorWithCode(w http.ResponseWriter, status int, code int, msg string) {
	WriteJSON(w, status, Response[any]{
		Code: code,
		Msg:  msg,
	})
}

// WriteHTTPErrorWithCodeCtx 同 WriteHTTPErrorWithCode，带有 context。
// 若 context 中含有 request_id，会将其一并写入响应。
func WriteHTTPErrorWithCodeCtx(ctx context.Context, w http.ResponseWriter, status int, code int, msg string) {
	WriteJSON(w, status, Response[any]{
		Code:      code,
		Msg:       msg,
		RequestID: RequestIDFromContext(ctx),
	})
}

// --- 重定向 ---

// Redirect 以指定状态码重定向到 url。
// 会设置 Location 响应头，并触发浏览器跳转。
func Redirect(w http.ResponseWriter, r *http.Request, url string, status int) {
	http.Redirect(w, r, url, status)
}

// RedirectCtx 同 Redirect，带有 context。
func RedirectCtx(ctx context.Context, w http.ResponseWriter, r *http.Request, url string, status int) {
	Redirect(w, r, url, status)
}

// RedirectTemporary 临时重定向（HTTP 302 Found）。
func RedirectTemporary(w http.ResponseWriter, r *http.Request, url string) {
	Redirect(w, r, url, http.StatusFound)
}

// RedirectTemporaryCtx 同 RedirectTemporary，带有 context。
func RedirectTemporaryCtx(ctx context.Context, w http.ResponseWriter, r *http.Request, url string) {
	Redirect(w, r, url, http.StatusFound)
}

// RedirectPermanent 永久重定向（HTTP 301 Moved Permanently）。
func RedirectPermanent(w http.ResponseWriter, r *http.Request, url string) {
	Redirect(w, r, url, http.StatusMovedPermanently)
}

// RedirectPermanentCtx 同 RedirectPermanent，带有 context。
func RedirectPermanentCtx(ctx context.Context, w http.ResponseWriter, r *http.Request, url string) {
	Redirect(w, r, url, http.StatusMovedPermanently)
}
