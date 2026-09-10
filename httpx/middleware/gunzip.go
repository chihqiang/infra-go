package middleware

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
)

// defaultMaxDecompressedBytes 默认解压后请求体上限（5MB，与 Cryption 的上限一致）。
const defaultMaxDecompressedBytes = 5 << 20

// errDecompressedTooLarge 解压后超过上限。
var errDecompressedTooLarge = errors.New("middleware: decompressed body exceeds limit")

// Gunzip 是 gzip 请求体自动解压中间件。
// 请求头 Content-Encoding 含 "gzip" 时，将请求体包装为 gzip 读取器；
// 解压失败返回 400 Bad Request。
//
// 安全说明：gzip 可被用于"解压炸弹"——极小体积的压缩数据能展开为极大的内容。
// 若只限制压缩后的大小（MaxBytes 依赖 Content-Length），限额会被绕过。
// 本中间件对**解压后**的字节数设上限（默认 5MB），超限时下游读取会收到错误，
// 避免解压结果撑爆内存。
//
// 建议与 MaxBytes 组合使用，且本中间件注册在更内层，
// 由 MaxBytes 先限制压缩体、再由本中间件限制解压体，两者互补：
//
//	server.Use(httpx.WithMaxBytes(1<<20), httpx.WithGunzip())
type Gunzip struct {
	maxDecompressedBytes int64
}

// NewGunzip 创建 gzip 解压中间件。
// 解压后上限默认为 5MB，可用 WithMaxDecompressedBytes 调整。
func NewGunzip() *Gunzip {
	return &Gunzip{maxDecompressedBytes: defaultMaxDecompressedBytes}
}

// WithMaxDecompressedBytes 设置解压后的请求体上限（字节）。
// n <= 0 表示不限制（不推荐：会重新引入解压炸弹风险）。
func (g *Gunzip) WithMaxDecompressedBytes(n int64) *Gunzip {
	g.maxDecompressedBytes = n
	return g
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的 gzip 解压中间件。
func (g *Gunzip) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.Header.Get("Content-Encoding"), "gzip") {
				next.ServeHTTP(w, r)
				return
			}

			reader, err := gzip.NewReader(r.Body)
			if err != nil {
				writeError(r.Context(), w, http.StatusBadRequest, "invalid gzip body")
				return
			}

			r.Body = &limitedReadCloser{
				reader: reader,
				limit:  g.maxDecompressedBytes,
			}
			// 解压后长度未知，且与压缩体的 Content-Length 不一致，必须清除，
			// 否则下游（含 MaxBytes）会按压缩体长度误判。
			r.ContentLength = -1
			// 请求体已解压，标识不应继续向下游传递。
			r.Header.Del("Content-Encoding")

			next.ServeHTTP(w, r)
		})
	}
}

// limitedReadCloser 限制从底层 reader 读取的总字节数，超过 limit 后返回错误。
//
// 最多读取 limit+1 字节：多出的 1 字节用于区分"恰好等于上限"与"超过上限"。
// 一旦确认超限，已读到的越界字节会被丢弃（返回 0, err），
// 调用方（如 io.ReadAll）随即停止读取。
type limitedReadCloser struct {
	reader   io.ReadCloser
	limit    int64
	consumed int64
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	if l.limit > 0 {
		// 上限 +1 用于探测溢出
		remaining := l.limit + 1 - l.consumed
		if remaining <= 0 {
			return 0, errDecompressedTooLarge
		}
		if int64(len(p)) > remaining {
			p = p[:remaining]
		}
	}

	n, err := l.reader.Read(p)
	l.consumed += int64(n)

	if l.limit > 0 && l.consumed > l.limit {
		return 0, errDecompressedTooLarge
	}
	return n, err
}

// Close 关闭底层 gzip reader，同时释放其包装的原始 Body。
func (l *limitedReadCloser) Close() error {
	return l.reader.Close()
}
