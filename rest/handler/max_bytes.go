package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/chihqiang/infra-go/rest/chain"
)

// MaxBytesHandler limits the amount of request body data that can be read.
// A nonpositive limit disables the check.
func MaxBytesHandler(limit int64) chain.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		if limit <= 0 {
			return next
		}

		return func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				markConnectionClose(w)
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			if r.Body != nil {
				r.Body = &maxBytesBody{
					ReadCloser: http.MaxBytesReader(w, r.Body, limit),
					onLimit:    func() { markConnectionClose(w) },
				}
			}
			next(w, r)
		}
	}
}

// maxBytesBody observes the limit error returned by http.MaxBytesReader. The
// underlying reader calls net/http's unexported requestTooLarge hook only when
// the ResponseWriter is the server's concrete response; middleware writers hide
// it, so the header is set here instead.
type maxBytesBody struct {
	io.ReadCloser
	onLimit  func()
	notified bool
}

func (b *maxBytesBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if !b.notified {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			b.notified = true
			b.onLimit()
		}
	}
	return n, err
}

// markConnectionClose asks net/http to close the keep-alive connection after the
// reply: WriteHeader sets closeAfterReply when the response carries this header.
func markConnectionClose(w http.ResponseWriter) {
	if w.Header().Get("Connection") == "" {
		w.Header().Set("Connection", "close")
	}
}
