package conf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chihqiang/infra-go/mapping"
)

// FillDefault 为给定结构体填充默认值和环境变量。
// 前提是结构体的所有字段必须为零值。
func FillDefault(v any) error {
	return mapping.FillDefault(v)
}

// Load 从文件加载配置到 v 中，支持 .json, .yaml, .yml 格式。
// 可通过 opts 选项自定义加载行为，例如 UseEnv() 展开环境变量引用。
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

// loadFromMap 处理已解析的配置 map：按 opts 展开环境变量，再反序列化到 v 并校验。
// 供 Load / LoadFromJSONBytes / LoadFromYAMLBytes 复用，保证各入口行为一致。
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

// ExpandEnv 展开文本中的环境变量引用，支持：
//   - ${VAR} / $VAR         未设置时展开为空字符串
//   - ${VAR:-default}       当 VAR 未设置或为空时，展开为 default（字面值）
//   - $$                    展开为字面量 $（转义）
//
// 供 conf 配合 UseEnv 内部调用；也可独立用于展开任意配置文本。
//
// 基于 os.Expand 实现：它已将 $VAR / ${VAR} 语法解析好，并把大括号内的整段内容
// （如 "VAR:-default"）作为变量名传给回调，因此只需在回调中识别 :- 后缀即可。
// os.Expand 把 "$" 当作单字符特殊变量，所以 "$$" 会以名字 "$" 回调，
// 借此实现转义——否则配置中无法书写字面 $（如密码 "p$ssword" 会被吞成 "p"）。
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

// expandDollarEscape 是 "$$" 在 os.Expand 回调中呈现的变量名。
const expandDollarEscape = "$"

// expandEnvMap 递归展开配置树中所有字符串（含 map 的键）里的环境变量引用。
//
// 为什么在解析之后展开，而不是解析前对原文做文本替换：
// 解析前的文本替换会让环境变量的值参与语法解析，由此带来两类问题——
//   - 配置中的字面 $ 被当成变量引用吞掉（"p$ssword" 变成 "p"）；
//   - 环境变量的值能注入/改写配置结构（值中含 `","admin":true` 时新增一个键）。
//
// 先解析、再改写字符串，环境变量的值只会落到某个字符串值或键名上，
// 不会被重新解析，因此不存在上述问题。
//
// 键同样会被展开（与旧行为保持一致）；由于不再重新解析，展开后的键只是一个
// 普通键名，同样安全。若不同的键展开成同一个名字，返回错误而不是静默丢弃数据。
//
// 注意：环境变量的值一律作为字符串写入。若某个配置项需要数组或对象，
// 请在配置文件中直接书写该结构，或在应用代码中自行解析这个字符串。
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

// expandEnvValue 展开单个配置值中的环境变量引用。
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

// MustLoad 从文件加载配置到 v 中，出错时直接 panic。
func MustLoad(path string, v any, opts ...Option) {
	if err := Load(path, v, opts...); err != nil {
		panic(fmt.Errorf("failed to load config %s: %w", path, err))
	}
}

// LoadFromJSONBytes 从 JSON 字节加载配置到 v 中，支持与 Load 相同的 opts（如 UseEnv）。
func LoadFromJSONBytes(content []byte, v any, opts ...Option) error {
	m, err := loadFromJSONBytes(content)
	if err != nil {
		return err
	}
	return loadFromMap(m, v, opts...)
}

// LoadFromYAMLBytes 从 YAML 字节加载配置到 v 中，支持与 Load 相同的 opts（如 UseEnv）。
func LoadFromYAMLBytes(content []byte, v any, opts ...Option) error {
	m, err := loadFromYAMLBytes(content)
	if err != nil {
		return err
	}
	return loadFromMap(m, v, opts...)
}
