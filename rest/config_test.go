package rest

import (
	"testing"
	"time"
)

func TestDefaultConfigValues(t *testing.T) {
	conf := DefaultConfig()

	if conf.Port != 8080 || conf.MaxBodyBytes == 0 || conf.MaxHeaderBytes == 0 {
		t.Fatalf("unexpected defaults: %+v", conf)
	}
	if !conf.Middlewares.Recovery || !conf.Middlewares.RequestID || !conf.Middlewares.Log {
		t.Fatalf("default middleware switches off: %+v", conf.Middlewares)
	}
	if conf.Middlewares.Trace || conf.Middlewares.Gunzip || conf.Middlewares.Timeout != 0 {
		t.Fatalf("optional middleware enabled by default: %+v", conf.Middlewares)
	}
	if err := conf.Validate(); err != nil {
		t.Fatalf("DefaultConfig failed validation: %v", err)
	}
}

func TestConfigValidateRejectsBadValues(t *testing.T) {
	tests := []struct {
		name string
		conf Config
	}{
		{name: "port too high", conf: Config{Port: 70000}},
		{name: "half tls", conf: Config{Port: 80, CertFile: "cert.pem"}},
		{name: "negative body limit", conf: Config{Port: 80, MaxBodyBytes: -1}},
		{name: "negative timeout", conf: Config{Port: 80, ReadTimeout: -time.Second}},
	}

	for _, test := range tests {
		if err := test.conf.Validate(); err == nil {
			t.Errorf("%s: Validate() = nil, want error", test.name)
		}
	}
}
