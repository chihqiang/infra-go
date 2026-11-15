package handler

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func gzipBody(t *testing.T, text string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestGunzipHandlerDecompressesBody(t *testing.T) {
	var got string
	handlerFn := GunzipHandler(func(_ http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		got = string(body)
	})

	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(gzipBody(t, "payload")))
	request.Header.Set(headerContentEncoding, "gzip")
	handlerFn.ServeHTTP(httptest.NewRecorder(), request)

	if got != "payload" {
		t.Fatalf("body = %q, want payload", got)
	}
}

func TestGunzipHandlerPassesPlainBodies(t *testing.T) {
	var got string
	handlerFn := GunzipHandler(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = string(body)
	})

	handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("plain")))

	if got != "plain" {
		t.Fatalf("body = %q, want plain", got)
	}
}

func TestGunzipHandlerRejectsInvalidGzip(t *testing.T) {
	called := false
	handlerFn := GunzipHandler(func(http.ResponseWriter, *http.Request) {
		called = true
	})

	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not gzip"))
	request.Header.Set(headerContentEncoding, "gzip")
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if called {
		t.Fatal("handler must not run for invalid gzip")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if got := recorder.Body.String(); got != "{\"code\":-1,\"msg\":\"invalid gzip body\"}\n" {
		t.Fatalf("body = %q, want the shared error envelope", got)
	}
}

func TestGunzipHandlerMatchesEncodingTokensCaseInsensitively(t *testing.T) {
	for _, encoding := range []string{"GZIP", "x-gzip", "identity, Gzip"} {
		var got string
		handlerFn := GunzipHandler(func(_ http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			got = string(body)
		})

		request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(gzipBody(t, "payload")))
		request.Header.Set(headerContentEncoding, encoding)
		handlerFn.ServeHTTP(httptest.NewRecorder(), request)

		if got != "payload" {
			t.Fatalf("encoding %q: body = %q, want payload", encoding, got)
		}
	}
}
