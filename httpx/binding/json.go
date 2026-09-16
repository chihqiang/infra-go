package binding

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// JSONBinding is a binder based on the JSON body.
type JSONBinding struct{}

// Name returns the binder name.
func (JSONBinding) Name() string {
	return "json"
}

// Bind binds the request JSON body into obj and validates it.
func (JSONBinding) Bind(req *http.Request, obj any) error {
	if req == nil || req.Body == nil {
		return errors.New("invalid request")
	}
	return decodeJSON(req.Body, obj)
}

// BindBody binds JSON from a byte slice into obj and validates it.
func (JSONBinding) BindBody(body []byte, obj any) error {
	return decodeJSON(bytes.NewReader(body), obj)
}

// decodeJSON decodes JSON from the reader into obj and validates it.
func decodeJSON(r io.Reader, obj any) error {
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(obj); err != nil {
		return err
	}
	return validate(obj)
}
