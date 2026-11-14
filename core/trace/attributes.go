package trace

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Attr is an alias for a tracing attribute, corresponding to attribute.KeyValue.
// Create it with the helpers provided by this package (AttrString / AttrInt, ...),
// so otel/attribute does not have to be imported directly.
type Attr = attribute.KeyValue

// SpanOption is an alias for a span start option, corresponding to trace.SpanStartOption.
// Create it with the helpers provided by this package (WithAttributes, ...), so
// otel/trace does not have to be imported directly.
type SpanOption = trace.SpanStartOption

// --- Attribute constructors ---

// AttrString creates a string attribute.
func AttrString(key, val string) Attr { return attribute.String(key, val) }

// AttrInt creates an int attribute.
func AttrInt(key string, val int) Attr { return attribute.Int(key, val) }

// AttrInt64 creates an int64 attribute.
func AttrInt64(key string, val int64) Attr { return attribute.Int64(key, val) }

// AttrBool creates a bool attribute.
func AttrBool(key string, val bool) Attr { return attribute.Bool(key, val) }

// AttrFloat64 creates a float64 attribute.
func AttrFloat64(key string, val float64) Attr { return attribute.Float64(key, val) }

// AttrStringSlice creates a string-slice attribute.
func AttrStringSlice(key string, val []string) Attr { return attribute.StringSlice(key, val) }

// AttrIntSlice creates an int-slice attribute.
func AttrIntSlice(key string, val []int) Attr { return attribute.IntSlice(key, val) }

// --- Span option functions ---

// WithAttributes creates a span start option carrying attributes.
// Usage:
//
//	ctx, span := trace.StartSpan(ctx, "op", trace.WithAttributes(
//	    trace.AttrString("user", "alice"),
//	    trace.AttrInt("age", 30),
//	))
func WithAttributes(attrs ...Attr) SpanOption {
	return trace.WithAttributes(attrs...)
}
