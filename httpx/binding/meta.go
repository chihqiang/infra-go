package binding

import (
	"mime/multipart"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Struct field metadata cache ---

// Standard types used for type pre-detection.
var (
	timeType       = reflect.TypeOf(time.Time{})
	durationType   = reflect.TypeOf(time.Duration(0))
	fileHeaderType = reflect.TypeOf(multipart.FileHeader{})
)

// structMeta holds the pre-parsed field metadata of a struct.
type structMeta struct {
	fields []fieldMeta
}

// fieldMeta holds the pre-parsed metadata of a struct field.
// The binding hot path uses cached information to avoid repeated tag parsing
// and type assertions on every request.
type fieldMeta struct {
	sf           reflect.StructField // original field information (used by the setter)
	name         string              // field name
	tagKey       string              // tag key (empty when no tag is set; falls back to the field name)
	skip         bool                // tag is "-", ignore the field
	hasDefault   bool                // whether a default option exists
	defaultValue string              // value of the default option
	anonymous    bool                // whether the field is anonymous (embedded)
	index        int                 // index of the field in the struct

	// Type pre-detection, replacing the runtime value.Interface().(type) assertion
	isTime       bool
	isDuration   bool
	isFileHeader bool

	// Cache for the time_format / time_utc / time_location tags
	timeFormat   string
	timeUTC      bool
	timeLocation string
}

// metaCacheKey is the struct metadata cache key: type + binding tag.
type metaCacheKey struct {
	typ reflect.Type
	tag string
}

// structMetaCache caches struct metadata.
var structMetaCache sync.Map

// getStructMeta returns (parsing and caching if needed) the struct field metadata for the given tag.
func getStructMeta(typ reflect.Type, tag string) *structMeta {
	key := metaCacheKey{typ: typ, tag: tag}
	if v, ok := structMetaCache.Load(key); ok {
		return v.(*structMeta)
	}
	meta := parseStructMeta(typ, tag)
	actual, _ := structMetaCache.LoadOrStore(key, meta)
	return actual.(*structMeta)
}

// parseStructMeta parses the struct field metadata.
func parseStructMeta(typ reflect.Type, tag string) *structMeta {
	meta := &structMeta{}
	n := typ.NumField()
	meta.fields = make([]fieldMeta, 0, n)
	for i := 0; i < n; i++ {
		sf := typ.Field(i)
		// Skip unexported non-anonymous fields
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

		// Parse tag options (default, etc.)
		var opt string
		for len(opts) > 0 {
			opt, opts = head(opts, ",")
			if k, v := head(opt, "="); k == "default" {
				fm.hasDefault = true
				fm.defaultValue = v
			}
		}

		// Type pre-detection (the base type after dereferencing pointers)
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

		// Cache the time-related tags
		if fm.isTime {
			fm.timeFormat = sf.Tag.Get("time_format")
			fm.timeUTC, _ = strconv.ParseBool(sf.Tag.Get("time_utc"))
			fm.timeLocation = sf.Tag.Get("time_location")
		}

		meta.fields = append(meta.fields, fm)
	}
	return meta
}

// head returns the part before the separator and the remainder.
func head(str, sep string) (head string, tail string) {
	head, tail, _ = strings.Cut(str, sep)
	return head, tail
}
