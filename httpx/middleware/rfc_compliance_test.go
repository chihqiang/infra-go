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

// 本文件验证错误响应的状态码与响应头符合 HTTP 规范：
//
//   - RFC 9110 §15.5.2：401 响应 MUST 携带 WWW-Authenticate
//   - RFC 6750 §3：Bearer 质询的 error 参数取值
//   - RFC 9110 §10.2.3：Retry-After 取值规则（delay-seconds）
//   - RFC 9110 §15.6.4：503 SHOULD 携带 Retry-After
//   - RFC 6585 §4：429 MAY 携带 Retry-After

// --- Challenge 渲染 ---

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

// TestChallenge_EscapesCRLF 验证头值中的 CR/LF 与控制字符被剔除，
// 避免响应头注入（RFC 9110 §11.2 的 quoted-string 不允许控制字符）。
func TestChallenge_EscapesCRLF(t *testing.T) {
	c := Challenge{Scheme: "Bearer", Realm: "ap\r\ni", Error: "bad\nvalue"}

	got := c.String()
	assert.NotContains(t, got, "\r")
	assert.NotContains(t, got, "\n")

	// 通过真实响应写入，确认 net/http 不会因非法头值而异常
	rec := httptest.NewRecorder()
	WriteUnauthorized(context.Background(), rec, c, "x")
	assert.NotContains(t, rec.Header().Get(HeaderWWWAuthenticate), "\n")
}

// TestChallenge_EscapesQuotes 验证内嵌引号被反斜杠转义（quoted-string 规则）。
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

// --- 401 必须带 WWW-Authenticate ---

// TestWriteUnauthorized_SetsHeader 验证 WriteUnauthorized 写入规范要求的头。
func TestWriteUnauthorized_SetsHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteUnauthorized(context.Background(), rec,
		Challenge{Scheme: "Bearer", Error: BearerErrorInvalidToken}, "invalid token")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, `Bearer error="invalid_token"`, rec.Header().Get(HeaderWWWAuthenticate))
}

// TestContentSecurity_AllUnauthorizedCarryChallenge 回归测试：content_security 的
// 每条 401 路径都必须带 WWW-Authenticate。
//
// 历史缺陷：仅返回状态码与消息，未提供质询，违反 RFC 9110 §15.5.2 的 MUST。
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

// --- Retry-After 集成 ---

// TestMaxConns_ServiceUnavailableCarriesRetryAfter 验证并发超限的 503 带 Retry-After。
// RFC 9110 §15.6.4：服务器因过载返回 503 时 SHOULD 给出该提示。
func TestMaxConns_ServiceUnavailableCarriesRetryAfter(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	mw := NewMaxConns(1)
	// 占满唯一的信号量
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

// TestRateLimit_TooManyRequestsCarriesRetryAfter 验证限流的 429 带 Retry-After。
// RFC 6585 §4 允许携带；客户端退避普遍依赖该头。
func TestRateLimit_TooManyRequestsCarriesRetryAfter(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	// 自定义限流器实现 RetryAfterProvider → 应使用其精确值
	lim := &retryAfterLimiter{after: 2500 * time.Millisecond}
	rec := perform(NewRateLimit(lim).Middleware(), ok, httptest.NewRequest(http.MethodGet, "/x", nil))

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	// 2.5s 向上取整为 3 秒
	assert.Equal(t, "3", rec.Header().Get(HeaderRetryAfter))
}

// TestRateLimit_RetryAfterFallback 验证限流器未实现 RetryAfterProvider 时
// 回退到 WithRetryAfter 配置值。
func TestRateLimit_RetryAfterFallback(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	lim := &plainDenyLimiter{}
	mw := NewRateLimit(lim).WithRetryAfter(5 * time.Second)
	rec := perform(mw.Middleware(), ok, httptest.NewRequest(http.MethodGet, "/x", nil))

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "5", rec.Header().Get(HeaderRetryAfter))
}

// TestRateLimit_RetryAfterOmittedWhenUnknown 验证无法估计时**不**发送该头。
// 与其给出编造的时长（客户端可能据此长时间不重试），不如省略。
func TestRateLimit_RetryAfterOmittedWhenUnknown(t *testing.T) {
	silenceLogger(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	lim := &plainDenyLimiter{}
	rec := perform(NewRateLimit(lim).Middleware(), ok, httptest.NewRequest(http.MethodGet, "/x", nil))

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Empty(t, rec.Header().Get(HeaderRetryAfter),
		"an unknown retry interval must be omitted rather than fabricated")
}

// TestRateLimit_RetryAfterIgnoresZeroFromProvider 验证 provider 返回 0 时
// 继续回退到配置值，而不是发送 Retry-After: 0。
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

// --- 测试用限流器 ---

// plainDenyLimiter 始终拒绝且不实现 RetryAfterProvider。
type plainDenyLimiter struct{}

func (plainDenyLimiter) Allow() bool { return false }
func (plainDenyLimiter) AllowContext(context.Context) (bool, error) {
	return false, nil
}

// retryAfterLimiter 始终拒绝，并实现 RetryAfterProvider。
type retryAfterLimiter struct{ after time.Duration }

func (l *retryAfterLimiter) Allow() bool { return false }
func (l *retryAfterLimiter) AllowContext(context.Context) (bool, error) {
	return false, nil
}
func (l *retryAfterLimiter) RetryAfter() time.Duration { return l.after }

// TestRetryAfterProvider_IsOptional 验证未实现该接口时不会误判。
func TestRetryAfterProvider_IsOptional(t *testing.T) {
	var lim RateLimiter = plainDenyLimiter{}
	_, ok := lim.(RetryAfterProvider)
	assert.False(t, ok, "plain limiter must not satisfy RetryAfterProvider")
}
