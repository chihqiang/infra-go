package mapping

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 optional 条件依赖（optional=Dep / optional=!Dep）的行为。
//
// 历史缺陷：OptionalDep 会被解析写入 fieldOptions，但 unmarshaler 从未读取它，
// 也不读取 Inherit。结果是文档承诺"当 other 未设置时此字段可选"，
// 实际字段**无条件可选**，且没有任何报错。

// TestUnmarshal_OptionalDep_PresentMakesOptional 验证 `optional=other`：
// 依赖存在时字段可选（可缺省）。
func TestUnmarshal_OptionalDep_PresentMakesOptional(t *testing.T) {
	type Config struct {
		Other string `json:"other,optional"`
		Value string `json:"value,optional=other"`
	}

	t.Run("dependency present, field omitted is allowed", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{"other": "x"}, &cfg)
		require.NoError(t, err)
		assert.Equal(t, "x", cfg.Other)
		assert.Equal(t, "", cfg.Value)
	})

	t.Run("dependency absent, field omitted is rejected", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{}, &cfg)
		require.Error(t, err, "without the dependency the field must be required")
		assert.Contains(t, err.Error(), "required because")
		assert.Contains(t, err.Error(), "other")
	})

	t.Run("dependency present and field set", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{"other": "x", "value": "v"}, &cfg)
		require.NoError(t, err)
		assert.Equal(t, "v", cfg.Value)
	})
}

// TestUnmarshal_OptionalDepNegated 验证 `optional=!other`：
// 依赖不存在时字段可选。
func TestUnmarshal_OptionalDepNegated(t *testing.T) {
	type Config struct {
		Other string `json:"other,optional"`
		Value string `json:"value,optional=!other"`
	}

	t.Run("dependency absent, field omitted is allowed", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{}, &cfg)
		require.NoError(t, err)
		assert.Equal(t, "", cfg.Value)
	})

	t.Run("dependency present, field omitted is rejected", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{"other": "x"}, &cfg)
		require.Error(t, err, "with the dependency set the field must be required")
		assert.Contains(t, err.Error(), "required because")
	})

	t.Run("dependency present and field set", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{"other": "x", "value": "v"}, &cfg)
		require.NoError(t, err)
		assert.Equal(t, "v", cfg.Value)
	})
}

// TestUnmarshal_OptionalDepWithDefault 验证有默认值时依赖不满足仍走默认值逻辑
// （默认值优先于"必填"判定）。
func TestUnmarshal_OptionalDepWithDefault(t *testing.T) {
	type Config struct {
		Other string `json:"other,optional"`
		Value string `json:"value,default=d,optional=other"`
	}

	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	require.NoError(t, err, "a default satisfies the field, so the dependency is irrelevant")
	assert.Equal(t, "d", cfg.Value)
}

// TestUnmarshal_OptionalDepUnknownDependency 回归测试：依赖名写错必须报错。
//
// 不校验的话，依赖名拼错会让字段**永久变为必填**（依赖永远找不到），
// 属于难以定位的静默行为偏差。
func TestUnmarshal_OptionalDepUnknownDependency(t *testing.T) {
	type Config struct {
		Other string `json:"other,optional"`
		Value string `json:"value,optional=missing"`
	}

	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"other": "x"}, &cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match any field key")
}

// TestUnmarshal_OptionalDepCaseInsensitiveViaCanonicalKey 验证依赖查找与字段查找
// 走同一条路径（设置 canonicalKey 时同样大小写不敏感）。
func TestUnmarshal_OptionalDepCaseInsensitiveViaCanonicalKey(t *testing.T) {
	type Config struct {
		Other string `json:"other,optional"`
		Value string `json:"value,optional=other"`
	}

	// 配置里键名是大写，字段标签是小写 → 依赖判定必须同样不敏感
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"OTHER": "x"}, &cfg,
		WithCanonicalKeyFunc(lowerFunc))
	require.NoError(t, err)
	assert.Equal(t, "x", cfg.Other)
}

// TestUnmarshal_OptionalDepNested 验证嵌套结构体中的条件可选独立生效。
func TestUnmarshal_OptionalDepNested(t *testing.T) {
	type Inner struct {
		Flag  string `json:"flag,optional"`
		Extra string `json:"extra,optional=flag"`
	}
	type Config struct {
		Inner Inner `json:"inner,optional"`
	}

	t.Run("nested dependency present", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{
			"inner": map[string]any{"flag": "on"},
		}, &cfg)
		require.NoError(t, err)
		assert.Equal(t, "on", cfg.Inner.Flag)
	})

	t.Run("nested dependency absent", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{
			"inner": map[string]any{},
		}, &cfg)
		require.Error(t, err, "nested optional dependency must be enforced too")
	})
}

// lowerFunc 供 WithCanonicalKeyFunc 使用，避免额外 import strings。
func lowerFunc(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
