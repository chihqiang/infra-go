package conf

import (
	"bytes"
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// loaders 全局后缀-加载器映射，统一管理支持的文件类型。
// 各加载器将文件内容解析为 map[string]any，统一交给 mapping 包处理。
var loaders = map[string]func([]byte) (map[string]any, error){
	".json": loadFromJSONBytes,
	".yaml": loadFromYAMLBytes,
	".yml":  loadFromYAMLBytes,
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
