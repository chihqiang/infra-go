package handler

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func signRequest(request *http.Request, key []byte, timestamp int64, body []byte) {
	request.Header.Set(HeaderTimestamp, strconv.FormatInt(timestamp, 10))
	request.Header.Set(HeaderSignature, SignRequest(key, timestamp, request, body))
}

func newSignatureHandler(config SignatureConfig) *SignatureHandler {
	config.Enabled = true
	return NewSignatureHandler(config)
}

func TestSignatureHandlerAcceptsSignedBody(t *testing.T) {
	key := []byte("signing-key")
	body := []byte(`{"amount":10}`)
	var received string
	handlerFn := newSignatureHandler(SignatureConfig{Keys: [][]byte{key}, TTL: time.Minute}).Handle(
		func(_ http.ResponseWriter, r *http.Request) {
			read, _ := io.ReadAll(r.Body)
			received = string(read)
		},
	)

	request := httptest.NewRequest(http.MethodPost, "/pay", bytes.NewReader(body))
	signRequest(request, key, time.Now().Unix(), body)
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if received != string(body) {
		t.Fatalf("handler body = %q, want %q", received, body)
	}
}

func TestSignatureHandlerRejectsBadSignature(t *testing.T) {
	key := []byte("signing-key")
	body := []byte(`{"amount":10}`)
	called := false
	handlerFn := newSignatureHandler(SignatureConfig{Keys: [][]byte{key}}).Handle(
		func(http.ResponseWriter, *http.Request) { called = true },
	)

	request := httptest.NewRequest(http.MethodPost, "/pay", bytes.NewReader(body))
	signRequest(request, []byte("other-key"), time.Now().Unix(), body)
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if called {
		t.Fatal("handler ran for an invalid signature")
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestSignatureHandlerRejectsTamperedBody(t *testing.T) {
	key := []byte("signing-key")
	original := []byte(`{"amount":10}`)
	handlerFn := newSignatureHandler(SignatureConfig{Keys: [][]byte{key}}).Handle(
		func(http.ResponseWriter, *http.Request) { t.Fatal("must not reach handler") },
	)

	request := httptest.NewRequest(http.MethodPost, "/pay", bytes.NewReader([]byte(`{"amount":1000}`)))
	signRequest(request, key, time.Now().Unix(), original)

	if recorder := httptest.NewRecorder(); func() int {
		handlerFn.ServeHTTP(recorder, request)
		return recorder.Code
	}() != http.StatusUnauthorized {
		t.Fatal("tampered body was accepted")
	}
}

func TestSignatureHandlerRejectsStaleTimestamp(t *testing.T) {
	key := []byte("signing-key")
	body := []byte("payload")
	now := time.Unix(1_700_000_000, 0)
	handlerFn := newSignatureHandler(SignatureConfig{
		Keys: [][]byte{key},
		TTL:  time.Minute,
		Now:  func() time.Time { return now },
	}).Handle(func(http.ResponseWriter, *http.Request) { t.Fatal("must not reach handler") })

	request := httptest.NewRequest(http.MethodPost, "/pay", bytes.NewReader(body))
	signRequest(request, key, now.Add(-2*time.Minute).Unix(), body)

	if recorder := httptest.NewRecorder(); func() int {
		handlerFn.ServeHTTP(recorder, request)
		return recorder.Code
	}() != http.StatusUnauthorized {
		t.Fatal("stale signature was accepted")
	}
}

func TestSignatureHandlerAcceptsRotatedKey(t *testing.T) {
	oldKey, newKey := []byte("old-key"), []byte("new-key")
	body := []byte("payload")
	handlerFn := newSignatureHandler(SignatureConfig{Keys: [][]byte{newKey, oldKey}}).Handle(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
	)

	request := httptest.NewRequest(http.MethodPost, "/pay", bytes.NewReader(body))
	signRequest(request, oldKey, time.Now().Unix(), body)
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
}

func TestSignatureHandlerEnforcesBodyLimit(t *testing.T) {
	key := []byte("signing-key")
	body := bytes.Repeat([]byte("a"), 32)
	handlerFn := newSignatureHandler(SignatureConfig{Keys: [][]byte{key}, MaxBody: 8}).Handle(
		func(http.ResponseWriter, *http.Request) { t.Fatal("must not reach handler") },
	)

	request := httptest.NewRequest(http.MethodPost, "/pay", bytes.NewReader(body))
	signRequest(request, key, time.Now().Unix(), body)
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", recorder.Code)
	}
}

func TestSignatureHandlerRequiresKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("SignatureHandler without keys should panic")
		}
	}()

	newSignatureHandler(SignatureConfig{})
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func (failingBody) Close() error { return nil }

func TestSignatureHandlerMapsReadErrorToBadRequest(t *testing.T) {
	handlerFn := newSignatureHandler(SignatureConfig{Keys: [][]byte{[]byte("signing-key")}}).Handle(
		func(http.ResponseWriter, *http.Request) {},
	)
	request := httptest.NewRequest(http.MethodPost, "/pay", nil)
	request.Body = failingBody{}
	request.Header.Set(HeaderTimestamp, strconv.FormatInt(time.Now().Unix(), 10))
	recorder := httptest.NewRecorder()

	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

type trackingBody struct {
	read bool
}

func (b *trackingBody) Read([]byte) (int, error) {
	b.read = true
	return 0, io.EOF
}

func (b *trackingBody) Close() error { return nil }

func TestSignatureHandlerChecksTimestampBeforeReadingBody(t *testing.T) {
	body := &trackingBody{}
	handlerFn := newSignatureHandler(SignatureConfig{
		Keys: [][]byte{[]byte("signing-key")},
		TTL:  time.Minute,
	}).Handle(func(http.ResponseWriter, *http.Request) {})

	request := httptest.NewRequest(http.MethodPost, "/pay", nil)
	request.Body = body
	request.Header.Set(HeaderTimestamp, strconv.FormatInt(time.Now().Add(-2*time.Minute).Unix(), 10))
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if body.read {
		t.Fatal("stale timestamp should be rejected before reading the body")
	}
}

// TestSignatureHandlerRejectsReplayOnAnotherPath is a regression test: the signed
// payload used to be only "timestamp.body", so a captured request could be
// replayed against any endpoint that accepts the same body.
func TestSignatureHandlerRejectsReplayOnAnotherPath(t *testing.T) {
	key := []byte("signing-key")
	body := []byte(`{"amount":10}`)
	now := time.Now().Unix()

	handlerFn := newSignatureHandler(SignatureConfig{Keys: [][]byte{key}, TTL: time.Minute}).Handle(
		func(http.ResponseWriter, *http.Request) { t.Fatal("replayed request reached the handler") },
	)

	// The captured request was signed for /pay.
	signed := httptest.NewRequest(http.MethodPost, "/pay", bytes.NewReader(body))
	signRequest(signed, key, now, body)
	timestamp := signed.Header.Get(HeaderTimestamp)
	signature := signed.Header.Get(HeaderSignature)

	// Replaying the same body, timestamp and signature on /refund must fail.
	replay := httptest.NewRequest(http.MethodPost, "/refund", bytes.NewReader(body))
	replay.Header.Set(HeaderTimestamp, timestamp)
	replay.Header.Set(HeaderSignature, signature)

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, replay)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a cross-endpoint replay", recorder.Code)
	}
}

// TestSignatureHandlerDefaultTTLRejectsStale proves that enabling the middleware
// without setting TTL no longer disables the freshness check.
func TestSignatureHandlerDefaultTTLRejectsStale(t *testing.T) {
	key := []byte("signing-key")
	body := []byte("payload")
	now := time.Unix(1_700_000_000, 0)
	handlerFn := newSignatureHandler(SignatureConfig{
		Keys: [][]byte{key},
		Now:  func() time.Time { return now },
	}).Handle(func(http.ResponseWriter, *http.Request) { t.Fatal("stale request reached the handler") })

	request := httptest.NewRequest(http.MethodPost, "/pay", bytes.NewReader(body))
	signRequest(request, key, now.Add(-10*time.Minute).Unix(), body)

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a timestamp older than the default TTL", recorder.Code)
	}
}

// TestSignatureHandlerNegativeTTLDisablesFreshness documents the explicit opt-out.
func TestSignatureHandlerNegativeTTLDisablesFreshness(t *testing.T) {
	key := []byte("signing-key")
	body := []byte("payload")
	now := time.Unix(1_700_000_000, 0)
	handlerFn := newSignatureHandler(SignatureConfig{
		Keys: [][]byte{key},
		TTL:  -1,
		Now:  func() time.Time { return now },
	}).Handle(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	request := httptest.NewRequest(http.MethodPost, "/pay", bytes.NewReader(body))
	signRequest(request, key, now.Add(-24*time.Hour).Unix(), body)

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 with freshness explicitly disabled", recorder.Code)
	}
}

// TestSignRequestBindsMethodPathAndQuery checks the public helper covers the whole
// request target.
func TestSignRequestBindsMethodPathAndQuery(t *testing.T) {
	key := []byte("signing-key")
	body := []byte("same")
	ts := int64(1_700_000_000)

	base := httptest.NewRequest(http.MethodPost, "/pay?x=1", bytes.NewReader(body))
	signature := SignRequest(key, ts, base, body)

	otherMethod := httptest.NewRequest(http.MethodPut, "/pay?x=1", bytes.NewReader(body))
	otherPath := httptest.NewRequest(http.MethodPost, "/refund?x=1", bytes.NewReader(body))
	otherQuery := httptest.NewRequest(http.MethodPost, "/pay?x=2", bytes.NewReader(body))

	for name, request := range map[string]*http.Request{
		"method": otherMethod, "path": otherPath, "query": otherQuery,
	} {
		if SignRequest(key, ts, request, body) == signature {
			t.Fatalf("%s is not covered by the signature", name)
		}
	}
}
