package binding

import (
	"mime/multipart"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Covers meta.go: parsing and caching of structMeta/fieldMeta.

type metaSample struct {
	Name string                `form:"name,default=foo"`
	Skip string                `form:"-"`
	When time.Time             `form:"when" time_format:"2006-01-02"`
	Dur  time.Duration         `form:"dur"`
	File *multipart.FileHeader `form:"file"`
	// hidden is an unexported field: a test fixture used only to verify that the parser skips
	// it (its value does not affect parsing)
	hidden string
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
	// Build an instance carrying an unexported field value: verifies that the parser skips
	// that field (the value does not affect the metadata)
	sample := metaSample{hidden: "should-not-appear"}
	meta := getStructMeta(reflect.TypeOf(sample), "form")

	// Unexported fields are skipped; there are 5 exported fields
	assert.Len(t, meta.fields, 5)
	assert.Nil(t, findField(meta, "hidden"))

	// default option
	f := findField(meta, "Name")
	assert.NotNil(t, f)
	assert.True(t, f.hasDefault)
	assert.Equal(t, "foo", f.defaultValue)

	// form:"-" is skipped
	sf := findField(meta, "Skip")
	assert.NotNil(t, sf)
	assert.True(t, sf.skip)

	// Type pre-detection: time / duration / fileHeader
	assert.True(t, findField(meta, "When").isTime)
	assert.True(t, findField(meta, "Dur").isDuration)
	assert.True(t, findField(meta, "File").isFileHeader)

	// time tag cache
	w := findField(meta, "When")
	assert.Equal(t, "2006-01-02", w.timeFormat)
	assert.False(t, w.timeUTC)
}

func TestStructMetaCache(t *testing.T) {
	typ := reflect.TypeOf(metaSample{})
	a := getStructMeta(typ, "form")
	b := getStructMeta(typ, "form")
	assert.Same(t, a, b) // same type and tag hit the cache

	// Different tags (e.g. header) should be different cache entries
	c := getStructMeta(typ, "header")
	assert.NotSame(t, a, c)
}
