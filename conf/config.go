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

	var opt options
	for _, o := range opts {
		o(&opt)
	}

	if opt.env {
		content = []byte(ExpandEnv(string(content)))
	}

	m, err := loader(content)
	if err != nil {
		return fmt.Errorf("failed to parse %s config: %w", ext, err)
	}

	if err := unmarshalMap(m, v); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return validate(v)
}

// ExpandEnv 展开文本中的环境变量引用，支持两种形式：
//   - ${VAR} / $VAR         与 os.ExpandEnv 一致，未设置时展开为空字符串
//   - ${VAR:-default}       当 VAR 未设置或为空时，展开为 default（字面值）
//
// 供 conf.Load 配合 UseEnv 内部调用；也可独立用于展开任意配置文本。
// 基于 os.Expand 实现：它已将 $VAR / ${VAR} 语法解析好，并把大括号内的整段内容
// （如 "VAR:-default"）作为变量名传给回调，因此只需在回调中识别 :- 后缀即可。
func ExpandEnv(s string) string {
	return os.Expand(s, func(name string) string {
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

// MustLoad 从文件加载配置到 v 中，出错时直接 panic。
func MustLoad(path string, v any, opts ...Option) {
	if err := Load(path, v, opts...); err != nil {
		panic(fmt.Errorf("failed to load config %s: %w", path, err))
	}
}

// LoadFromJSONBytes 从 JSON 字节加载配置到 v 中。
func LoadFromJSONBytes(content []byte, v any) error {
	m, err := loadFromJSONBytes(content)
	if err != nil {
		return err
	}
	if err := unmarshalMap(m, v); err != nil {
		return err
	}
	return validate(v)
}

// LoadFromYAMLBytes 从 YAML 字节加载配置到 v 中。
func LoadFromYAMLBytes(content []byte, v any) error {
	m, err := loadFromYAMLBytes(content)
	if err != nil {
		return err
	}
	if err := unmarshalMap(m, v); err != nil {
		return err
	}
	return validate(v)
}
