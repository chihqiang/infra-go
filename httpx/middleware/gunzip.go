package middleware

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// Gunzip 是 gzip 请求体自动解压中间件。
// 请求头 Content-Encoding 含 "gzip" 时，将请求体包装为 gzip 读取器；
// 解压失败返回 400 Bad Request。
type Gunzip struct{}

// NewGunzip 创建 gzip 解压中间件。
func NewGunzip() *Gunzip {
	return &Gunzip{}
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的 gzip 解压中间件。
func (g *Gunzip) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.Header.Get("Content-Encoding"), "gzip") {
				reader, err := gzip.NewReader(r.Body)
				if err != nil {
					writeError(r.Context(), w, http.StatusBadRequest, "invalid gzip body")
					return
				}
				defer reader.Close()
				r.Body = reader
			}
			next.ServeHTTP(w, r)
		})
	}
}
