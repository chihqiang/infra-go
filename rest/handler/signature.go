package handler

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/chihqiang/infra-go/core/hash"
	"github.com/chihqiang/infra-go/rest/chain"
)

const (
	// HeaderSignature carries the HMAC signature of "timestamp.body".
	HeaderSignature = "X-Signature"
	// HeaderTimestamp carries the unix timestamp that was signed.
	HeaderTimestamp = "X-Signature-Timestamp"
)

// SignatureConfig configures SignatureHandler.
type SignatureConfig struct {
	// Keys holds the accepted HMAC keys; several keys allow key rotation.
	Keys [][]byte
	// TTL bounds the accepted clock skew of the signed timestamp. Zero disables
	// the freshness check.
	TTL time.Duration
	// MaxBody caps the body size read for verification. Zero means unlimited;
	// keep MaxBytesHandler in front of this handler to bound memory use.
	MaxBody int64
	// Now overrides the clock, mainly for tests.
	Now func() time.Time
}

// SignatureHandler rejects requests whose body is not signed with one of the
// configured HMAC keys.
func SignatureHandler(cfg SignatureConfig) chain.Middleware {
	if len(cfg.Keys) == 0 {
		panic("handler: SignatureHandler requires at least one key")
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			body, err := readSignedBody(r, cfg.MaxBody)
			if err != nil {
				writeStatusJSON(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			timestamp, err := signedTimestamp(r, cfg)
			if err != nil {
				writeStatusJSON(w, http.StatusUnauthorized, err.Error())
				return
			}

			payload := fmt.Sprintf("%d.%s", timestamp, body)
			signature := r.Header.Get(HeaderSignature)
			for _, key := range cfg.Keys {
				if hash.HMACVerify(key, payload, signature) {
					next(w, r)
					return
				}
			}

			writeStatusJSON(w, http.StatusUnauthorized, "invalid signature")
		}
	}
}

var (
	errBodyTooLarge     = errors.New("handler: signed body exceeds the limit")
	errMissingTimestamp = errors.New("missing signature timestamp")
	errInvalidTimestamp = errors.New("invalid signature timestamp")
	errStaleTimestamp   = errors.New("signature timestamp expired")
)

// Sign returns the signature value a client must send for body.
func Sign(key []byte, timestamp int64, body []byte) string {
	return hash.HMACSign(key, fmt.Sprintf("%d.%s", timestamp, body))
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
