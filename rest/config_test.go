package rest

import (
	"testing"
	"time"

	resthandler "github.com/chihqiang/infra-go/rest/handler"
)

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
