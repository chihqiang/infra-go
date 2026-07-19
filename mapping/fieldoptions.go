package mapping

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

const (
	optionDefault  = "default"
	optionEnv      = "env"
	optionOptional = "optional"
	optionOptions  = "options"
	optionRange    = "range"
	optionString   = "string"
	optionInherit  = "inherit"

	optionSeparator = "|"
	equalToken      = "="
	escapeChar      = '\\'

	leftBracket        = '('
	rightBracket       = ')'
	leftSquareBracket  = '['
	rightSquareBracket = ']'
	segmentSeparator   = ','
)

var (
	errNumberRange = fmt.Errorf("invalid number range setting")
)

// fieldOptions 存储从结构体标签中解析出的字段选项。
type fieldOptions struct {
	// Default 字段的默认值。
	Default string
	// EnvVar 环境变量名，如果设置了，优先从环境变量读取。
	EnvVar string
	// Optional 字段是否可选。
	Optional bool
	// OptionalDep 可选依赖，用于条件可选。
	// 例如 optional=!other 表示当 other 未设置时此字段可选。
	OptionalDep string
	// Options 允许的值列表。
	Options []string
	// Range 数值范围。
	Range *numberRange
	// FromString 是否从字符串解析值。
	FromString bool
	// Inherit 是否从父级继承值。
	Inherit bool
}

// numberRange 表示一个数值范围。
type numberRange struct {
	left         float64
	leftInclude  bool
	right        float64
	rightInclude bool
}

// hasDefault 返回是否设置了默认值。
func (o *fieldOptions) hasDefault() (string, bool) {
	if o == nil {
		return "", false
	}
	return o.Default, len(o.Default) > 0
}

// isOptional 返回是否可选。
func (o *fieldOptions) isOptional() bool {
	return o != nil && o.Optional
}

// allowedOptions 返回允许的值列表。
func (o *fieldOptions) allowedOptions() []string {
	if o == nil {
		return nil
	}
	return o.Options
}

// isFromString 返回是否从字符串解析。
func (o *fieldOptions) isFromString() bool {
	return o != nil && o.FromString
}

// isInRange 检查数值是否在范围内。
func (o *fieldOptions) isInRange(v float64) bool {
	if o == nil || o.Range == nil {
		return true
	}
	nr := o.Range
	if nr.leftInclude && v < nr.left {
		return false
	}
	if !nr.leftInclude && v <= nr.left {
		return false
	}
	if nr.rightInclude && v > nr.right {
		return false
	}
	if !nr.rightInclude && v >= nr.right {
		return false
	}
	return true
}

// fieldOptionsCacheValue 缓存已解析的标签选项，避免重复解析。
type fieldOptionsCacheValue struct {
	key     string
	options *fieldOptions
	err     error
}

var (
	optionsCache     = make(map[string]fieldOptionsCacheValue)
	optionsCacheLock sync.RWMutex
)

// parseKeyAndOptions 从结构体字段的标签中解析键名和选项。
// tagName 是标签名，通常是 "json"。
// 返回解析出的键名（如果标签为空则返回字段名）、选项和错误。
func parseKeyAndOptions(tagName string, field reflect.StructField) (string, *fieldOptions, error) {
	value := strings.TrimSpace(field.Tag.Get(tagName))
	if len(value) == 0 {
		return field.Name, nil, nil
	}

	optionsCacheLock.RLock()
	cached, ok := optionsCache[value]
	optionsCacheLock.RUnlock()
	if ok {
		if len(cached.key) > 0 {
			return cached.key, cached.options, cached.err
		}
		return field.Name, cached.options, cached.err
	}

	key, opts, err := doParseKeyAndOptions(field.Name, value)

	optionsCacheLock.Lock()
	optionsCache[value] = fieldOptionsCacheValue{
		key:     key,
		options: opts,
		err:     err,
	}
	optionsCacheLock.Unlock()

	if len(key) > 0 {
		return key, opts, err
	}
	return field.Name, opts, err
}

// doParseKeyAndOptions 实际解析标签值。
func doParseKeyAndOptions(fieldName, value string) (string, *fieldOptions, error) {
	segments := parseSegments(value)
	key := strings.TrimSpace(segments[0])
	options := segments[1:]

	if len(options) == 0 {
		return key, nil, nil
	}

	var opts fieldOptions
	for _, segment := range options {
		option := strings.TrimSpace(segment)
		if err := parseOption(&opts, fieldName, option); err != nil {
			return "", nil, err
		}
	}

	return key, &opts, nil
}

// parseOption 解析单个选项。
func parseOption(opts *fieldOptions, fieldName, option string) error {
	switch {
	case option == optionInherit:
		opts.Inherit = true
	case option == optionString:
		opts.FromString = true
	case strings.HasPrefix(option, optionOptional):
		segs := strings.Split(option, equalToken)
		switch len(segs) {
		case 1:
			opts.Optional = true
		case 2:
			opts.Optional = true
			opts.OptionalDep = segs[1]
		default:
			return fmt.Errorf("invalid optional option for field %q", fieldName)
		}
	case strings.HasPrefix(option, optionOptions):
		val, err := parseProperty(fieldName, optionOptions, option)
		if err != nil {
			return err
		}
		opts.Options = parseOptionsValue(val)
	case strings.HasPrefix(option, optionDefault):
		val, err := parseProperty(fieldName, optionDefault, option)
		if err != nil {
			return err
		}
		opts.Default = val
	case strings.HasPrefix(option, optionEnv):
		val, err := parseProperty(fieldName, optionEnv, option)
		if err != nil {
			return err
		}
		opts.EnvVar = val
	case strings.HasPrefix(option, optionRange):
		val, err := parseProperty(fieldName, optionRange, option)
		if err != nil {
			return err
		}
		nr, err := parseNumberRange(val)
		if err != nil {
			return err
		}
		opts.Range = nr
	}
	return nil
}

// parseProperty 解析 key=value 格式的选项。
func parseProperty(field, tag, val string) (string, error) {
	segs := strings.Split(val, equalToken)
	if len(segs) != 2 {
		return "", fmt.Errorf("invalid %q option for field %q", tag, field)
	}
	return strings.TrimSpace(segs[1]), nil
}

// parseOptionsValue 解析允许值列表。
// 支持两种格式: [a,b,c] 或 a|b|c
func parseOptionsValue(val string) []string {
	if len(val) == 0 {
		return nil
	}
	if val[0] == leftSquareBracket {
		return parseGroupedSegments(val)
	}
	return strings.Split(val, optionSeparator)
}

// parseNumberRange 解析数值范围。
// 支持以下格式:
//
//	[:5]  (:5]  [:5)  (:5)    — 只有上界
//	[1:]  [1:)  (1:]  (1:)    — 只有下界
//	[1:5] [1:5) (1:5] (1:5)   — 上下界都有
func parseNumberRange(str string) (*numberRange, error) {
	if len(str) == 0 {
		return nil, errNumberRange
	}

	leftInclude, err := isLeftInclude(str[0])
	if err != nil {
		return nil, err
	}

	str = str[1:]
	if len(str) == 0 {
		return nil, errNumberRange
	}

	rightInclude, err := isRightInclude(str[len(str)-1])
	if err != nil {
		return nil, err
	}

	str = str[:len(str)-1]
	fields := strings.Split(str, ":")
	if len(fields) != 2 {
		return nil, errNumberRange
	}

	if len(fields[0]) == 0 && len(fields[1]) == 0 {
		return nil, errNumberRange
	}

	var left float64
	if len(fields[0]) > 0 {
		if left, err = strconv.ParseFloat(fields[0], 64); err != nil {
			return nil, err
		}
	} else {
		left = -math.MaxFloat64
	}

	var right float64
	if len(fields[1]) > 0 {
		if right, err = strconv.ParseFloat(fields[1], 64); err != nil {
			return nil, err
		}
	} else {
		right = math.MaxFloat64
	}

	if left > right {
		return nil, errNumberRange
	}

	// [2:2] 有效, [2:2) 无效, (2:2] 无效, (2:2) 无效
	if left == right && (!leftInclude || !rightInclude) {
		return nil, errNumberRange
	}

	return &numberRange{
		left:         left,
		leftInclude:  leftInclude,
		right:        right,
		rightInclude: rightInclude,
	}, nil
}

func isLeftInclude(b byte) (bool, error) {
	switch b {
	case '[':
		return true, nil
	case '(':
		return false, nil
	default:
		return false, errNumberRange
	}
}

func isRightInclude(b byte) (bool, error) {
	switch b {
	case ']':
		return true, nil
	case ')':
		return false, nil
	default:
		return false, errNumberRange
	}
}

// parseSegments 将标签值按逗号分隔为段，但括号内的逗号不作为分隔符。
// 例如: "name,options=[a,b,c],range=[0:100]" => ["name", "options=[a,b,c]", "range=[0:100]"]
func parseSegments(val string) []string {
	var segments []string
	var escaped, grouped bool
	var buf strings.Builder

	for _, ch := range val {
		if escaped {
			buf.WriteRune(ch)
			escaped = false
			continue
		}

		switch ch {
		case segmentSeparator:
			if grouped {
				buf.WriteRune(ch)
			} else {
				segments = append(segments, strings.TrimSpace(buf.String()))
				buf.Reset()
			}
		case escapeChar:
			if grouped {
				buf.WriteRune(ch)
			} else {
				escaped = true
			}
		case leftBracket, leftSquareBracket:
			buf.WriteRune(ch)
			grouped = true
		case rightBracket, rightSquareBracket:
			buf.WriteRune(ch)
			grouped = false
		default:
			buf.WriteRune(ch)
		}
	}

	last := strings.TrimSpace(buf.String())
	if len(last) > 0 {
		segments = append(segments, last)
	}

	return segments
}

// parseGroupedSegments 解析被括号包围的值列表。
// 例如: "[a,b,c]" => ["a", "b", "c"]
func parseGroupedSegments(val string) []string {
	val = strings.TrimLeftFunc(val, func(r rune) bool {
		return r == leftBracket || r == leftSquareBracket
	})
	val = strings.TrimRightFunc(val, func(r rune) bool {
		return r == rightBracket || r == rightSquareBracket
	})
	return parseSegments(val)
}
