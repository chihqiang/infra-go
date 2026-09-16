package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file verifies that the status codes and response headers of error responses comply
// with the HTTP specifications:
//
//   - RFC 9110 §15.5.2: a 401 response MUST carry WWW-Authenticate
//   - RFC 6750 §3: allowed values of the Bearer challenge error parameter
//   - RFC 9110 §10.2.3: rules for Retry-After values (delay-seconds)
//   - RFC 9110 §15.6.4: a 503 SHOULD carry Retry-After
//   - RFC 6585 §4: a 429 MAY carry Retry-After

// --- Challenge rendering ---

func TestChallenge_String(t *testing.T) {
	tests := []struct {
		name string
		in   Challenge
		want string
	}{
		{"scheme only", Challenge{Scheme: "Bearer"}, "Bearer"},
		{"realm", Challenge{Scheme: "Bearer", Realm: "api"}, `Bearer realm="api"`},
		{
			"realm and error", Challenge{Scheme: "Bearer", Realm: "api", Error: BearerErrorInvalidToken},
			`Bearer realm="api", error="invalid_token"`,
		},
		{"error only", Challenge{Scheme: "Bearer", Error: BearerErrorInvalidRequest},
			`Bearer error="invalid_request"`},
		{"empty scheme yields empty value", Challenge{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.in.String())
		})
	}
}

// TestChallenge_EscapesCRLF verifies CR/LF and control characters are stripped from the
// header value, preventing response header injection (the RFC 9110 §11.2 quoted-string does
// not allow control characters).
func TestChallenge_EscapesCRLF(t *testing.T) {
	c := Challenge{Scheme: "Bearer", Realm: "ap\r\ni", Error: "bad\nvalue"}

	got := c.String()
	assert.NotContains(t, got, "\r")
	assert.NotContains(t, got, "\n")

	// write through a real response to confirm net/http does not choke on the illegal header value
	rec := httptest.NewRecorder()
	WriteUnauthorized(context.Background(), rec, c, "x")
	assert.NotContains(t, rec.Header().Get(HeaderWWWAuthenticate), "\n")
}

// TestChallenge_EscapesQuotes verifies embedded quotes are backslash-escaped (quoted-string rule).
func TestChallenge_EscapesQuotes(t *testing.T) {
	c := Challenge{Scheme: "Bearer", Realm: `a"b`}
	assert.Equal(t, `Bearer realm="a\"b"`, c.String())
}

// --- RetryAfterSeconds ---

func TestRetryAfterSeconds(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want int
	}{
		{"zero means omit", 0, 0},
		{"negative means omit", -time.Second, 0},
		{"sub-second rounds up to 1", 200 * time.Millisecond, 1},
		{"exactly one second", time.Second, 1},
		{"rounds up", 1100 * time.Millisecond, 2},
		{"millisecond floor", time.Millisecond, 1},
		{"many seconds", 90 * time.Second, 90},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, RetryAfterSeconds(tc.in))
		})
	}
}

// --- a 401 must carry WWW-Authenticate ---

// TestWriteUnauthorized_SetsHeader verifies WriteUnauthorized writes the headers the spec requires.
func TestWriteUnauthorized_SetsHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteUnauthorized(context.Background(), rec,
		Challenge{Scheme: "Bearer", Error: BearerErrorInvalidToken}, "invalid token")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, `Bearer error="invalid_token"`, rec.Header().Get(HeaderWWWAuthenticate))
}

// TestContentSecurity_AllUnauthorizedCarryChallenge is a regression test: every 401 path of
// content_security must carry WWW-Authenticate.
//
// Historical defect: it returned only a status code and message with no challenge, violating
// the MUST in RFC 9110 §15.5.2.
func TestContentSecurity_AllUnauthorizedCarryChallenge(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mw := NewContentSecurity(testKey, 5*time.Minute).Middleware()

	cases := []struct {
		name string
		req  *http.Request
	}{
		{
			"missing header",
			httptest.NewRequest(http.MethodPost, "/data", nil),
		},
		{
			"invalid timestamp",
			func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/data", nil)
				r.Header.Set(ContentSecurityHeader, "time=abc; signature=xx")
				return r
			}(),
		},
		{
			"expired timestamp",
			signedRequest(t, testKey, http.MethodPost, "/data", "x", time.Now().Add(-10*time.Minute).Unix()),
		},
		{
			"invalid signature",
			signedRequest(t, []byte("wrong-key-1234567"), http.MethodPost, "/data", "x", time.Now().Unix()),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := perform(mw, ok, tc.req)
			require.Equal(t, http.StatusUnauthorized, rec.Code)

			challenge := rec.Header().Get(HeaderWWWAuthenticate)
			require.NotEmpty(t, challenge,
				"401 MUST carry WWW-Authenticate (RFC 9110 §15.5.2)")
			assert.True(t, strings.HasPrefix(challenge, contentSecurityScheme),
				"challenge must name the scheme, got %q", challenge)
		})
	}
}

// --- Retry-After integration ---

// TestMaxConns_ServiceUnavailableCarriesRetryAfter verifies the 503 returned when the
// concurrency limit is exceeded carries Retry-After.
// RFC 9110 §15.6.4: a server returning 503 because of overload SHOULD give this hint.
func TestMaxConns_ServiceUnavailableCarriesRetryAfter(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	mw := NewMaxConns(1)
	// occupy the single semaphore slot
	block := make(chan struct{})
	hold := func(w http.ResponseWriter, r *http.Request) { <-block }
	go func() {
		mw.Middleware()(http.HandlerFunc(hold)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	}()
	time.Sleep(20 * time.Millisecond)

	rec := perform(mw.Middleware(), ok, httptest.NewRequest(http.MethodGet, "/x", nil))
	close(block)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "1", rec.Header().Get(HeaderRetryAfter),
		"503 SHOULD carry Retry-After (RFC 9110 §15.6.4)")
}

// TestRateLimit_TooManyRequestsCarriesRetryAfter verifies the 429 from rate limiting carries
// Retry-After.
// RFC 6585 §4 permits it; client backoff widely relies on this header.
func TestRateLimit_TooManyRequestsCarriesRetryAfter(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// a custom limiter implementing RetryAfterProvider -> its exact value must be used
	lim := &retryAfterLimiter{after: 2500 * time.Millisecond}
	rec := perform(NewRateLimit(lim).Middleware(), ok, httptest.NewRequest(http.MethodGet, "/x", nil))

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	// 2.5s rounds up to 3 seconds
	assert.Equal(t, "3", rec.Header().Get(HeaderRetryAfter))
}

// TestRateLimit_RetryAfterFallback verifies that a limiter not implementing RetryAfterProvider
// falls back to the WithRetryAfter configuration value.
func TestRateLimit_RetryAfterFallback(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	lim := &plainDenyLimiter{}
	mw := NewRateLimit(lim).WithRetryAfter(5 * time.Second)
	rec := perform(mw.Middleware(), ok, httptest.NewRequest(http.MethodGet, "/x", nil))

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "5", rec.Header().Get(HeaderRetryAfter))
}

// TestRateLimit_RetryAfterOmittedWhenUnknown verifies the header is **not** sent when the
// interval cannot be estimated.
// Better to omit it than to fabricate a duration the client might back off by for a long time.
func TestRateLimit_RetryAfterOmittedWhenUnknown(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	lim := &plainDenyLimiter{}
	rec := perform(NewRateLimit(lim).Middleware(), ok, httptest.NewRequest(http.MethodGet, "/x", nil))

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Empty(t, rec.Header().Get(HeaderRetryAfter),
		"an unknown retry interval must be omitted rather than fabricated")
}

// TestRateLimit_RetryAfterIgnoresZeroFromProvider verifies that a provider returning 0 falls
// back to the configured value rather than sending Retry-After: 0.
func TestRateLimit_RetryAfterIgnoresZeroFromProvider(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	lim := &retryAfterLimiter{after: 0}
	mw := NewRateLimit(lim).WithRetryAfter(3 * time.Second)
	rec := perform(mw.Middleware(), ok, httptest.NewRequest(http.MethodGet, "/x", nil))

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "3", rec.Header().Get(HeaderRetryAfter))
	assert.NotEqual(t, "0", rec.Header().Get(HeaderRetryAfter))
}

// --- limiters used by the tests ---

// plainDenyLimiter always denies and does not implement RetryAfterProvider.
type plainDenyLimiter struct{}

func (plainDenyLimiter) Allow() bool { return false }
func (plainDenyLimiter) AllowContext(context.Context) (bool, error) {
	return false, nil
}

// retryAfterLimiter always denies and implements RetryAfterProvider.
type retryAfterLimiter struct{ after time.Duration }

func (l *retryAfterLimiter) Allow() bool { return false }
func (l *retryAfterLimiter) AllowContext(context.Context) (bool, error) {
	return false, nil
}
func (l *retryAfterLimiter) RetryAfter() time.Duration { return l.after }

// TestRetryAfterProvider_IsOptional verifies there is no false positive when the interface is
// not implemented.
func TestRetryAfterProvider_IsOptional(t *testing.T) {
	var lim RateLimiter = plainDenyLimiter{}
	_, ok := lim.(RetryAfterProvider)
	assert.False(t, ok, "plain limiter must not satisfy RetryAfterProvider")
}
