package conf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chihqiang/infra-go/mapping"
	"gopkg.in/yaml.v3"
)

// loaders 全局后缀-加载器映射，统一管理支持的文件类型。
// 各加载器将文件内容解析为 map[string]any，统一交给 mapping 包处理。
var loaders = map[string]func([]byte) (map[string]any, error){
	".json": loadFromJSONBytes,
	".yaml": loadFromYAMLBytes,
	".yml":  loadFromYAMLBytes,
}

// fillDefaultUnmarshaler 用于 FillDefault 的反序列化器。
var fillDefaultUnmarshaler = mapping.NewUnmarshaler(jsonTagKey, mapping.WithDefault())

const jsonTagKey = "json"

// FillDefault 为给定结构体填充默认值和环境变量。
// 前提是结构体的所有字段必须为零值。
func FillDefault(v any) error {
	return fillDefaultUnmarshaler.Unmarshal(map[string]any{}, v)
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
		content = []byte(os.ExpandEnv(string(content)))
	}

	m, err := loader(content)
	if err != nil {
		return fmt.Errorf("failed to parse %s config: %w", ext, err)
	}

	// 将 key 转小写以支持大小写不敏感匹配
	m = lowercaseKeys(m)

	if err := mapping.UnmarshalJsonMap(m, v, mapping.WithCanonicalKeyFunc(strings.ToLower)); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return validate(v)
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
	m = lowercaseKeys(m)
	if err := mapping.UnmarshalJsonMap(m, v, mapping.WithCanonicalKeyFunc(strings.ToLower)); err != nil {
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
	m = lowercaseKeys(m)
	if err := mapping.UnmarshalJsonMap(m, v, mapping.WithCanonicalKeyFunc(strings.ToLower)); err != nil {
		return err
	}
	return validate(v)
}

// lowercaseKeys 递归地将 map 中所有 key 转为小写，支持大小写不敏感匹配。
func lowercaseKeys(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[strings.ToLower(k)] = lowercaseValues(v)
	}
	return result
}

// lowercaseValues 递归地将嵌套 map 中的 key 转为小写。
func lowercaseValues(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case map[string]any:
		return lowercaseKeys(val)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = lowercaseValues(item)
		}
		return result
	default:
		return v
	}
}

// loadFromJSONBytes 将 JSON 字节解析为 map[string]any。
// 使用 json.Number 保持数值精度。
func loadFromJSONBytes(content []byte) (map[string]any, error) {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(content))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// loadFromYAMLBytes 将 YAML 字节解析为 map[string]any。
// 内部将 YAML 的数值类型统一转换为 json.Number，保持与 JSON 一致的处理逻辑。
func loadFromYAMLBytes(content []byte) (map[string]any, error) {
	var m map[string]any
	if err := yaml.Unmarshal(content, &m); err != nil {
		return nil, err
	}
	return normalizeMap(m), nil
}

// normalizeMap 递归地将 map 中的值规范化：
// - 将各种数值类型（int, int64, float64 等）统一转为 json.Number
// - 确保所有嵌套的 map 键为 string 类型
func normalizeMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = normalizeValue(v)
	}
	return result
}

// normalizeValue 递归规范化值。
func normalizeValue(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case bool, string:
		return val
	case int:
		return json.Number(strconv.FormatInt(int64(val), 10))
	case int8:
		return json.Number(strconv.FormatInt(int64(val), 10))
	case int16:
		return json.Number(strconv.FormatInt(int64(val), 10))
	case int32:
		return json.Number(strconv.FormatInt(int64(val), 10))
	case int64:
		return json.Number(strconv.FormatInt(val, 10))
	case uint:
		return json.Number(strconv.FormatUint(uint64(val), 10))
	case uint8:
		return json.Number(strconv.FormatUint(uint64(val), 10))
	case uint16:
		return json.Number(strconv.FormatUint(uint64(val), 10))
	case uint32:
		return json.Number(strconv.FormatUint(uint64(val), 10))
	case uint64:
		return json.Number(strconv.FormatUint(val, 10))
	case float32:
		return json.Number(strconv.FormatFloat(float64(val), 'f', -1, 32))
	case float64:
		return json.Number(strconv.FormatFloat(val, 'f', -1, 64))
	case json.Number:
		return val
	case map[string]any:
		return normalizeMap(val)
	case map[any]any:
		return normalizeAnyKeyMap(val)
	case []any:
		return normalizeSlice(val)
	case []map[string]any:
		slice := make([]any, len(val))
		for i, item := range val {
			slice[i] = normalizeMap(item)
		}
		return slice
	case []map[any]any:
		slice := make([]any, len(val))
		for i, item := range val {
			slice[i] = normalizeAnyKeyMap(item)
		}
		return slice
	default:
		return fmt.Sprintf("%v", val)
	}
}

// normalizeAnyKeyMap 将 map[any]any 转换为 map[string]any。
func normalizeAnyKeyMap(m map[any]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[fmt.Sprintf("%v", k)] = normalizeValue(v)
	}
	return result
}

// normalizeSlice 规范化切片中的每个元素。
func normalizeSlice(s []any) []any {
	if s == nil {
		return nil
	}
	result := make([]any, len(s))
	for i, v := range s {
		result[i] = normalizeValue(v)
	}
	return result
}
