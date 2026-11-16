package rest

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"

	"github.com/chihqiang/infra-go/core/logger"
	"google.golang.org/grpc/status"
)

const (
	xmlVersion  = "1.0"
	xmlEncoding = "UTF-8"

	// BusinessCodeOK is the code for a successful business response.
	BusinessCodeOK = 0
	// BusinessMsgOK is the default message for a successful business response.
	BusinessMsgOK = "ok"
	// BusinessCodeError is the code for a failed business response.
	BusinessCodeError = -1

	// JSONContentType is the content type of JSON responses.
	JSONContentType = "application/json; charset=utf-8"
	// XMLContentType is the content type of XML responses.
	XMLContentType = "application/xml; charset=utf-8"
	// HTMLContentType is the content type of HTML responses.
	HTMLContentType = "text/html; charset=utf-8"
)

// BaseResponse is the envelope used by business APIs. Code is a business code,
// not an HTTP status; RequestID is filled from the request context when the
// RequestIDHandler middleware is in the chain.
type BaseResponse[T any] struct {
	Code      int    `json:"code" xml:"code"`
	Msg       string `json:"msg" xml:"msg"`
	RequestID string `json:"request_id,omitempty" xml:"request_id,omitempty"`
	Data      T      `json:"data,omitempty" xml:"data,omitempty"`
}

type xmlEnvelope[T any] struct {
	XMLName  xml.Name `xml:"xml"`
	Version  string   `xml:"version,attr"`
	Encoding string   `xml:"encoding,attr"`
	BaseResponse[T]
}

// WrapBaseResponse converts v into a BaseResponse:
//   - *CodeMsg / CodeMsg keep their business code and message
//   - gRPC status values map their code and message
//   - other errors become BusinessCodeError
//   - everything else becomes BusinessCodeOK with v as Data
func WrapBaseResponse(v any) BaseResponse[any] {
	switch data := v.(type) {
	case *CodeMsg:
		return BaseResponse[any]{Code: data.Code, Msg: data.Msg}
	case CodeMsg:
		return BaseResponse[any]{Code: data.Code, Msg: data.Msg}
	case *status.Status:
		return BaseResponse[any]{Code: int(data.Code()), Msg: data.Message()}
	case interface{ GRPCStatus() *status.Status }:
		grpcStatus := data.GRPCStatus()
		return BaseResponse[any]{Code: int(grpcStatus.Code()), Msg: grpcStatus.Message()}
	case error:
		return BaseResponse[any]{Code: BusinessCodeError, Msg: data.Error()}
	default:
		return BaseResponse[any]{Code: BusinessCodeOK, Msg: BusinessMsgOK, Data: v}
	}
}

// WriteJSON encodes v as JSON with the given HTTP status.
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	header := w.Header()
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", JSONContentType)
	}
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// OkJSON writes v as JSON with status 200.
func OkJSON(w http.ResponseWriter, v any) error {
	return WriteJSON(w, http.StatusOK, v)
}

// CreatedJSON writes v as JSON with status 201.
func CreatedJSON(w http.ResponseWriter, v any) error {
	return WriteJSON(w, http.StatusCreated, v)
}

// NoContent writes a bodyless 204 response.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Error writes err as a BaseResponse envelope. The HTTP status comes from
// StatusOf, while Code stays a business code (-1 for generic errors or the
// CodeMsg code). For 5xx responses, details of unknown internal errors are
// hidden; a CodeMsg keeps its code and message because the caller chose them to
// be user visible (for example a maintenance-window 503).
func Error(w http.ResponseWriter, err error) error {
	return ErrorCtx(context.Background(), w, err)
}

// ErrorCtx is Error with a request context so the envelope carries the request
// ID assigned by RequestIDHandler.
func ErrorCtx(ctx context.Context, w http.ResponseWriter, err error) error {
	status := StatusOf(err)
	if status >= http.StatusInternalServerError {
		if codeMsg := asCodeMsg(err); codeMsg != nil {
			// An expected business failure (a maintenance window, an upstream 502).
			// It is not an internal error: log it without a stack trace so it does not
			// pollute the error log.
			logger.WarnCtx(ctx, "rest: business error response",
				logger.Int("status", status), logger.Err(err))
			return WriteJSON(w, status, WrapBaseResponseCtx(ctx, codeMsg))
		}
		logger.ErrorCtx(ctx, "rest: request failed", logger.Err(err))
		return WriteJSON(w, status, WrapBaseResponseCtx(ctx, fmt.Errorf("%s", http.StatusText(status))))
	}
	return WriteJSON(w, status, WrapBaseResponseCtx(ctx, err))
}

// asCodeMsg reports the business error carried by err, including wrapped ones.
// CodeMsg is the only error type whose message is safe to expose on a 5xx
// response; arbitrary errors may contain internal details.
func asCodeMsg(err error) *CodeMsg {
	var codeMsg *CodeMsg
	if errors.As(err, &codeMsg) {
		return codeMsg
	}
	return nil
}

// WrapBaseResponseCtx is WrapBaseResponse with the request ID taken from ctx.
func WrapBaseResponseCtx(ctx context.Context, v any) BaseResponse[any] {
	resp := WrapBaseResponse(v)
	resp.RequestID = RequestIDFromContext(ctx)
	return resp
}

// JSONBaseResponse writes v wrapped in BaseResponse as JSON with status 200.
func JSONBaseResponse(w http.ResponseWriter, v any) {
	JSONBaseResponseCtx(context.Background(), w, v)
}

// JSONBaseResponseCtx writes v wrapped in BaseResponse as JSON with status 200,
// adding the request ID from ctx.
func JSONBaseResponseCtx(ctx context.Context, w http.ResponseWriter, v any) {
	if err := OkJSON(w, WrapBaseResponseCtx(ctx, v)); err != nil {
		logger.ErrorCtx(ctx, "rest: write json base response", logger.Err(err))
	}
}

// XMLBaseResponse writes v wrapped in BaseResponse as XML with status 200.
func XMLBaseResponse(w http.ResponseWriter, v any) {
	XMLBaseResponseCtx(context.Background(), w, v)
}

// XMLBaseResponseCtx writes v wrapped in BaseResponse as XML with status 200,
// adding the request ID from ctx.
func XMLBaseResponseCtx(ctx context.Context, w http.ResponseWriter, v any) {
	OkXMLCtx(ctx, w, xmlEnvelope[any]{
		Version:      xmlVersion,
		Encoding:     xmlEncoding,
		BaseResponse: WrapBaseResponseCtx(ctx, v),
	})
}

// OkXML writes v as an XML body with status 200.
func OkXML(w http.ResponseWriter, v any) {
	WriteXML(w, http.StatusOK, v)
}

// OkXMLCtx writes v as an XML body with status 200 and logs with ctx.
func OkXMLCtx(ctx context.Context, w http.ResponseWriter, v any) {
	if err := doWriteXML(w, http.StatusOK, v); err != nil {
		logger.ErrorCtx(ctx, "rest: write xml response", logger.Err(err))
	}
}

// WriteXML writes v as an XML body with the given status.
func WriteXML(w http.ResponseWriter, status int, v any) {
	if err := doWriteXML(w, status, v); err != nil {
		logger.Error("rest: write xml response", logger.Err(err))
	}
}

func doWriteXML(w http.ResponseWriter, status int, v any) error {
	body, err := xml.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return fmt.Errorf("marshal xml: %w", err)
	}

	w.Header().Set("Content-Type", XMLContentType)
	w.WriteHeader(status)
	return writeAll(w, body)
}

// OkHTML writes v as an HTML body with status 200.
func OkHTML(w http.ResponseWriter, v string) {
	WriteHTML(w, http.StatusOK, v)
}

// OkHTMLCtx writes v as an HTML body with status 200 and logs with ctx.
func OkHTMLCtx(ctx context.Context, w http.ResponseWriter, v string) {
	if err := doWriteHTML(w, http.StatusOK, v); err != nil {
		logger.ErrorCtx(ctx, "rest: write html response", logger.Err(err))
	}
}

// WriteHTML writes v as an HTML body with the given status.
func WriteHTML(w http.ResponseWriter, status int, v string) {
	if err := doWriteHTML(w, status, v); err != nil {
		logger.Error("rest: write html response", logger.Err(err))
	}
}

func doWriteHTML(w http.ResponseWriter, status int, v string) error {
	w.Header().Set("Content-Type", HTMLContentType)
	w.WriteHeader(status)
	return writeAll(w, []byte(v))
}

func writeAll(w http.ResponseWriter, body []byte) error {
	written, err := w.Write(body)
	if err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	if written < len(body) {
		return fmt.Errorf("write response: wrote %d of %d bytes", written, len(body))
	}
	return nil
}
