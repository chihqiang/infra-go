package trace

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Attr 是链路追踪属性的别名，对应 attribute.KeyValue。
// 使用本包提供的 AttrString / AttrInt 等函数创建，无需直接导入 otel/attribute。
type Attr = attribute.KeyValue

// SpanOption 是 Span 启动选项的别名，对应 trace.SpanStartOption。
// 使用本包提供的 WithAttributes 等函数创建，无需直接导入 otel/trace。
type SpanOption = trace.SpanStartOption

// --- 属性构造函数 ---

// AttrString 创建一个字符串类型的属性。
func AttrString(key, val string) Attr { return attribute.String(key, val) }

// AttrInt 创建一个 int 类型的属性。
func AttrInt(key string, val int) Attr { return attribute.Int(key, val) }

// AttrInt64 创建一个 int64 类型的属性。
func AttrInt64(key string, val int64) Attr { return attribute.Int64(key, val) }

// AttrBool 创建一个 bool 类型的属性。
func AttrBool(key string, val bool) Attr { return attribute.Bool(key, val) }

// AttrFloat64 创建一个 float64 类型的属性。
func AttrFloat64(key string, val float64) Attr { return attribute.Float64(key, val) }

// AttrStringSlice 创建一个字符串切片类型的属性。
func AttrStringSlice(key string, val []string) Attr { return attribute.StringSlice(key, val) }

// AttrIntSlice 创建一个 int 切片类型的属性。
func AttrIntSlice(key string, val []int) Attr { return attribute.IntSlice(key, val) }

// --- Span 选项函数 ---

// WithAttributes 创建一个携带属性的 Span 启动选项。
// 用法：
//
//	ctx, span := trace.StartSpan(ctx, "op", trace.WithAttributes(
//	    trace.AttrString("user", "alice"),
//	    trace.AttrInt("age", 30),
//	))
func WithAttributes(attrs ...Attr) SpanOption {
	return trace.WithAttributes(attrs...)
}
