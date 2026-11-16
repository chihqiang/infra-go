package rest

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"

	"github.com/chihqiang/infra-go/rest/handler"
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

	// JSONContentType is the content type used for JSON responses.
	JSONContentType = "application/json; charset=utf-8"
	// XMLContentType is the content type used for XML responses.
	XMLContentType = "application/xml; charset=utf-8"
	// HTMLContentType is the content type used for HTML responses.
	HTMLContentType = "text/html; charset=utf-8"
)

// BaseResponse is the envelope used by business API errors. Code is a business
// code, not an HTTP status; RequestID is filled from the request context when
// the RequestIDHandler middleware is in the chain.
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

// WriteJSON encodes v as JSON with the given HTTP status and returns encoding
// or write failures to the caller.
func WriteJSON(_ context.Context, w http.ResponseWriter, status int, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	body = append(body, '\n')

	header := w.Header()
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", JSONContentType)
	}
	w.WriteHeader(status)
	n, err := w.Write(body)
	if err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	if n < len(body) {
		return fmt.Errorf("write response: wrote %d of %d bytes", n, len(body))
	}
	return nil
}

// OkJSON writes v as a JSON BaseResponse with status 200.
func OkJSON(w http.ResponseWriter, v any) error {
	return OkJSONCtx(context.Background(), w, v)
}

// OkJSONCtx writes v as a JSON BaseResponse with status 200 and fills the
// request ID from ctx.
func OkJSONCtx(ctx context.Context, w http.ResponseWriter, v any) error {
	return WriteJSON(ctx, w, http.StatusOK, wrapBaseResponse(ctx, v))
}

// ErrorJSON writes err as a JSON BaseResponse. The HTTP status comes from
// StatusOf; internal 5xx details are hidden.
func ErrorJSON(w http.ResponseWriter, err error) error {
	return ErrorJSONCtx(context.Background(), w, err)
}

// ErrorJSONCtx is ErrorJSON with a request context.
func ErrorJSONCtx(ctx context.Context, w http.ResponseWriter, err error) error {
	return WriteJSON(ctx, w, StatusOf(err), wrapBaseResponse(ctx, err))
}

// WriteXML writes v as XML with the given HTTP status.
func WriteXML(w http.ResponseWriter, status int, v any) error {
	body, err := xml.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return fmt.Errorf("marshal xml: %w", err)
	}

	w.Header().Set("Content-Type", XMLContentType)
	w.WriteHeader(status)
	n, err := w.Write(body)
	if err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	if n < len(body) {
		return fmt.Errorf("write response: wrote %d of %d bytes", n, len(body))
	}
	return nil
}

// OkXML writes v as an XML BaseResponse with status 200.
func OkXML(w http.ResponseWriter, v any) error {
	return OkXMLCtx(context.Background(), w, v)
}

// OkXMLCtx writes v as an XML BaseResponse with status 200 and fills the
// request ID from ctx.
func OkXMLCtx(ctx context.Context, w http.ResponseWriter, v any) error {
	body := xmlEnvelope[any]{
		Version:      xmlVersion,
		Encoding:     xmlEncoding,
		BaseResponse: wrapBaseResponse(ctx, v),
	}
	return WriteXML(w, http.StatusOK, body)
}

// ErrorXML writes err as an XML BaseResponse. The HTTP status comes from
// StatusOf; internal 5xx details are hidden.
func ErrorXML(w http.ResponseWriter, err error) error {
	return ErrorXMLCtx(context.Background(), w, err)
}

// ErrorXMLCtx is ErrorXML with a request context.
func ErrorXMLCtx(ctx context.Context, w http.ResponseWriter, err error) error {
	body := xmlEnvelope[any]{
		Version:      xmlVersion,
		Encoding:     xmlEncoding,
		BaseResponse: wrapBaseResponse(ctx, err),
	}
	return WriteXML(w, StatusOf(err), body)
}

// WriteHTML writes v as an HTML body with the given HTTP status.
func WriteHTML(w http.ResponseWriter, status int, v string) error {
	w.Header().Set("Content-Type", HTMLContentType)
	w.WriteHeader(status)
	n, err := w.Write([]byte(v))
	if err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	if n < len(v) {
		return fmt.Errorf("write response: wrote %d of %d bytes", n, len(v))
	}
	return nil
}

func wrapBaseResponse(ctx context.Context, v any) BaseResponse[any] {
	var response BaseResponse[any]
	switch data := v.(type) {
	case CodeMsg:
		response = BaseResponse[any]{Code: data.Code, Msg: data.Msg}
	case *status.Status:
		response = BaseResponse[any]{Code: int(data.Code()), Msg: data.Message()}
	case interface{ GRPCStatus() *status.Status }:
		grpcStatus := data.GRPCStatus()
		response = BaseResponse[any]{Code: int(grpcStatus.Code()), Msg: grpcStatus.Message()}
	case error:
		var codeMsg *CodeMsg
		status := StatusOf(data)
		switch {
		case errors.As(data, &codeMsg):
			// Business errors are intentional and retain their code and message.
			response = BaseResponse[any]{Code: codeMsg.Code, Msg: codeMsg.Msg}
		case status >= http.StatusInternalServerError:
			// Hide internal details of unexpected 5xx errors.
			response = BaseResponse[any]{Code: BusinessCodeError, Msg: http.StatusText(status)}
		default:
			response = BaseResponse[any]{Code: BusinessCodeError, Msg: data.Error()}
		}
	default:
		response = BaseResponse[any]{Code: BusinessCodeOK, Msg: BusinessMsgOK, Data: v}
	}
	response.RequestID, _ = handler.RequestIDFromContext(ctx)
	return response
}
