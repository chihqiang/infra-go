package httpx

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// discardWriter 丢弃响应内容的 ResponseWriter，用于基准测试避免内存累积。
type discardWriter struct {
	header http.Header
}

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *discardWriter) WriteHeader(int)             {}

func newDiscardWriter() *discardWriter {
	return &discardWriter{header: make(http.Header)}
}

// --- 路由分发基准 ---

// BenchmarkDispatch 基准：注册 10 条路由后请求分发（含全局中间件）。
func BenchmarkDispatch(b *testing.B) {
	s := newTestServer()
	s.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r)
		}
	})
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/users/%d", i)
		s.AddRoute(Route{
			Method: "GET", Path: path, Handler: func(w http.ResponseWriter, r *http.Request) {},
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/users/5", nil)
	rec := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Handler().ServeHTTP(rec, req)
	}
}

// BenchmarkDispatch_NoMiddleware 基准：无中间件时的路由分发。
func BenchmarkDispatch_NoMiddleware(b *testing.B) {
	s := newTestServer()
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/users/%d", i)
		s.AddRoute(Route{
			Method: "GET", Path: path, Handler: func(w http.ResponseWriter, r *http.Request) {},
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/users/5", nil)
	rec := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Handler().ServeHTTP(rec, req)
	}
}

// --- 响应基准 ---

func BenchmarkOkJSON(b *testing.B) {
	w := newDiscardWriter()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		OkJSON(w, map[string]string{"name": "Alice"})
	}
}

func BenchmarkWriteJSON(b *testing.B) {
	w := newDiscardWriter()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WriteJSON(w, http.StatusOK, map[string]string{"name": "Alice"})
	}
}

func BenchmarkOkXML(b *testing.B) {
	w := newDiscardWriter()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		OkXML(w, xmlMessage{Name: "anyone"})
	}
}

func BenchmarkWriteHTML(b *testing.B) {
	w := newDiscardWriter()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WriteHTML(w, http.StatusOK, "<h1>Hello, World!</h1>")
	}
}

func BenchmarkWriteHTTPError(b *testing.B) {
	w := newDiscardWriter()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WriteHTTPError(w, http.StatusNotFound, "not found")
	}
}

func BenchmarkRedirect(b *testing.B) {
	w := newDiscardWriter()
	req := httptest.NewRequest(http.MethodGet, "/old", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Redirect(w, req, "/new", http.StatusFound)
	}
}

// --- 参数绑定基准 ---

type benchRequest struct {
	Name  string `json:"name" form:"name" binding:"required"`
	Age   int    `json:"age" form:"age" binding:"gte=0"`
	Email string `json:"email" form:"email" binding:"required,email"`
}

func BenchmarkBindJSON(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := bytes.NewBufferString(`{"name":"Alice","age":25,"email":"alice@example.com"}`)
		req := httptest.NewRequest(http.MethodPost, "/", body)
		req.Header.Set("Content-Type", MIMEJSON)

		var obj benchRequest
		if err := BindJSON(req, &obj); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBindQuery(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/?name=Alice&age=25&email=alice%40example.com", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var obj benchRequest
		if err := BindQuery(req, &obj); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBindForm(b *testing.B) {
	body := bytes.NewBufferString("name=Alice&age=25&email=alice%40example.com")
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", MIMEPOSTForm)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var obj benchRequest
		if err := BindForm(req, &obj); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBind_AutoDetect(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := bytes.NewBufferString(`{"name":"Alice","age":25,"email":"alice@example.com"}`)
		req := httptest.NewRequest(http.MethodPost, "/", body)
		req.Header.Set("Content-Type", MIMEJSON)

		var obj benchRequest
		if err := Bind(req, &obj); err != nil {
			b.Fatal(err)
		}
	}
}

// --- 中间件链基准 ---

// BenchmarkMiddlewareChain 基准：3 层全局中间件 + 路由分发。
func BenchmarkMiddlewareChain(b *testing.B) {
	s := newTestServer()
	mw := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r)
		}
	}
	s.Use(mw, mw, mw)
	s.AddRoute(Route{
		Method: "GET", Path: "/users", Handler: func(w http.ResponseWriter, r *http.Request) {},
	})

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	rec := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Handler().ServeHTTP(rec, req)
	}
}

// --- 静态文件基准 ---

func BenchmarkStaticFile(b *testing.B) {
	dir := b.TempDir()
	file := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(file, []byte("hello world"), 0o644); err != nil {
		b.Fatal(err)
	}

	s := newTestServer()
	s.AddRoute(StaticFile("/hello.txt", file))

	req := httptest.NewRequest(http.MethodGet, "/hello.txt", nil)
	rec := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Handler().ServeHTTP(rec, req)
	}
}

func BenchmarkStaticFS(b *testing.B) {
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("var x = 1;"), 0o644); err != nil {
		b.Fatal(err)
	}

	s := newTestServer()
	s.AddRoute(StaticFS("/static/", os.DirFS(dir)))

	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	rec := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Handler().ServeHTTP(rec, req)
	}
}

// --- 工具基准 ---

func BenchmarkWriteJSON_Response(b *testing.B) {
	resp := Response[any]{Code: CodeOK, Msg: MsgOK, Data: map[string]string{"name": "Alice"}}
	w := newDiscardWriter()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WriteJSON(w, http.StatusOK, resp)
	}
}

func BenchmarkNewUploadRequest(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildBenchMultipart("hello world")
	}
}

// buildBenchMultipart 构造 multipart 请求体（用于 Multipart 基准）。
func buildBenchMultipart(content string) *http.Request {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.txt")
	_, _ = part.Write([]byte(content))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}
