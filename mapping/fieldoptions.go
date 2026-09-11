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

	// optionalNegatePrefix 是 optional 依赖的取反前缀，如 `optional=!Other`。
	optionalNegatePrefix = "!"

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
	// 标签 `optional=Other` 表示只有在 Other 已设置时此字段才可选；
	// `optional=!Other` 表示只有在 Other 未设置时此字段才可选。
	OptionalDep string
	// OptionalDepNegate 表示 OptionalDep 是否带 "!" 前缀（取反）。
	OptionalDepNegate bool
	// Options 允许的值列表。
	Options []string
	// Range 数值范围。
	Range *numberRange
	// FromString 是否从字符串解析值。
	FromString bool
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
//
// 支持 `key` 与 `key=value` 两种形式（value 中不允许再出现 `=`）。
//
// 未知选项一律返回错误：旧实现用 strings.HasPrefix 逐个匹配且没有 default 分支，导致
//   - 拼写错误被静默忽略（`optinal` 不报错，字段按必填处理，直到运行期才以
//     "field not set" 暴露，错误信息也不指向真实原因）；
//   - 前缀误匹配（`defaultFoo=bar` 被当成 `default=bar` 生效）。
func parseOption(opts *fieldOptions, fieldName, option string) error {
	name, value, hasValue := strings.Cut(option, equalToken)
	name = strings.TrimSpace(name)

	// 校验 value 形状：不允许出现第二个 "="，也不允许空值
	if hasValue {
		if strings.Contains(value, equalToken) {
			return fmt.Errorf("invalid %q option for field %q", name, fieldName)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("invalid %q option for field %q: empty value", name, fieldName)
		}
	}

	switch name {
	case optionOptional:
		opts.Optional = true
		if !hasValue {
			return nil
		}
		// `optional=!Other` → 依赖取反
		dep := value
		if strings.HasPrefix(dep, optionalNegatePrefix) {
			opts.OptionalDepNegate = true
			dep = strings.TrimPrefix(dep, optionalNegatePrefix)
			if dep == "" {
				return fmt.Errorf("invalid optional option for field %q: empty dependency after %q",
					fieldName, optionalNegatePrefix)
			}
		}
		opts.OptionalDep = dep
		return nil

	case optionString:
		if hasValue {
			return fmt.Errorf("option %q of field %q does not take a value", name, fieldName)
		}
		opts.FromString = true
		return nil

	case optionOptions:
		if !hasValue {
			return fmt.Errorf("invalid %q option for field %q", name, fieldName)
		}
		opts.Options = parseOptionsValue(value)
		return nil

	case optionDefault:
		if !hasValue {
			return fmt.Errorf("invalid %q option for field %q", name, fieldName)
		}
		opts.Default = value
		return nil

	case optionEnv:
		if !hasValue {
			return fmt.Errorf("invalid %q option for field %q", name, fieldName)
		}
		opts.EnvVar = value
		return nil

	case optionRange:
		if !hasValue {
			return fmt.Errorf("invalid %q option for field %q", name, fieldName)
		}
		nr, err := parseNumberRange(value)
		if err != nil {
			return err
		}
		opts.Range = nr
		return nil

	case optionInherit:
		// inherit 曾在此解析并置位，但从未被 unmarshaler 读取，
		// 属于"文档承诺了却什么都没做"的选项。该语义在本设计中没有定义
		// （标签驱动的反序列化没有"父级"概念），因此明确拒绝而不是静默忽略。
		return fmt.Errorf(
			"option %q of field %q is not supported: "+
				"define the value explicitly or give it a default instead",
			optionInherit, fieldName)

	default:
		return fmt.Errorf("unknown option %q for field %q", name, fieldName)
	}
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
