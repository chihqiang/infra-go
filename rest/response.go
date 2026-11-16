package rest

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"

	"github.com/chihqiang/infra-go/core/logger"
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

// WriteJSON encodes v as JSON with the given HTTP status, logs encoding and
// write failures with ctx, and returns them.
func WriteJSON(ctx context.Context, w http.ResponseWriter, status int, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		logger.ErrorCtx(ctx, "rest: write json response", logger.Err(err))
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
		logger.ErrorCtx(ctx, "rest: write json response", logger.Err(err))
		return fmt.Errorf("write response: %w", err)
	}
	if n < len(body) {
		err := fmt.Errorf("write response: wrote %d of %d bytes", n, len(body))
		logger.ErrorCtx(ctx, "rest: write json response", logger.Err(err))
		return err
	}
	return nil
}

// OkJSON writes v as JSON with status 200.
func OkJSON(w http.ResponseWriter, v any) error {
	return WriteJSON(context.Background(), w, http.StatusOK, v)
}

// OkJSONCtx writes v as JSON with status 200 and logs with ctx.
func OkJSONCtx(ctx context.Context, w http.ResponseWriter, v any) error {
	return WriteJSON(ctx, w, http.StatusOK, v)
}

// ErrorJSON writes err as a JSON BaseResponse. The HTTP status comes from
// StatusOf; internal 5xx details are hidden.
func ErrorJSON(w http.ResponseWriter, err error) error {
	return ErrorJSONCtx(context.Background(), w, err)
}

// ErrorJSONCtx is ErrorJSON with a request context.
func ErrorJSONCtx(ctx context.Context, w http.ResponseWriter, err error) error {
	status, response := errorResponse(ctx, err)
	return WriteJSON(ctx, w, status, response)
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

// OkXML writes v as XML with status 200.
func OkXML(w http.ResponseWriter, v any) error {
	return WriteXML(w, http.StatusOK, v)
}

// OkXMLCtx writes v as XML with status 200 and logs with ctx.
func OkXMLCtx(ctx context.Context, w http.ResponseWriter, v any) error {
	if err := WriteXML(w, http.StatusOK, v); err != nil {
		logger.ErrorCtx(ctx, "rest: write xml response", logger.Err(err))
		return err
	}
	return nil
}

// ErrorXML writes err as an XML BaseResponse. The HTTP status comes from
// StatusOf; internal 5xx details are hidden.
func ErrorXML(w http.ResponseWriter, err error) error {
	return ErrorXMLCtx(context.Background(), w, err)
}

// ErrorXMLCtx is ErrorXML with a request context.
func ErrorXMLCtx(ctx context.Context, w http.ResponseWriter, err error) error {
	status, response := errorResponse(ctx, err)
	body := xmlEnvelope[any]{
		Version:      xmlVersion,
		Encoding:     xmlEncoding,
		BaseResponse: response,
	}
	if err := WriteXML(w, status, body); err != nil {
		logger.ErrorCtx(ctx, "rest: write xml error response", logger.Err(err))
		return err
	}
	return nil
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

func errorResponse(ctx context.Context, err error) (int, BaseResponse[any]) {
	status := StatusOf(err)
	if status >= http.StatusInternalServerError {
		if codeMsg := asCodeMsg(err); codeMsg != nil {
			// Business 5xx responses are intentional and retain their message.
			logger.WarnCtx(ctx, "rest: business error response",
				logger.Int("status", status), logger.Err(err))
			return status, wrapBaseResponse(ctx, codeMsg)
		}
		logger.ErrorCtx(ctx, "rest: request failed", logger.Err(err))
		err = fmt.Errorf("%s", http.StatusText(status))
	}
	return status, wrapBaseResponse(ctx, err)
}

func wrapBaseResponse(ctx context.Context, v any) BaseResponse[any] {
	var response BaseResponse[any]
	switch data := v.(type) {
	case *CodeMsg:
		response = BaseResponse[any]{Code: data.Code, Msg: data.Msg}
	case CodeMsg:
		response = BaseResponse[any]{Code: data.Code, Msg: data.Msg}
	case *status.Status:
		response = BaseResponse[any]{Code: int(data.Code()), Msg: data.Message()}
	case interface{ GRPCStatus() *status.Status }:
		grpcStatus := data.GRPCStatus()
		response = BaseResponse[any]{Code: int(grpcStatus.Code()), Msg: grpcStatus.Message()}
	case error:
		response = BaseResponse[any]{Code: BusinessCodeError, Msg: data.Error()}
	default:
		response = BaseResponse[any]{Code: BusinessCodeOK, Msg: BusinessMsgOK, Data: v}
	}
	response.RequestID, _ = handler.RequestIDFromContext(ctx)
	return response
}

func asCodeMsg(err error) *CodeMsg {
	var codeMsg *CodeMsg
	if errors.As(err, &codeMsg) {
		return codeMsg
	}
	return nil
}
