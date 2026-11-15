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
	request.Header.Set(HeaderSignature, Sign(key, timestamp, body))
}

func TestSignatureHandlerAcceptsSignedBody(t *testing.T) {
	key := []byte("signing-key")
	body := []byte(`{"amount":10}`)
	var received string
	handlerFn := SignatureHandler(SignatureConfig{Keys: [][]byte{key}, TTL: time.Minute})(
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
	handlerFn := SignatureHandler(SignatureConfig{Keys: [][]byte{key}})(
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
	handlerFn := SignatureHandler(SignatureConfig{Keys: [][]byte{key}})(
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
	handlerFn := SignatureHandler(SignatureConfig{
		Keys: [][]byte{key},
		TTL:  time.Minute,
		Now:  func() time.Time { return now },
	})(func(http.ResponseWriter, *http.Request) { t.Fatal("must not reach handler") })

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
	handlerFn := SignatureHandler(SignatureConfig{Keys: [][]byte{newKey, oldKey}})(
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
	handlerFn := SignatureHandler(SignatureConfig{Keys: [][]byte{key}, MaxBody: 8})(
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

	SignatureHandler(SignatureConfig{})
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func (failingBody) Close() error { return nil }

func TestSignatureHandlerMapsReadErrorToBadRequest(t *testing.T) {
	handlerFn := SignatureHandler(SignatureConfig{Keys: [][]byte{[]byte("signing-key")}})(
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
	handlerFn := SignatureHandler(SignatureConfig{
		Keys: [][]byte{[]byte("signing-key")},
		TTL:  time.Minute,
	})(func(http.ResponseWriter, *http.Request) {})

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
