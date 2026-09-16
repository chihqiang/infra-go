package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chihqiang/infra-go/hash"
	"github.com/stretchr/testify/assert"
)

// Covers content_security.go: the content security verification middleware
// (tamper-proof + replay-proof).

// bodySHA256Hex computes the hex SHA-256 digest of a request body (used to build a signature).
func bodySHA256Hex(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// signedRequest builds a request carrying a valid signature.
func signedRequest(t *testing.T, key []byte, method, path, body string, ts int64) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	// Signed content: timestamp\nmethod\npath\nquery\nbodySha256Hex
	signContent := fmt.Sprintf("%d\n%s\n%s\n%s\n%s",
		ts, method, "/"+strings.TrimPrefix(path, "/"), "", bodySHA256Hex(body))
	signature := hash.HMACSign(key, signContent)
	req.Header.Set(ContentSecurityHeader, fmt.Sprintf("time=%d; signature=%s", ts, signature))
	return req
}

func TestContentSecurity_Valid(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	req := signedRequest(t, testKey, http.MethodPost, "/data", `{"a":1}`, time.Now().Unix())
	rec := perform(NewContentSecurity(testKey, 5*time.Minute).Middleware(), ok, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestContentSecurity_InvalidSignature(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// Signed with the wrong key → 401
	req := signedRequest(t, []byte("wrong-key-1234567"), http.MethodPost, "/data", "x", time.Now().Unix())
	rec := perform(NewContentSecurity(testKey, 5*time.Minute).Middleware(), ok, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestContentSecurity_Expired(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// Timestamp 10 minutes in the past (outside the 5 minute tolerance) → 401 replay protection.
	//
	// 401 rather than 403: invalid credentials (an expired timestamp here) is the
	// RFC 9110 §15.5.2 condition "lacks valid authentication credentials", whereas 403 means
	// "authenticated but not permitted" — a different meaning. The other failure paths of this
	// middleware also use 401.
	req := signedRequest(t, testKey, http.MethodPost, "/data", "x", time.Now().Add(-10*time.Minute).Unix())
	rec := perform(NewContentSecurity(testKey, 5*time.Minute).Middleware(), ok, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestContentSecurity_MissingHeader(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	req := httptest.NewRequest(http.MethodPost, "/data", nil)
	rec := perform(NewContentSecurity(testKey, 5*time.Minute).Middleware(), ok, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestContentSecurity_InvalidTimestamp(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// non-numeric timestamp → 401
	req := signedRequest(t, testKey, http.MethodPost, "/data", "x", time.Now().Unix())
	req.Header.Set(ContentSecurityHeader, "time=abc; signature=xyz")
	rec := perform(NewContentSecurity(testKey, 5*time.Minute).Middleware(), ok, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
