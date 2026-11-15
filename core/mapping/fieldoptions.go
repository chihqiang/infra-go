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

	// optionalNegatePrefix is the negation prefix for optional dependencies,
	// e.g. `optional=!Other`.
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

// fieldOptions holds the field options parsed from a struct tag.
type fieldOptions struct {
	// Default is the field's default value.
	Default string
	// EnvVar is the environment variable name; when set, the value is read from
	// it first.
	EnvVar string
	// Optional reports whether the field is optional.
	Optional bool
	// OptionalDep is the optional dependency used for conditional optionality.
	// The tag `optional=Other` means this field is optional only when Other is
	// set; `optional=!Other` means it is optional only when Other is not set.
	OptionalDep string
	// OptionalDepNegate reports whether OptionalDep carries the "!" negation prefix.
	OptionalDepNegate bool
	// Options is the list of allowed values.
	Options []string
	// Range is the numeric range.
	Range *numberRange
	// FromString reports whether the value is parsed from a string.
	FromString bool
}

// numberRange represents a numeric range.
type numberRange struct {
	left         float64
	leftInclude  bool
	right        float64
	rightInclude bool
}

// hasDefault reports whether a default value is set.
func (o *fieldOptions) hasDefault() (string, bool) {
	if o == nil {
		return "", false
	}
	return o.Default, len(o.Default) > 0
}

// isOptional reports whether the field is optional.
func (o *fieldOptions) isOptional() bool {
	return o != nil && o.Optional
}

// allowedOptions returns the list of allowed values.
func (o *fieldOptions) allowedOptions() []string {
	if o == nil {
		return nil
	}
	return o.Options
}

// isFromString reports whether the value is parsed from a string.
func (o *fieldOptions) isFromString() bool {
	return o != nil && o.FromString
}

// isInRange reports whether the number lies within the range.
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

// fieldOptionsCacheValue caches the parsed tag options to avoid re-parsing.
type fieldOptionsCacheValue struct {
	key     string
	options *fieldOptions
	err     error
}

var (
	optionsCache     = make(map[string]fieldOptionsCacheValue)
	optionsCacheLock sync.RWMutex
)

// parseKeyAndOptions parses the key name and options from a struct field tag.
// tagName is the tag name, usually "json".
// It returns the parsed key name (the field name when the tag is empty), the
// options and an error.
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

// doParseKeyAndOptions performs the actual tag value parsing.
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

// parseOption parses a single option.
//
// Both the `key` and the `key=value` forms are supported (value may not contain
// another `=`).
//
// Unknown options always return an error: the old implementation matched each
// prefix with strings.HasPrefix and had no default branch, so
//   - typos were silently ignored (`optinal` did not fail; the field was treated
//     as required and only surfaced at runtime as "field not set", with an error
//     message that pointed at the wrong cause);
//   - prefixes matched incorrectly (`defaultFoo=bar` took effect as
//     `default=bar`).
func parseOption(opts *fieldOptions, fieldName, option string) error {
	name, value, hasValue := strings.Cut(option, equalToken)
	name = strings.TrimSpace(name)

	// Validate the shape of value: no second "=" and no empty value are allowed
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
		// `optional=!Other` -> negated dependency
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
		// inherit used to be parsed and stored here, but the unmarshaler never
		// read it: an option that "promised" something and did nothing. Its
		// semantics are undefined in this design (tag-driven unmarshalling has no
		// notion of a "parent"), so it is rejected explicitly instead of being
		// silently ignored.
		return fmt.Errorf(
			"option %q of field %q is not supported: "+
				"define the value explicitly or give it a default instead",
			optionInherit, fieldName)

	default:
		return fmt.Errorf("unknown option %q for field %q", name, fieldName)
	}
}

// parseOptionsValue parses the list of allowed values.
// Two formats are supported: [a,b,c] or a|b|c
func parseOptionsValue(val string) []string {
	if len(val) == 0 {
		return nil
	}
	if val[0] == leftSquareBracket {
		return parseGroupedSegments(val)
	}
	return strings.Split(val, optionSeparator)
}

// parseNumberRange parses a numeric range.
// The following formats are supported:
//
//	[:5]  (:5]  [:5)  (:5)    — upper bound only
//	[1:]  [1:)  (1:]  (1:)    — lower bound only
//	[1:5] [1:5] (1:5] (1:5)   — both bounds
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

	// [2:2] is valid; [2:2), (2:2] and (2:2) are invalid
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

// parseSegments splits a tag value into segments on commas, but commas inside
// brackets are not treated as separators.
// For example: "name,options=[a,b,c],range=[0:100]" => ["name", "options=[a,b,c]", "range=[0:100]"]
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

// parseGroupedSegments parses a bracket-wrapped value list.
// For example: "[a,b,c]" => ["a", "b", "c"]
func parseGroupedSegments(val string) []string {
	val = strings.TrimLeftFunc(val, func(r rune) bool {
		return r == leftBracket || r == leftSquareBracket
	})
	val = strings.TrimRightFunc(val, func(r rune) bool {
		return r == rightBracket || r == rightSquareBracket
	})
	return parseSegments(val)
}
