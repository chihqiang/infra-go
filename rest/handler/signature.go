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

// SignatureConfig configures SignatureHandler.
type SignatureConfig struct {
	Enabled bool `json:",optional"`
	// Keys holds the accepted HMAC keys; several keys allow key rotation.
	Keys [][]byte `json:",optional"`
	// TTL bounds the accepted clock skew of the signed timestamp.
	// Zero uses the secure default (5 minutes); a negative value explicitly
	// disables the freshness check (not recommended: captured requests can be
	// replayed forever).
	TTL time.Duration `json:",optional"`
	// MaxBody caps the body size read for verification. Zero means unlimited;
	// keep a MaxBytesHandler in front of this handler to bound memory use.
	MaxBody int64 `json:",optional"`
	// Now overrides the clock, mainly for tests.
	Now func() time.Time `json:"-"`
}

// SignatureHandler rejects requests whose body is not HMAC signed.
type SignatureHandler struct {
	config SignatureConfig
}

// defaultSignatureTTL is the freshness window applied when TTL is zero, so
// enabling the middleware never silently disables replay protection.
const defaultSignatureTTL = 5 * time.Minute

// NewSignatureHandler creates a SignatureHandler. It panics when enabled without
// any keys, since no request could ever be verified. A zero TTL is replaced by the
// secure default (5 minutes); use a negative TTL to disable the check explicitly.
func NewSignatureHandler(config SignatureConfig) *SignatureHandler {
	if config.Enabled && len(config.Keys) == 0 {
		panic("handler: SignatureHandler requires at least one key")
	}
	if config.TTL == 0 {
		config.TTL = defaultSignatureTTL
	}
	return &SignatureHandler{config: config}
}

// Handle wraps next with signature verification.
func (h *SignatureHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Validate the timestamp before reading the body: a stale or missing
		// timestamp must not cost a full body read.
		timestamp, err := signedTimestamp(r, h.config)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		body, err := readSignedBody(r, h.config.MaxBody)
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
		for _, key := range h.config.Keys {
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

func signedTimestamp(r *http.Request, cfg SignatureConfig) (int64, error) {
	raw := r.Header.Get(HeaderTimestamp)
	if raw == "" {
		return 0, errMissingTimestamp
	}

	timestamp, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, errInvalidTimestamp
	}
	if cfg.TTL <= 0 {
		return timestamp, nil
	}

	now := time.Now
	if cfg.Now != nil {
		now = cfg.Now
	}
	skew := now().Sub(time.Unix(timestamp, 0))
	if skew < 0 {
		skew = -skew
	}
	if skew > cfg.TTL {
		return 0, errStaleTimestamp
	}
	return timestamp, nil
}
