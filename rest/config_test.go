package rest

import (
	"testing"
	"time"

	"github.com/chihqiang/infra-go/core/conf"
	resthandler "github.com/chihqiang/infra-go/rest/handler"
	"github.com/stretchr/testify/require"
)

func TestConfigLoadFromYAML(t *testing.T) {
	const yamlConfig = `
host: 127.0.0.1
port: 9090
logger:
  serviceName: api
  mode: file
  level: debug
health:
  enabled: true
  path: healthz
middlewares:
  recovery:
    enabled: false
  requestID:
    enabled: false
  log:
    enabled: true
    skipPaths: ["/healthz"]
  timeout:
    enabled: true
    timeout: 3s
  maxBytes:
    enabled: true
    limit: 2048
`

	var cfg Config
	require.NoError(t, conf.LoadFromYAMLBytes([]byte(yamlConfig), &cfg))
	require.NoError(t, cfg.Validate())

	require.Equal(t, "127.0.0.1", cfg.Host)
	require.Equal(t, 9090, cfg.Port)
	require.Equal(t, "api", cfg.Logger.ServiceName)
	require.Equal(t, "file", cfg.Logger.Mode)
	require.Equal(t, "debug", cfg.Logger.Level)
	require.Equal(t, "logs", cfg.Logger.Path)
	require.Equal(t, "json", cfg.Logger.Encoding)
	require.True(t, cfg.Health.Enabled)
	require.Equal(t, "healthz", cfg.Health.Path)
	require.False(t, cfg.Middlewares.Recovery.Enabled)
	require.False(t, cfg.Middlewares.RequestID.Enabled)
	require.Equal(t, []string{"/healthz"}, cfg.Middlewares.Log.SkipPaths)
	require.True(t, cfg.Middlewares.Timeout.Enabled)
	require.Equal(t, 3*time.Second, cfg.Middlewares.Timeout.Timeout)
	require.Equal(t, int64(2048), cfg.Middlewares.MaxBytes.Limit)
}

// TestConfigLoadFromYAMLDefaultSections pins the section semantics: logger,
// middlewares and health are all optional, so a section that is not written
// stays zero and its nested `default` tags only apply once the section exists.
func TestConfigLoadFromYAMLDefaultSections(t *testing.T) {
	var cfg Config
	require.NoError(t, conf.LoadFromYAMLBytes([]byte("port: 8080\n"), &cfg))
	require.NoError(t, cfg.Validate())

	require.Equal(t, 8080, cfg.Port)
	require.Empty(t, cfg.Logger.Mode)
	require.Empty(t, cfg.Logger.Path)
	require.Empty(t, cfg.Logger.Level)
	require.Zero(t, cfg.Middlewares)
	require.False(t, cfg.Health.Enabled)
	require.Empty(t, cfg.Health.Path)
}

// TestConfigLoadFromYAMLEmptyMiddlewareEntry documents what an empty entry
// means: `recovery: {}` is present, so its own defaults apply, while entries
// that are not listed stay disabled.
func TestConfigLoadFromYAMLEmptyMiddlewareEntry(t *testing.T) {
	var cfg Config
	require.NoError(t, conf.LoadFromYAMLBytes([]byte("middlewares:\n  recovery: {}\n"), &cfg))

	require.True(t, cfg.Middlewares.Recovery.Enabled)
	require.False(t, cfg.Middlewares.RequestID.Enabled)
	require.False(t, cfg.Middlewares.Log.Enabled)
	require.False(t, cfg.Middlewares.MaxBytes.Enabled)
}

// TestConfigLoadFromYAMLNullSections covers a section key written with nothing
// under it (`middlewares:` with every entry commented out): it must behave like
// an omitted section instead of failing as a nil value.
func TestConfigLoadFromYAMLNullSections(t *testing.T) {
	var cfg Config
	require.NoError(t, conf.LoadFromYAMLBytes([]byte("logger:\nmiddlewares:\nhealth:\n"), &cfg))
	require.NoError(t, cfg.Validate())

	require.Empty(t, cfg.Logger.Mode)
	require.Zero(t, cfg.Middlewares)
	require.False(t, cfg.Health.Enabled)
}

// TestConfigLoadFromYAMLHealthDefaultPath pins the health path default: the
// section only needs `enabled: true`, and path falls back to /health.
func TestConfigLoadFromYAMLHealthDefaultPath(t *testing.T) {
	var cfg Config
	require.NoError(t, conf.LoadFromYAMLBytes([]byte("health:\n  enabled: true\n"), &cfg))
	require.NoError(t, cfg.Validate())

	require.True(t, cfg.Health.Enabled)
	require.Equal(t, "/health", cfg.Health.Path)
}

// TestConfigLoadFromYAMLWithoutRuntimeMiddleware pins the common case: entries
// whose handler needs a runtime dependency (auth, rateLimit, breaker, signature)
// can be left out of the file entirely. They stay disabled and Validate does not
// ask for their parser/limiter/breaker/key.
func TestConfigLoadFromYAMLWithoutRuntimeMiddleware(t *testing.T) {
	const yamlConfig = `
middlewares:
  recovery: {}
  log:
    skipPaths: ["/healthz"]
`
	var cfg Config
	require.NoError(t, conf.LoadFromYAMLBytes([]byte(yamlConfig), &cfg))
	require.NoError(t, cfg.Validate())

	require.True(t, cfg.Middlewares.Recovery.Enabled)
	require.True(t, cfg.Middlewares.Log.Enabled)
	require.False(t, cfg.Middlewares.Signature.Enabled)
	require.Nil(t, cfg.Middlewares.Signature.Keys)
	require.False(t, cfg.Middlewares.Auth.Enabled)
	require.Nil(t, cfg.Middlewares.Auth.Parser)
	require.False(t, cfg.Middlewares.RateLimit.Enabled)
	require.False(t, cfg.Middlewares.Breaker.Enabled)
}

func TestDefaultConfigValues(t *testing.T) {
	conf := DefaultConfig()

	if conf.Port != 8080 || conf.MaxHeaderBytes == 0 || conf.Middlewares.MaxBytes.Limit == 0 {
		t.Fatalf("unexpected defaults: %+v", conf)
	}
	if !conf.Middlewares.Recovery.Enabled || !conf.Middlewares.RequestID.Enabled || !conf.Middlewares.Log.Enabled {
		t.Fatalf("default middleware switches off: %+v", conf.Middlewares)
	}
	if conf.Middlewares.Trace.Enabled || conf.Middlewares.Gunzip.Enabled ||
		conf.Middlewares.Timeout.Enabled || conf.Middlewares.MaxConns.Enabled {
		t.Fatalf("optional middleware enabled by default: %+v", conf.Middlewares)
	}
	if !conf.Middlewares.MaxBytes.Enabled || conf.Middlewares.MaxBytes.Limit <= 0 {
		t.Fatalf("default body limit off: %+v", conf.Middlewares.MaxBytes)
	}
	if err := conf.Validate(); err != nil {
		t.Fatalf("DefaultConfig failed validation: %v", err)
	}
}

func TestConfigValidateRejectsBadValues(t *testing.T) {
	negativeBody := DefaultConfig()
	negativeBody.Middlewares.MaxBytes.Limit = -1

	negativeConns := DefaultConfig()
	negativeConns.Middlewares.MaxConns = resthandler.MaxConnsConfig{Enabled: true, Limit: -1}

	negativeMiddlewareTimeout := DefaultConfig()
	negativeMiddlewareTimeout.Middlewares.Timeout = resthandler.TimeoutConfig{Enabled: true, Timeout: -time.Second}

	authWithoutParser := DefaultConfig()
	authWithoutParser.Middlewares.Auth = resthandler.AuthConfig{Enabled: true}

	rateLimitWithoutLimiter := DefaultConfig()
	rateLimitWithoutLimiter.Middlewares.RateLimit = resthandler.RateLimitConfig{Enabled: true}

	breakerWithoutBreaker := DefaultConfig()
	breakerWithoutBreaker.Middlewares.Breaker = resthandler.BreakerConfig{Enabled: true}

	signatureWithoutKeys := DefaultConfig()
	signatureWithoutKeys.Middlewares.Signature = resthandler.SignatureConfig{Enabled: true}

	negativeSignatureBody := DefaultConfig()
	negativeSignatureBody.Middlewares.Signature = resthandler.SignatureConfig{
		Enabled: true,
		Keys:    [][]byte{[]byte("signing-key")},
		MaxBody: -1,
	}

	tests := []struct {
		name string
		conf Config
	}{
		{name: "port too high", conf: Config{Port: 70000}},
		{name: "half tls", conf: Config{Port: 80, CertFile: "cert.pem"}},
		{name: "negative body limit", conf: negativeBody},
		{name: "negative max conns", conf: negativeConns},
		{name: "negative middleware timeout", conf: negativeMiddlewareTimeout},
		{name: "auth without parser", conf: authWithoutParser},
		{name: "rate limit without limiter", conf: rateLimitWithoutLimiter},
		{name: "breaker without breaker", conf: breakerWithoutBreaker},
		{name: "signature without keys", conf: signatureWithoutKeys},
		{name: "negative signature body limit", conf: negativeSignatureBody},
		{name: "negative timeout", conf: Config{Port: 80, ReadTimeout: -time.Second}},
	}

	for _, test := range tests {
		if err := test.conf.Validate(); err == nil {
			t.Errorf("%s: Validate() = nil, want error", test.name)
		}
	}
}

func TestConfigValidateTrustedProxies(t *testing.T) {
	valid := DefaultConfig()
	valid.Middlewares.Log.TrustedProxies = []string{"127.0.0.1", "10.0.0.0/8", "2001:db8::/32"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid trusted proxies rejected: %v", err)
	}

	invalid := DefaultConfig()
	invalid.Middlewares.Log.TrustedProxies = []string{"not-an-ip"}
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid trusted proxy should fail validation")
	}
}

// TestConfigValidateIgnoresDisabledMiddlewareSettings makes the contract
// explicit: only enabled middleware takes part in validation, so a disabled
// feature can keep placeholder or inherited values.
func TestConfigValidateIgnoresDisabledMiddlewareSettings(t *testing.T) {
	conf := DefaultConfig()
	conf.Middlewares.Log = resthandler.LogConfig{Enabled: false, TrustedProxies: []string{"not-an-ip"}}
	conf.Middlewares.MaxConns = resthandler.MaxConnsConfig{Enabled: false, Limit: -1}
	conf.Middlewares.Timeout = resthandler.TimeoutConfig{Enabled: false, Timeout: -time.Second}
	conf.Middlewares.MaxBytes = resthandler.MaxBytesConfig{Enabled: false, Limit: -1}
	conf.Middlewares.Auth = resthandler.AuthConfig{Enabled: false}
	conf.Middlewares.RateLimit = resthandler.RateLimitConfig{Enabled: false}
	conf.Middlewares.Breaker = resthandler.BreakerConfig{Enabled: false}
	conf.Middlewares.Signature = resthandler.SignatureConfig{Enabled: false, MaxBody: -1}

	if err := conf.Validate(); err != nil {
		t.Fatalf("disabled middleware settings must not be validated: %v", err)
	}
}
