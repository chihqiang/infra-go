package binding

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
)

type xmlBinding struct{}

func (xmlBinding) Name() string { return "xml" }

func (xmlBinding) Bind(req *http.Request, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}
	if err := decodeXML(req.Body, dst); err != nil {
		return err
	}
	return validate(dst)
}

func (xmlBinding) BindBody(body []byte, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}
	if err := decodeXML(bytes.NewReader(body), dst); err != nil {
		return err
	}
	return validate(dst)
}

func decodeXML(reader io.Reader, dst any) error {
	if reader == nil {
		return badRequest("rest/binding: xml body is empty")
	}
	decoder := xml.NewDecoder(reader)
	if err := decoder.Decode(dst); err != nil {
		return requestError("rest/binding: decode xml: %w", err)
	}
	// Like the JSON binder, reject data after the first document instead of
	// silently ignoring it.
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err != nil {
			return badRequest("rest/binding: xml body contains trailing data: %v", err)
		}
		return badRequest("rest/binding: xml body contains trailing data")
	}
	return nil
}
