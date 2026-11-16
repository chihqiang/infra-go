package rest

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chihqiang/infra-go/core/logger"
)

func TestServerLoggerAccessors(t *testing.T) {
	srv, err := NewServer(DefaultConfig())
	require.NoError(t, err)
	assert.Nil(t, srv.Logger())

	l := logger.New(logger.Config{})
	withLogger, err := NewServer(DefaultConfig(), WithLogger(l))
	require.NoError(t, err)
	assert.Same(t, l, withLogger.Logger())
}

func TestServerServeNilListener(t *testing.T) {
	srv, err := NewServer(DefaultConfig())
	require.NoError(t, err)
	require.Error(t, srv.Serve(nil))
	assert.Contains(t, srv.Serve(nil).Error(), "listener must not be nil")
}

func TestServerStartReportsListenError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	srv, err := NewServer(Config{Host: "127.0.0.1", Port: port})
	require.NoError(t, err)

	require.Error(t, srv.Start(), "the port is already in use")
}

func TestServerCloseAndStopWithoutServe(t *testing.T) {
	srv, err := NewServer(DefaultConfig())
	require.NoError(t, err)
	require.NoError(t, srv.Close())

	srv2, err := NewServer(DefaultConfig())
	require.NoError(t, err)
	require.NoError(t, srv2.Stop())
}

func TestWithBreakerNilAndAddServiceNil(t *testing.T) {
	srv, err := NewServer(DefaultConfig(), WithBreaker(nil))
	require.NoError(t, err)
	srv.AddService(nil) // ignored
	srv.Register(nil)   // ignored
	assert.Nil(t, srv.Logger())
}

func TestResponseXMLAndHTMLWriters(t *testing.T) {
	type payload struct {
		Name string `xml:"name"`
	}

	xmlRecorder := httptest.NewRecorder()
	require.NoError(t, OkXML(xmlRecorder, payload{Name: "alice"}))
	assert.Equal(t, http.StatusOK, xmlRecorder.Code)
	assert.Equal(t, XMLContentType, xmlRecorder.Header().Get("Content-Type"))
	assert.Contains(t, xmlRecorder.Body.String(), "<name>alice</name>")

	written := httptest.NewRecorder()
	require.NoError(t, WriteXML(written, http.StatusCreated, payload{Name: "bob"}))
	assert.Equal(t, http.StatusCreated, written.Code)

	// A value that cannot be marshalled becomes a 500 from http.Error.
	bad := httptest.NewRecorder()
	require.Error(t, OkXML(bad, make(chan int)))
	assert.Equal(t, http.StatusInternalServerError, bad.Code)

	htmlRecorder := httptest.NewRecorder()
	require.NoError(t, WriteHTML(htmlRecorder, http.StatusOK, "<p>hi</p>"))
	assert.Equal(t, http.StatusOK, htmlRecorder.Code)
	assert.Equal(t, HTMLContentType, htmlRecorder.Header().Get("Content-Type"))
	assert.Equal(t, "<p>hi</p>", htmlRecorder.Body.String())

	writtenHTML := httptest.NewRecorder()
	require.NoError(t, WriteHTML(writtenHTML, http.StatusAccepted, "ok"))
	assert.Equal(t, http.StatusAccepted, writtenHTML.Code)

	ctxXML := httptest.NewRecorder()
	require.NoError(t, OkXMLCtx(nil, ctxXML, payload{Name: "ctx"}))
	assert.Equal(t, http.StatusOK, ctxXML.Code)
}

type shortWriter struct {
	header http.Header
	status int
}

func (w *shortWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *shortWriter) WriteHeader(status int) { w.status = status }
func (w *shortWriter) Write(p []byte) (int, error) {
	return len(p) - 1, nil
}

func TestResponseWritersReportShortWrite(t *testing.T) {
	require.ErrorContains(t, WriteXML(&shortWriter{}, http.StatusOK, "body"), "wrote")
	require.ErrorContains(t, WriteHTML(&shortWriter{}, http.StatusOK, "body"), "wrote")
}

func TestRouteOptionsNoOp(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	route := Route{Method: http.MethodGet, Path: "/", Handler: handler}

	// No middleware and a nil handler are no-ops.
	WithRouteMiddleware()(&route)
	assert.NotNil(t, route.Handler)
	route.Handler = nil
	WithRouteMiddleware(func(next http.HandlerFunc) http.HandlerFunc { return next })(&route)
	assert.Nil(t, route.Handler)

	got := applyRouteOptions(Route{Path: "/x"}, []RouteOption{nil, func(r *Route) { r.Method = http.MethodPost }})
	assert.Equal(t, "/x", got.Path)
	assert.Equal(t, http.MethodPost, got.Method)
}

func TestWithLoggerKeepsGlobalUntouched(t *testing.T) {
	before := logger.GetGlobal()
	l := logger.New(logger.Config{})
	srv, err := NewServer(DefaultConfig(), WithLogger(l))
	require.NoError(t, err)
	assert.Same(t, l, srv.Logger())
	assert.Same(t, before, logger.GetGlobal(), "WithLogger must not install the logger globally")
}

// TestWithLoggerDrivesAccessLog guards the option ordering in NewServer: the
// injected logger must reach the access-log middleware, not only s.Logger().
func TestWithLoggerDrivesAccessLog(t *testing.T) {
	dir := t.TempDir()
	injected := logger.New(logger.Config{Mode: "file", Path: dir, Encoding: "plain", Level: "info"})
	t.Cleanup(func() {
		if closer, ok := injected.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})

	server, err := NewServer(DefaultConfig(), WithLogger(injected))
	require.NoError(t, err)
	server.HandleFunc(http.MethodGet, "/logged", func(http.ResponseWriter, *http.Request) {})
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/logged", nil))
	require.NoError(t, injected.Sync())

	data, err := os.ReadFile(filepath.Join(dir, "access.log"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "/logged")
}

// TestWithLoggerTakesPrecedenceOverConfigLogger documents the option contract:
// an injected logger wins and is never installed globally by NewServer.
func TestWithLoggerTakesPrecedenceOverConfigLogger(t *testing.T) {
	previous := logger.GetGlobal()
	t.Cleanup(func() { logger.SetGlobal(previous) })

	injected := logger.New(logger.Config{})
	server, err := NewServer(Config{Logger: logger.Config{Mode: "console"}}, WithLogger(injected))
	require.NoError(t, err)

	assert.Same(t, injected, server.Logger())
	assert.Same(t, previous, logger.GetGlobal(), "an injected logger must not become the global logger")
}
