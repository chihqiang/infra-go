package logger

import (
	"context"
	"time"
)

// FieldType identifies the value carried by a Field.
type FieldType uint8

const (
	// StringType is a string field.
	StringType FieldType = iota
	// Int64Type is an integer field.
	Int64Type
	// Float64Type is a floating-point field.
	Float64Type
	// BoolType is a boolean field.
	BoolType
	// DurationType is a time.Duration field, stored in nanoseconds.
	DurationType
	// TimeType is a time.Time field.
	TimeType
	// ErrorType is an error field.
	ErrorType
	// AnyType is a field holding an arbitrary value.
	AnyType
)

// Field is a log field, used to pass key-value pairs into structured logging.
type Field struct {
	Key       string
	Type      FieldType
	String    string
	Integer   int64
	Interface any
}

// --- Field constructors ---

// String creates a log field of type string.
func String(key, val string) Field {
	return Field{Key: key, Type: StringType, String: val}
}

// Int creates a log field of type int.
func Int(key string, val int) Field {
	return Field{Key: key, Type: Int64Type, Integer: int64(val)}
}

// Int64 creates a log field of type int64.
func Int64(key string, val int64) Field {
	return Field{Key: key, Type: Int64Type, Integer: val}
}

// Float64 creates a log field of type float64.
func Float64(key string, val float64) Field {
	return Field{Key: key, Type: Float64Type, Interface: val}
}

// Bool creates a log field of type bool.
func Bool(key string, val bool) Field {
	integer := int64(0)
	if val {
		integer = 1
	}
	return Field{Key: key, Type: BoolType, Integer: integer}
}

// Duration creates a log field of type time.Duration.
func Duration(key string, val time.Duration) Field {
	return Field{Key: key, Type: DurationType, Integer: int64(val)}
}

// Time creates a log field of type time.Time.
func Time(key string, val time.Time) Field {
	return Field{Key: key, Type: TimeType, Interface: val}
}

// Any creates a log field holding a value of any type.
func Any(key string, val any) Field {
	return Field{Key: key, Type: AnyType, Interface: val}
}

// Err creates a log field of type error, with the fixed key name "error".
func Err(err error) Field {
	return Field{Key: "error", Type: ErrorType, Interface: err}
}

// Level is the log level type.
type Level int8

// Supported log levels.
const (
	DebugLevel  Level = -1
	InfoLevel   Level = 0
	WarnLevel   Level = 1
	ErrorLevel  Level = 2
	DPanicLevel Level = 3
	PanicLevel  Level = 4
	FatalLevel  Level = 5
)

// String returns the upper-case name of the level.
func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "DEBUG"
	case InfoLevel:
		return "INFO"
	case WarnLevel:
		return "WARN"
	case ErrorLevel:
		return "ERROR"
	case DPanicLevel:
		return "DPANIC"
	case PanicLevel:
		return "PANIC"
	case FatalLevel:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// ILogger defines the core logger interface.
// Every implementation must provide the basic log methods and level checking.
type ILogger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Panic(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)

	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Panicf(format string, args ...any)
	Fatalf(format string, args ...any)

	DebugCtx(ctx context.Context, msg string, fields ...Field)
	InfoCtx(ctx context.Context, msg string, fields ...Field)
	WarnCtx(ctx context.Context, msg string, fields ...Field)
	ErrorCtx(ctx context.Context, msg string, fields ...Field)
	PanicCtx(ctx context.Context, msg string, fields ...Field)
	FatalCtx(ctx context.Context, msg string, fields ...Field)

	DebugfCtx(ctx context.Context, format string, args ...any)
	InfofCtx(ctx context.Context, format string, args ...any)
	WarnfCtx(ctx context.Context, format string, args ...any)
	ErrorfCtx(ctx context.Context, format string, args ...any)
	PanicfCtx(ctx context.Context, format string, args ...any)
	FatalfCtx(ctx context.Context, format string, args ...any)

	Sync() error
}
