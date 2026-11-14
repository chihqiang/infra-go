package binding

import (
	"bytes"
	"encoding/xml"
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
	if err := xml.NewDecoder(reader).Decode(dst); err != nil {
		return badRequest("rest/binding: decode xml: %v", err)
	}
	return nil
}
