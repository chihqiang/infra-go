package respw

import (
	"bytes"
	"errors"
	"net/http"
)

// CryptionWriter 缓冲 handler 写入的响应，便于整体加密后输出。
// 供 httpx.WithCryption 中间件使用：handler 的响应先写入内存缓冲，
// handler 结束后由中间件统一 AES-GCM 加密后输出。
// maxBufBytes 限制缓冲上限，超出时 Write 返回错误并置 Overflowed，
// 中间件检测后回退为明文输出，避免 OOM。
type CryptionWriter struct {
	http.ResponseWriter
	buf         bytes.Buffer
	code        int  // 记录状态码；Write 时不透传，加密完成后统一写
	overflowed  bool // 缓冲是否超限
	maxBufBytes int  // 最大缓冲字节数
}

// NewCryptionWriter 创建一个响应加密缓冲包装器。
// maxBufBytes 为缓冲上限，<=0 表示不限制。
func NewCryptionWriter(w http.ResponseWriter, maxBufBytes int) *CryptionWriter {
	return &CryptionWriter{ResponseWriter: w, maxBufBytes: maxBufBytes}
}

// Overflowed 返回缓冲是否已超限。
func (w *CryptionWriter) Overflowed() bool { return w.overflowed }

// StatusCode 返回 handler 记录的状态码（未显式 WriteHeader 时为 0）。
func (w *CryptionWriter) StatusCode() int { return w.code }

// Buffered 返回已缓冲的响应体内容。
func (w *CryptionWriter) Buffered() []byte { return w.buf.Bytes() }

// Header 返回底层 ResponseWriter 的响应头。
func (w *CryptionWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

// Write 将响应体缓冲到内存，待 handler 结束后统一加密输出。
// 超过 maxBufBytes 时标记 overflowed 并拒绝继续写入，
// 中间件将回退为明文输出。
func (w *CryptionWriter) Write(p []byte) (int, error) {
	if w.overflowed {
		return 0, errors.New("respw: encrypted response buffer overflowed")
	}
	if w.maxBufBytes > 0 && w.buf.Len()+len(p) > w.maxBufBytes {
		w.overflowed = true
		return 0, errors.New("respw: encrypted response exceeds max buffer size")
	}
	return w.buf.Write(p)
}

// WriteHeader 记录状态码（延迟到加密完成后写入）。
func (w *CryptionWriter) WriteHeader(code int) {
	w.code = code
}

// Flush 底层支持 Flush 时透传（流式响应场景）。
func (w *CryptionWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
