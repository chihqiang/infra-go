package binding

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
)

// XMLBinding is a binder based on the XML body.
type XMLBinding struct{}

// Name returns the binder name.
func (XMLBinding) Name() string {
	return "xml"
}

// Bind binds the request XML body into obj and validates it.
func (XMLBinding) Bind(req *http.Request, obj any) error {
	if req == nil || req.Body == nil {
		return errors.New("invalid request")
	}
	return decodeXML(req.Body, obj)
}

// BindBody binds XML from a byte slice into obj and validates it.
func (XMLBinding) BindBody(body []byte, obj any) error {
	return decodeXML(bytes.NewReader(body), obj)
}

// decodeXML decodes XML from the reader into obj and validates it.
func decodeXML(r io.Reader, obj any) error {
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(obj); err != nil {
		return err
	}
	return validate(obj)
}
