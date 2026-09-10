package respw

import (
	"bytes"
	"net/http"
)

// CryptionWriter 缓冲 handler 写入的响应，便于整体加密后输出。
// 供 httpx.WithCryption 中间件使用：handler 的响应先写入内存缓冲，
// handler 结束后由中间件统一 AES-GCM 加密后输出。
// maxBufBytes 限制缓冲上限；一旦超出（Overflowed 为 true），
// 会立即切换为"明文直写底层"模式：先原样写出已缓冲的内容，
// 之后所有写入直接透传，避免响应被截断（数据损坏）。
type CryptionWriter struct {
	http.ResponseWriter
	buf         bytes.Buffer
	code        int  // 记录状态码；缓冲模式下不透传，加密完成后统一写
	overflowed  bool // 缓冲是否超限（超限后进入明文透传模式）
	wroteHeader bool // 是否已把状态码写入底层（透传模式下仅写一次）
	maxBufBytes int  // 最大缓冲字节数
}

// NewCryptionWriter 创建一个响应加密缓冲包装器。
// maxBufBytes 为缓冲上限，<=0 表示不限制。
func NewCryptionWriter(w http.ResponseWriter, maxBufBytes int) *CryptionWriter {
	return &CryptionWriter{ResponseWriter: w, maxBufBytes: maxBufBytes}
}

// Overflowed 返回缓冲是否已超限。
// 超限后写入已切换为明文直写底层，中间件不应再重复输出缓冲内容。
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
//
// 超过 maxBufBytes 时切换为明文透传模式：先把已缓冲内容原样写入底层，
// 再直写本次及后续写入，保证客户端拿到完整响应。
// （历史缺陷：超限后仅保留并输出截断的缓冲前缀，响应体静默丢失后半部分。）
func (w *CryptionWriter) Write(p []byte) (int, error) {
	if w.overflowed {
		return w.ResponseWriter.Write(p)
	}
	if w.maxBufBytes > 0 && w.buf.Len()+len(p) > w.maxBufBytes {
		w.overflowed = true
		w.writeHeaderToUnderlying()
		if w.buf.Len() > 0 {
			if _, err := w.ResponseWriter.Write(w.buf.Bytes()); err != nil {
				w.buf.Reset()
				return 0, err
			}
			w.buf.Reset()
		}
		return w.ResponseWriter.Write(p)
	}
	return w.buf.Write(p)
}

// WriteHeader 记录状态码，缓冲模式下延迟到加密/透传完成后统一写入底层；
// 已进入明文透传模式时同步写入底层。
func (w *CryptionWriter) WriteHeader(code int) {
	w.code = code
	if w.overflowed {
		w.writeHeaderToUnderlying()
	}
}

// writeHeaderToUnderlying 把记录的状态码写入底层 ResponseWriter（仅首次）。
// code 为 0（handler 未显式 WriteHeader）时不写，交由 net/http 隐式写 200。
func (w *CryptionWriter) writeHeaderToUnderlying() {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	if w.code != 0 {
		w.ResponseWriter.WriteHeader(w.code)
	}
}

// Flush 在明文透传模式下透传底层 Flush（大响应流式输出所需）；
// 缓冲模式下为空操作 —— 加密需拿到完整响应体才能统一输出，
// 提前透传会在数据未就绪时向客户端发出空响应，造成"半发送"状态。
func (w *CryptionWriter) Flush() {
	if !w.overflowed {
		return
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
