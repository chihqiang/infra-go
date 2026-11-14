package binding

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

type jsonBinding struct{}

func (jsonBinding) Name() string { return "json" }

func (jsonBinding) Bind(req *http.Request, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}
	if req.Body == nil {
		return badRequest("rest/binding: json body is empty")
	}

	decoder := json.NewDecoder(req.Body)
	decoder.UseNumber()
	if err := decodeJSON(decoder, dst); err != nil {
		return err
	}
	return validate(dst)
}

func (jsonBinding) BindBody(body []byte, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decodeJSON(decoder, dst); err != nil {
		return err
	}
	return validate(dst)
}

func decodeJSON(decoder *json.Decoder, dst any) error {
	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return badRequest("rest/binding: json body is empty")
		}
		return requestError("rest/binding: decode json: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	return nil
}

// ensureJSONEOF rejects anything after the top-level JSON value. json.Decoder.More
// is not enough: it reports false for a trailing '}' or ']', silently accepting
// malformed documents such as `{"a":1}}`.
func ensureJSONEOF(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return badRequest("rest/binding: json body contains trailing data: %v", err)
	}
	return badRequest("rest/binding: json body contains trailing data: %v", token)
}
