package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serveHealth(t *testing.T, srv *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	srv.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func TestHealthEndpointDisabledByDefault(t *testing.T) {
	srv, err := NewServer(DefaultConfig())
	require.NoError(t, err)

	assert.False(t, DefaultConfig().Health.Enabled)
	assert.Empty(t, DefaultConfig().Health.Path, "a disabled health endpoint must not get a default path")

	recorder := serveHealth(t, srv, http.MethodGet, defaultHealthPath)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestHealthEndpointEnabled(t *testing.T) {
	srv, err := NewServer(Config{Health: HealthConfig{Enabled: true}})
	require.NoError(t, err)

	recorder := serveHealth(t, srv, http.MethodGet, defaultHealthPath)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, JSONContentType, recorder.Header().Get("Content-Type"))

	var body BaseResponse[map[string]string]
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, BusinessCodeOK, body.Code)
	assert.Equal(t, "ok", body.Data["status"])

	// The GET pattern also serves HEAD.
	assert.Equal(t, http.StatusOK, serveHealth(t, srv, http.MethodHead, defaultHealthPath).Code)
}

func TestHealthEndpointCustomPath(t *testing.T) {
	srv, err := NewServer(Config{Health: HealthConfig{Enabled: true, Path: "/healthz"}})
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, serveHealth(t, srv, http.MethodGet, "/healthz").Code)
	assert.Equal(t, http.StatusNotFound, serveHealth(t, srv, http.MethodGet, "/health").Code)
}

func TestHealthEndpointNormalizesPath(t *testing.T) {
	cases := []string{"healthz", "/healthz", " /healthz "}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			srv, err := NewServer(Config{Health: HealthConfig{Enabled: true, Path: path}})
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, serveHealth(t, srv, http.MethodGet, "/healthz").Code)
		})
	}
}

func TestHealthRouteIsRecorded(t *testing.T) {
	srv, err := NewServer(Config{Health: HealthConfig{Enabled: true, Path: "/live"}})
	require.NoError(t, err)

	var found bool
	for _, route := range srv.Routes() {
		if route.Method == http.MethodGet && route.Path == "/live" {
			found = true
		}
	}
	assert.True(t, found, "the health route must appear in Routes()")
}

func TestHealthConfigValidation(t *testing.T) {
	invalid := []string{"/", "/health/", "/he alth", "/health?x=1", "/health/{id}"}
	for _, path := range invalid {
		t.Run(path, func(t *testing.T) {
			err := Config{Health: HealthConfig{Enabled: true, Path: path}}.Validate()
			require.ErrorIs(t, err, ErrInvalidHealthPath)
		})
	}

	require.NoError(t, Config{Health: HealthConfig{Enabled: true, Path: "/health"}}.Validate())
	// A disabled endpoint keeps its path unchecked.
	require.NoError(t, Config{Health: HealthConfig{Enabled: false, Path: "/"}}.Validate())
}
