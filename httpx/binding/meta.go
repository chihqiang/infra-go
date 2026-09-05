package binding

import (
	"mime/multipart"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- 结构体字段元信息缓存 ---

// 类型预判用到的标准类型。
var (
	timeType       = reflect.TypeOf(time.Time{})
	durationType   = reflect.TypeOf(time.Duration(0))
	fileHeaderType = reflect.TypeOf(multipart.FileHeader{})
)

// structMeta 结构体的预解析字段元信息。
type structMeta struct {
	fields []fieldMeta
}

// fieldMeta 预解析的结构体字段元信息。
// 绑定热路径使用缓存信息，避免每次请求重复的 Tag 解析与类型断言。
type fieldMeta struct {
	sf           reflect.StructField // 原始字段信息（供 setter 使用）
	name         string              // 字段名
	tagKey       string              // 标签键名（未设置标签时为空，回退用字段名）
	skip         bool                // 标签为 "-"，忽略
	hasDefault   bool                // 是否有 default 选项
	defaultValue string              // default 选项值
	anonymous    bool                // 是否匿名（内嵌）字段
	index        int                 // 字段在结构体中的索引

	// 类型预判，替代运行时 value.Interface().(type) 断言
	isTime       bool
	isDuration   bool
	isFileHeader bool

	// time_format / time_utc / time_location 标签缓存
	timeFormat   string
	timeUTC      bool
	timeLocation string
}

// metaCacheKey 结构体元信息缓存键：类型 + 绑定标签。
type metaCacheKey struct {
	typ reflect.Type
	tag string
}

// structMetaCache 结构体元信息缓存。
var structMetaCache sync.Map

// getStructMeta 获取（或解析并缓存）指定标签下的结构体字段元信息。
func getStructMeta(typ reflect.Type, tag string) *structMeta {
	key := metaCacheKey{typ: typ, tag: tag}
	if v, ok := structMetaCache.Load(key); ok {
		return v.(*structMeta)
	}
	meta := parseStructMeta(typ, tag)
	actual, _ := structMetaCache.LoadOrStore(key, meta)
	return actual.(*structMeta)
}

// parseStructMeta 解析结构体字段元信息。
func parseStructMeta(typ reflect.Type, tag string) *structMeta {
	meta := &structMeta{}
	n := typ.NumField()
	meta.fields = make([]fieldMeta, 0, n)
	for i := 0; i < n; i++ {
		sf := typ.Field(i)
		// 跳过未导出的非匿名字段
		if sf.PkgPath != "" && !sf.Anonymous {
			continue
		}

		fm := fieldMeta{
			sf:        sf,
			name:      sf.Name,
			index:     i,
			anonymous: sf.Anonymous,
		}

		tagRaw := sf.Tag.Get(tag)
		if tagRaw == "-" {
			fm.skip = true
			meta.fields = append(meta.fields, fm)
			continue
		}
		tagValue, opts := head(tagRaw, ",")
		fm.tagKey = tagValue

		// 解析标签选项（default 等）
		var opt string
		for len(opts) > 0 {
			opt, opts = head(opts, ",")
			if k, v := head(opt, "="); k == "default" {
				fm.hasDefault = true
				fm.defaultValue = v
			}
		}

		// 类型预判（解引用指针后的基础类型）
		base := sf.Type
		for base.Kind() == reflect.Ptr {
			base = base.Elem()
		}
		switch base {
		case timeType:
			fm.isTime = true
		case durationType:
			fm.isDuration = true
		case fileHeaderType:
			fm.isFileHeader = true
		}

		// 时间标签缓存
		if fm.isTime {
			fm.timeFormat = sf.Tag.Get("time_format")
			fm.timeUTC, _ = strconv.ParseBool(sf.Tag.Get("time_utc"))
			fm.timeLocation = sf.Tag.Get("time_location")
		}

		meta.fields = append(meta.fields, fm)
	}
	return meta
}

// head 返回分隔符前的部分和剩余部分。
func head(str, sep string) (head string, tail string) {
	head, tail, _ = strings.Cut(str, sep)
	return head, tail
}
