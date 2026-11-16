package rest

import (
	"net"
	"net/http"
	"net/http/httptest"
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

func TestRequestIDNilRequest(t *testing.T) {
	assert.Equal(t, "", RequestID(nil))
	assert.Equal(t, "", RequestIDFromContext(nil))
}

func TestResponseXMLAndHTMLWriters(t *testing.T) {
	type payload struct {
		Name string `xml:"name"`
	}

	xmlRecorder := httptest.NewRecorder()
	OkXML(xmlRecorder, payload{Name: "alice"})
	assert.Equal(t, http.StatusOK, xmlRecorder.Code)
	assert.Equal(t, XMLContentType, xmlRecorder.Header().Get("Content-Type"))
	assert.Contains(t, xmlRecorder.Body.String(), "<name>alice</name>")

	written := httptest.NewRecorder()
	WriteXML(written, http.StatusCreated, payload{Name: "bob"})
	assert.Equal(t, http.StatusCreated, written.Code)

	// A value that cannot be marshalled becomes a 500 from http.Error.
	bad := httptest.NewRecorder()
	OkXML(bad, make(chan int))
	assert.Equal(t, http.StatusInternalServerError, bad.Code)

	htmlRecorder := httptest.NewRecorder()
	OkHTML(htmlRecorder, "<p>hi</p>")
	assert.Equal(t, http.StatusOK, htmlRecorder.Code)
	assert.Equal(t, HTMLContentType, htmlRecorder.Header().Get("Content-Type"))
	assert.Equal(t, "<p>hi</p>", htmlRecorder.Body.String())

	writtenHTML := httptest.NewRecorder()
	WriteHTML(writtenHTML, http.StatusAccepted, "ok")
	assert.Equal(t, http.StatusAccepted, writtenHTML.Code)

	ctxHTML := httptest.NewRecorder()
	OkHTMLCtx(nil, ctxHTML, "ctx")
	assert.Equal(t, http.StatusOK, ctxHTML.Code)

	ctxXML := httptest.NewRecorder()
	OkXMLCtx(nil, ctxXML, payload{Name: "ctx"})
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

func TestWriteAllReportsShortWrite(t *testing.T) {
	err := writeAll(&shortWriter{}, []byte("body"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrote")
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
