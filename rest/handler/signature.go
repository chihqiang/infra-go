package handler

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chihqiang/infra-go/core/hash"
)

const (
	// HeaderSignature carries the HMAC signature of "timestamp.body".
	HeaderSignature = "X-Signature"
	// HeaderTimestamp carries the unix timestamp that was signed.
	HeaderTimestamp = "X-Signature-Timestamp"
)

// SignatureOption customises SignatureHandler.
type SignatureOption func(*signatureOptions)

type signatureOptions struct {
	// ttl bounds the accepted clock skew of the signed timestamp. A negative
	// value explicitly disables the freshness check (captured requests could be
	// replayed forever).
	ttl time.Duration
	// maxBody caps the body size read for verification; zero means unlimited.
	// Keep a MaxBytesHandler in front of this handler to bound memory use.
	maxBody int64
	// now overrides the clock, mainly for tests.
	now func() time.Time
}

// WithSignatureTTL bounds the accepted clock skew of the signed timestamp. Zero
// uses the secure default (5 minutes); a negative value disables the freshness
// check entirely.
func WithSignatureTTL(ttl time.Duration) SignatureOption {
	return func(opts *signatureOptions) {
		opts.ttl = ttl
	}
}

// WithSignatureMaxBody caps the body size read for verification. Zero means
// unlimited; keep a MaxBytesHandler in front of this handler to bound memory.
// A negative value panics at construction.
func WithSignatureMaxBody(maxBody int64) SignatureOption {
	return func(opts *signatureOptions) {
		opts.maxBody = maxBody
	}
}

// withSignatureClock overrides the clock, mainly for tests.
func withSignatureClock(now func() time.Time) SignatureOption {
	return func(opts *signatureOptions) {
		opts.now = now
	}
}

// SignatureHandler rejects requests whose body is not HMAC signed.
type SignatureHandler struct {
	keys    [][]byte
	options signatureOptions
}

// defaultSignatureTTL is the freshness window applied when TTL is zero, so
// enabling the middleware never silently disables replay protection.
const defaultSignatureTTL = 5 * time.Minute

// NewSignatureHandler creates a SignatureHandler for the given HMAC keys;
// several keys allow rotation. It panics when no key is configured, since no
// request could ever be verified, and when a negative MaxBody is requested.
func NewSignatureHandler(keys [][]byte, opts ...SignatureOption) *SignatureHandler {
	if len(keys) == 0 {
		panic("handler: SignatureHandler requires at least one key")
	}
	var options signatureOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if options.maxBody < 0 {
		panic("handler: SignatureHandler max body must not be negative")
	}
	if options.ttl == 0 {
		options.ttl = defaultSignatureTTL
	}
	return &SignatureHandler{keys: keys, options: options}
}

// Handle wraps next with signature verification.
func (h *SignatureHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Validate the timestamp before reading the body: a stale or missing
		// timestamp must not cost a full body read.
		timestamp, err := signedTimestamp(r, h.options)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		body, err := readSignedBody(r, h.options.maxBody)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.Is(err, errBodyTooLarge) || errors.As(err, &maxBytesErr) {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		// The signature covers the request target as well as the body, so a captured
		// signature cannot be replayed against another endpoint that accepts the same
		// payload.
		payload := signaturePayload(r.Method, signaturePath(r), r.URL.RawQuery, timestamp, body)
		signature := r.Header.Get(HeaderSignature)
		for _, key := range h.keys {
			if hash.HMACVerify(key, payload, signature) {
				next(w, r)
				return
			}
		}

		w.WriteHeader(http.StatusUnauthorized)
	}
}

var (
	errBodyTooLarge     = errors.New("handler: signed body exceeds the limit")
	errMissingTimestamp = errors.New("missing signature timestamp")
	errInvalidTimestamp = errors.New("invalid signature timestamp")
	errStaleTimestamp   = errors.New("signature timestamp expired")
)

// Sign returns the signature value a client must send for a request.
//
// method, path and query must match what the server sees: path is the escaped URL
// path (r.URL.EscapedPath()) and query is the raw query string (r.URL.RawQuery).
// SignRequest is the convenience wrapper for an *http.Request.
func Sign(key []byte, timestamp int64, method, path, query string, body []byte) string {
	return hash.HMACSign(key, signaturePayload(method, path, query, timestamp, body))
}

// SignRequest signs an *http.Request using its method, escaped path and raw query.
func SignRequest(key []byte, timestamp int64, r *http.Request, body []byte) string {
	if r == nil || r.URL == nil {
		return Sign(key, timestamp, http.MethodGet, "/", "", body)
	}
	return Sign(key, timestamp, r.Method, signaturePath(r), r.URL.RawQuery, body)
}

// signaturePayload builds the canonical string covered by the HMAC:
//
//	METHOD \n escaped-path \n raw-query \n timestamp \n body
//
// The method/path/query prefix binds a signature to one endpoint; the body is
// last so it may contain arbitrary bytes.
func signaturePayload(method, path, query string, timestamp int64, body []byte) string {
	var buf bytes.Buffer
	buf.WriteString(strings.ToUpper(method))
	buf.WriteByte('\n')
	buf.WriteString(path)
	buf.WriteByte('\n')
	buf.WriteString(query)
	buf.WriteByte('\n')
	buf.WriteString(strconv.FormatInt(timestamp, 10))
	buf.WriteByte('\n')
	buf.Write(body)
	return buf.String()
}

// signaturePath returns the escaped path the server signed, defaulting to "/".
func signaturePath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "/"
	}
	if path := r.URL.EscapedPath(); path != "" {
		return path
	}
	return "/"
}

func readSignedBody(r *http.Request, maxBody int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}

	reader := io.Reader(r.Body)
	if maxBody > 0 {
		reader = io.LimitReader(r.Body, maxBody+1)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if maxBody > 0 && int64(len(body)) > maxBody {
		return nil, errBodyTooLarge
	}
	return body, nil
}

func signedTimestamp(r *http.Request, cfg signatureOptions) (int64, error) {
	raw := r.Header.Get(HeaderTimestamp)
	if raw == "" {
		return 0, errMissingTimestamp
	}

	timestamp, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, errInvalidTimestamp
	}
	if cfg.ttl <= 0 {
		return timestamp, nil
	}

	now := time.Now
	if cfg.now != nil {
		now = cfg.now
	}
	// Compare absolute times instead of now.Sub(timestamp): Sub saturates at the
	// duration limit, and negating math.MinInt64 overflows, so a timestamp far in
	// the future (> ~292 years) used to bypass the freshness check entirely.
	current := now()
	signedAt := time.Unix(timestamp, 0)
	if signedAt.Before(current.Add(-cfg.ttl)) || signedAt.After(current.Add(cfg.ttl)) {
		return 0, errStaleTimestamp
	}
	return timestamp, nil
}
