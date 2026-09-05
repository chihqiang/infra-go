package binding

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 对应 query.go：QueryBinding。

type queryReq struct {
	Name  string `form:"name" binding:"required"`
	Age   int    `form:"age"`
	Email string `form:"email" binding:"required,email"`
}

func newQueryReq(rawurl string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, rawurl, nil)
	return req
}

func TestQuery_Bind(t *testing.T) {
	var q queryReq
	require.NoError(t, Query.Bind(newQueryReq("/?name=Alice&age=25&email=a@b.com"), &q))
	assert.Equal(t, "Alice", q.Name)
	assert.Equal(t, 25, q.Age)
	assert.Equal(t, "a@b.com", q.Email)
}

func TestQuery_ValidationError(t *testing.T) {
	var q queryReq
	require.Error(t, Query.Bind(newQueryReq("/?name=Alice&age=25"), &q)) // 缺 email
}

func TestQuery_DefaultValue(t *testing.T) {
	type dReq struct {
		Page int `form:"page,default=1"`
	}
	var d dReq
	require.NoError(t, Query.Bind(newQueryReq("/"), &d))
	assert.Equal(t, 1, d.Page)
}

func TestQuery_EmptyValueFallsBackToDefault(t *testing.T) {
	type dReq struct {
		Page int `form:"page,default=7"`
	}
	var d dReq
	require.NoError(t, Query.Bind(newQueryReq("/?page="), &d))
	assert.Equal(t, 7, d.Page)
}

func TestQuery_SliceFromComma(t *testing.T) {
	type sReq struct {
		Tags []string `form:"tags"`
	}
	var s sReq
	require.NoError(t, Query.Bind(newQueryReq("/?tags=a,b,c"), &s))
	assert.Equal(t, []string{"a", "b", "c"}, s.Tags)
}

func TestQuery_SliceRepeatedKey(t *testing.T) {
	type sReq struct {
		Tags []string `form:"tags"`
	}
	var s sReq
	require.NoError(t, Query.Bind(newQueryReq("/?tags=a&tags=b"), &s))
	assert.Equal(t, []string{"a", "b"}, s.Tags)
}

func TestQuery_Types(t *testing.T) {
	type typReq struct {
		Count int           `form:"count"`
		Rate  float64       `form:"rate"`
		On    bool          `form:"on"`
		Dur   time.Duration `form:"dur"`
		When  time.Time     `form:"when"`
	}
	var r typReq
	require.NoError(t, Query.Bind(newQueryReq(
		"/?count=10&rate=1.5&on=true&dur=1m30s&when=2024-01-02T15:04:05Z"), &r))
	assert.Equal(t, 10, r.Count)
	assert.Equal(t, 1.5, r.Rate)
	assert.True(t, r.On)
	assert.Equal(t, 90*time.Second, r.Dur)
	assert.Equal(t, time.Date(2024, 1, 2, 15, 4, 5, 0, time.UTC), r.When)
}

func TestQuery_IntOverflow(t *testing.T) {
	type iReq struct {
		V int8 `form:"v"`
	}
	var r iReq
	require.Error(t, Query.Bind(newQueryReq("/?v=300"), &r)) // int8 溢出
}
