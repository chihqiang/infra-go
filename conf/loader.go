package conf

import (
	"bytes"
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// loaders is the global extension-to-loader map, keeping the supported file types in
// one place. Every loader parses file content into a map[string]any and hands it to the
// mapping package.
var loaders = map[string]func([]byte) (map[string]any, error){
	".json": loadFromJSONBytes,
	".yaml": loadFromYAMLBytes,
	".yml":  loadFromYAMLBytes,
}

// loadFromJSONBytes parses JSON bytes into a map[string]any.
// json.Number is used to preserve numeric precision.
func loadFromJSONBytes(content []byte) (map[string]any, error) {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(content))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// loadFromYAMLBytes parses YAML bytes into a map[string]any.
// YAML numeric types are converted to json.Number internally, matching the JSON path.
func loadFromYAMLBytes(content []byte) (map[string]any, error) {
	var m map[string]any
	if err := yaml.Unmarshal(content, &m); err != nil {
		return nil, err
	}
	return normalizeMap(m), nil
}
