package binding

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// JSON decodes a JSON request body.
var JSON Binding = jsonBinding{}

type jsonBinding struct{}

func (jsonBinding) Bind(req *http.Request, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}

	decoder := json.NewDecoder(req.Body)
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return wrapError(errors.New("binding: json body is empty"))
		}
		return wrapError(fmt.Errorf("binding: decode json: %w", err))
	}
	if decoder.More() {
		return wrapError(errors.New("binding: json body contains trailing data"))
	}

	return validate(dst)
}

func checkDestination(dst any) error {
	if dst == nil {
		return errors.New("binding: destination must not be nil")
	}
	return nil
}
