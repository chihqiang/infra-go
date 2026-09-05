// Package binding 提供 HTTP 请求数据绑定能力：
//   - 绑定器接口（Binding/BindingBody/BindingUri）与内置实例（JSON/XML/Form/Query/Header/Uri）
//   - 常见 Content-Type 的 MIME 常量与 Default 绑定器选择
//   - 表单/Query/Header/URI → 结构体的反射映射引擎与字段元信息缓存（mapping.go / meta.go / value.go）
//   - 基于 go-playground/validator 的结构体校验（validator.go）
//
// httpx 主包的便捷绑定函数（Bind*/MustBind*，见 httpx/request.go）直接调用本包；
// 绑定器实例、MIME 常量、Default 选择与校验入口均统一由本包提供。
package binding

import (
	"mime"
	"net/http"
	"strings"
)

// --- MIME 类型常量 ---

// 常见的 Content-Type MIME 类型。
const (
	MIMEJSON              = "application/json"
	MIMEXML               = "application/xml"
	MIMEXML2              = "text/xml"
	MIMEPlain             = "text/plain"
	MIMEPOSTForm          = "application/x-www-form-urlencoded"
	MIMEMultipartPOSTForm = "multipart/form-data"
)

// --- 绑定器接口 ---

// Binding 描述将请求数据绑定到结构体的接口。
// 不同数据来源（JSON body、Query 参数、Form 表单等）实现此接口。
type Binding interface {
	// Name 返回绑定器名称。
	Name() string
	// Bind 将请求数据绑定到 obj 结构体。
	Bind(*http.Request, any) error
}

// BindingBody 扩展 Binding 接口，支持从原始字节绑定。
// 用于 JSON、XML 等基于 body 的绑定器。
type BindingBody interface {
	Binding
	// BindBody 从字节数组绑定到 obj 结构体。
	BindBody([]byte, any) error
}

// BindingUri 描述从 URI 路径参数绑定的接口，用于路由形参（如 /users/{id}）。
// 通过 httpx.BindURI / BindURIWithValues 使用；路径参数以 map 形式传入。
type BindingUri interface {
	Name() string
	// BindUri 从路径参数 map 绑定到 obj 结构体。
	BindUri(map[string][]string, any) error
}

// --- 内置绑定器实例 ---

var (
	// JSON 基于 JSON body 的绑定器。
	JSON BindingBody = JSONBinding{}
	// XML 基于 XML body 的绑定器。
	XML BindingBody = XMLBinding{}
	// Form 基于 Form 表单的绑定器（包含 query 和 post form）。
	Form Binding = FormBinding{}
	// Query 基于 URL query 参数的绑定器。
	Query Binding = QueryBinding{}
	// Header 基于 HTTP header 的绑定器。
	Header Binding = HeaderBinding{}
	// Uri 基于 URI 路径参数的绑定器。
	Uri BindingUri = URIBinding{}
)

// Default 根据请求方法和 Content-Type 返回合适的绑定器。
// GET 请求固定返回 Form（绑定 query）；其余请求按 Content-Type 匹配：
// JSON → JSON，XML → XML，form/multipart → Form，无法解析或未知类型 → Form。
func Default(method, contentType string) Binding {
	if method == http.MethodGet {
		return Form
	}

	// 解析 Content-Type，去除参数（如 ; charset=utf-8）并忽略大小写，
	// 避免 "application/json; charset=utf-8" 等常见格式匹配失败。
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return Form
	}
	mediaType = strings.ToLower(mediaType)

	switch mediaType {
	case MIMEJSON:
		return JSON
	case MIMEXML, MIMEXML2:
		return XML
	case MIMEMultipartPOSTForm:
		return Form
	default: // case MIMEPOSTForm:
		return Form
	}
}
