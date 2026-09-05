package binding

import (
	"mime/multipart"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// 对应 meta.go：structMeta/fieldMeta 的解析与缓存。

type metaSample struct {
	Name   string                `form:"name,default=foo"`
	Skip   string                `form:"-"`
	When   time.Time             `form:"when" time_format:"2006-01-02"`
	Dur    time.Duration         `form:"dur"`
	File   *multipart.FileHeader `form:"file"`
	hidden string                // 未导出的非匿名字段，应被跳过
}

func findField(meta *structMeta, name string) *fieldMeta {
	for i := range meta.fields {
		if meta.fields[i].name == name {
			return &meta.fields[i]
		}
	}
	return nil
}

func TestParseStructMeta(t *testing.T) {
	meta := getStructMeta(reflect.TypeOf(metaSample{}), "form")

	// 未导出字段被跳过；导出字段共 5 个
	assert.Len(t, meta.fields, 5)
	assert.Nil(t, findField(meta, "hidden"))

	// default 选项
	f := findField(meta, "Name")
	assert.NotNil(t, f)
	assert.True(t, f.hasDefault)
	assert.Equal(t, "foo", f.defaultValue)

	// form:"-" 跳过
	sf := findField(meta, "Skip")
	assert.NotNil(t, sf)
	assert.True(t, sf.skip)

	// 类型预判：time / duration / fileHeader
	assert.True(t, findField(meta, "When").isTime)
	assert.True(t, findField(meta, "Dur").isDuration)
	assert.True(t, findField(meta, "File").isFileHeader)

	// time 标签缓存
	w := findField(meta, "When")
	assert.Equal(t, "2006-01-02", w.timeFormat)
	assert.False(t, w.timeUTC)
}

func TestStructMetaCache(t *testing.T) {
	typ := reflect.TypeOf(metaSample{})
	a := getStructMeta(typ, "form")
	b := getStructMeta(typ, "form")
	assert.Same(t, a, b) // 同类型同标签命中缓存

	// 不同标签（如 header）应为不同缓存条目
	c := getStructMeta(typ, "header")
	assert.NotSame(t, a, c)
}
