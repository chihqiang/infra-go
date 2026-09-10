package conf

// 本文件负责将解析出的配置 map 规整到可反序列化状态：
//   - normalizeMap/normalizeValue/...：把 YAML 产生的各类值统一为 json.Number
//   - unmarshalMap：规整后的 map 反序列化到目标结构体
//     （字段名大小写不敏感匹配由 mapping 的 canonicalKey 完成，
//     不在这里改写 map 的键，以免破坏 map 字段的数据）

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/chihqiang/infra-go/mapping"
)

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

// unmarshalMap 将解析后的配置 map 反序列化到 v。
// 内部通过 mapping.WithCanonicalKeyFunc 实现字段名大小写不敏感匹配，
// 供 Load / LoadFromJSONBytes / LoadFromYAMLBytes 复用。
//
// 注意：这里刻意**不**预先小写化输入 map 的键。map 的键同时也是
// map 类型字段的数据，整体小写化会静默破坏用户数据
// （labels: {AppName: x} 会被写成 appname，且 AppName/appname 并存时
// 因 map 遍历顺序不同而结果不确定）。大小写不敏感匹配由 mapping 侧完成。
func unmarshalMap(m map[string]any, v any) error {
	return mapping.UnmarshalJsonMap(m, v, mapping.WithCanonicalKeyFunc(strings.ToLower))
}
