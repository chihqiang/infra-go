package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// entry is one log record. Encoders format it; sinks write it.
type entry struct {
	level  Level
	now    time.Time
	caller string
	app    string
	msg    string
	fields []Field
	stack  string
}

// encoder renders one entry into buf.
type encoder interface {
	encodeInto(buf *bytes.Buffer, e entry)
}

// newEncoder picks the encoder for Config.Encoding.
func newEncoder(encoding, timeFormat string) encoder {
	if encoding == ConsoleEncoding {
		return plainEncoder{timeFormat: timeFormat}
	}
	return jsonEncoder{timeFormat: timeFormat}
}

// --- JSON ---

type jsonEncoder struct {
	timeFormat string
}

func (e jsonEncoder) encodeInto(buf *bytes.Buffer, entry entry) {
	buf.WriteByte('{')
	first := true
	writePair := func(key, value string) {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		e.writeString(buf, key)
		buf.WriteByte(':')
		e.writeString(buf, value)
	}

	writePair("level", entry.level.String())
	writePair("time", entry.now.Format(e.timeFormat))
	if entry.caller != "" {
		writePair("caller", entry.caller)
	}
	if entry.app != "" {
		writePair("app", entry.app)
	}
	writePair("msg", entry.msg)
	for _, field := range entry.fields {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		e.writeString(buf, field.Key)
		buf.WriteByte(':')
		e.writeField(buf, field)
	}
	if entry.stack != "" {
		writePair("stacktrace", entry.stack)
	}
	buf.WriteString("}\n")
}

// writeField appends one structured field as "key":value.
func (e jsonEncoder) writeField(buf *bytes.Buffer, field Field) {
	switch field.Type {
	case StringType:
		e.writeString(buf, field.String)
	case Int64Type:
		buf.WriteString(strconv.FormatInt(field.Integer, 10))
	case Float64Type:
		e.writeValue(buf, field.Interface)
	case BoolType:
		if field.Integer != 0 {
			buf.WriteString("true")
			return
		}
		buf.WriteString("false")
	case DurationType:
		millis := int64(time.Duration(field.Integer) / time.Millisecond)
		buf.WriteString(strconv.FormatInt(millis, 10))
	case TimeType:
		if t, ok := field.Interface.(time.Time); ok {
			e.writeString(buf, t.Format(e.timeFormat))
			return
		}
		e.writeValue(buf, field.Interface)
	case ErrorType:
		if field.Interface == nil {
			buf.WriteString("null")
			return
		}
		if err, ok := field.Interface.(error); ok {
			e.writeString(buf, err.Error())
			return
		}
		e.writeValue(buf, field.Interface)
	default:
		e.writeValue(buf, field.Interface)
	}
}

// writeString appends s as a JSON string. Using json.Marshal keeps the output
// identical to encoding/json, including HTML escaping and invalid-UTF-8
// replacement.
func (e jsonEncoder) writeString(buf *bytes.Buffer, s string) {
	data, err := json.Marshal(s)
	if err != nil {
		buf.WriteString(`""`)
		return
	}
	buf.Write(data)
}

// writeValue appends any value as JSON, falling back to its string form when
// marshalling fails.
func (e jsonEncoder) writeValue(buf *bytes.Buffer, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		e.writeString(buf, fmt.Sprint(v))
		return
	}
	buf.Write(data)
}

// --- plain (console) ---

type plainEncoder struct {
	timeFormat string
}

func (e plainEncoder) encodeInto(buf *bytes.Buffer, entry entry) {
	buf.WriteString(entry.now.Format(e.timeFormat))
	buf.WriteByte(' ')
	buf.WriteString(entry.level.String())
	if entry.caller != "" {
		buf.WriteByte(' ')
		buf.WriteString(entry.caller)
	}
	if entry.app != "" {
		buf.WriteString(" app=")
		buf.WriteString(entry.app)
	}
	buf.WriteByte(' ')
	buf.WriteString(entry.msg)
	for _, field := range entry.fields {
		buf.WriteByte(' ')
		buf.WriteString(field.Key)
		buf.WriteByte('=')
		buf.WriteString(e.fieldText(field))
	}
	if entry.stack != "" {
		buf.WriteString(" stacktrace=")
		buf.WriteString(entry.stack)
	}
	buf.WriteByte('\n')
}

func (e plainEncoder) fieldText(field Field) string {
	switch field.Type {
	case StringType:
		return field.String
	case Int64Type:
		return strconv.FormatInt(field.Integer, 10)
	case Float64Type:
		return fmt.Sprint(field.Interface)
	case BoolType:
		return strconv.FormatBool(field.Integer != 0)
	case DurationType:
		return time.Duration(field.Integer).String()
	case TimeType:
		if t, ok := field.Interface.(time.Time); ok {
			return t.Format(e.timeFormat)
		}
		return fmt.Sprint(field.Interface)
	case ErrorType:
		if field.Interface == nil {
			return "<nil>"
		}
		if err, ok := field.Interface.(error); ok {
			return err.Error()
		}
		return fmt.Sprint(field.Interface)
	default:
		return fmt.Sprint(field.Interface)
	}
}
