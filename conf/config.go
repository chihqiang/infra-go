package conf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chihqiang/infra-go/mapping"
)

// FillDefault fills default values and environment variables into the given struct.
// It requires all fields of the struct to be zero values.
func FillDefault(v any) error {
	return mapping.FillDefault(v)
}

// Load loads configuration from a file into v, supporting the .json, .yaml and .yml
// formats. The loading behaviour can be customised through opts, e.g. UseEnv() to
// expand environment variable references.
func Load(file string, v any, opts ...Option) error {
	content, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", file, err)
	}

	ext := strings.ToLower(filepath.Ext(file))
	loader, ok := loaders[ext]
	if !ok {
		return fmt.Errorf("unsupported config file type: %s, supported: .json .yaml .yml", ext)
	}

	m, err := loader(content)
	if err != nil {
		return fmt.Errorf("failed to parse %s config: %w", ext, err)
	}

	return loadFromMap(m, v, opts...)
}

// loadFromMap handles an already parsed config map: it expands environment variables
// according to opts, then unmarshals into v and validates it. It is shared by Load /
// LoadFromJSONBytes / LoadFromYAMLBytes so that every entry point behaves the same.
func loadFromMap(m map[string]any, v any, opts ...Option) error {
	var opt options
	for _, o := range opts {
		o(&opt)
	}

	if opt.env {
		expanded, err := expandEnvMap(m)
		if err != nil {
			return fmt.Errorf("failed to expand env variables: %w", err)
		}
		m = expanded
	}

	if err := unmarshalMap(m, v); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return validate(v)
}

// ExpandEnv expands environment variable references in text. Supported forms:
//   - ${VAR} / $VAR         expands to the empty string when unset
//   - ${VAR:-default}       expands to default (a literal) when VAR is unset or empty
//   - $$                    expands to a literal $ (escape)
//
// Used internally by conf together with UseEnv; it is also usable standalone
// to expand arbitrary configuration text.
//
// Built on os.Expand: that already parses the $VAR / ${VAR} syntax and passes the
// whole braced content (e.g. "VAR:-default") to the callback as the variable name,
// so the callback only has to recognise the ":-" suffix.
// os.Expand treats "$" as a single-character special variable, hence "$$" is passed
// to the callback under the name "$". That is how escaping works -- otherwise a
// literal $ could not be written in a config (a password "p$ssword" would be
// swallowed down to "p").
func ExpandEnv(s string) string {
	return os.Expand(s, func(name string) string {
		if name == expandDollarEscape {
			return "$"
		}
		key, def, hasDefault := strings.Cut(name, ":-")
		if v, ok := os.LookupEnv(key); ok && v != "" {
			return v
		}
		if hasDefault {
			return def
		}
		return ""
	})
}

// expandDollarEscape is the variable name "$$" appears under in the os.Expand callback.
const expandDollarEscape = "$"

// expandEnvMap recursively expands environment variable references in every string of
// the config tree (including map keys).
//
// Why expand after parsing instead of doing a textual replacement on the raw text
// before parsing: a pre-parse textual replacement lets environment variable values
// take part in syntax parsing, which causes two classes of problems --
//   - a literal $ in the config is swallowed as a variable reference ("p$ssword" -> "p");
//   - environment variable values can inject/rewrite the config structure (a value
//     containing `","admin":true` adds a new key).
//
// Parsing first and rewriting the strings afterwards means environment variable values
// only ever land in some string value or key name and are never re-parsed, so neither
// problem can occur.
//
// Keys are expanded as well (keeping the previous behaviour); because nothing is
// re-parsed, an expanded key is just an ordinary key name and is equally safe. If two
// different keys expand to the same name, an error is returned instead of silently
// dropping data.
//
// Note: environment variable values are always written as strings. If a config item
// needs an array or an object, write that structure directly in the config file, or
// parse this string yourself in application code.
func expandEnvMap(m map[string]any) (map[string]any, error) {
	if m == nil {
		return nil, nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		expandedKey := ExpandEnv(k)
		if _, dup := result[expandedKey]; dup {
			return nil, fmt.Errorf("env expansion produces duplicate key %q", expandedKey)
		}
		ev, err := expandEnvValue(v)
		if err != nil {
			return nil, err
		}
		result[expandedKey] = ev
	}
	return result, nil
}

// expandEnvValue expands environment variable references inside a single config value.
func expandEnvValue(v any) (any, error) {
	switch val := v.(type) {
	case string:
		return ExpandEnv(val), nil
	case map[string]any:
		return expandEnvMap(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			ev, err := expandEnvValue(item)
			if err != nil {
				return nil, err
			}
			out[i] = ev
		}
		return out, nil
	default:
		return v, nil
	}
}

// MustLoad loads configuration from a file into v and panics on error.
func MustLoad(path string, v any, opts ...Option) {
	if err := Load(path, v, opts...); err != nil {
		panic(fmt.Errorf("failed to load config %s: %w", path, err))
	}
}

// LoadFromJSONBytes loads configuration from JSON bytes into v. It supports the same
// opts as Load (e.g. UseEnv).
func LoadFromJSONBytes(content []byte, v any, opts ...Option) error {
	m, err := loadFromJSONBytes(content)
	if err != nil {
		return err
	}
	return loadFromMap(m, v, opts...)
}

// LoadFromYAMLBytes loads configuration from YAML bytes into v. It supports the same
// opts as Load (e.g. UseEnv).
func LoadFromYAMLBytes(content []byte, v any, opts ...Option) error {
	m, err := loadFromYAMLBytes(content)
	if err != nil {
		return err
	}
	return loadFromMap(m, v, opts...)
}
