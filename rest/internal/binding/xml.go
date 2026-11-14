package binding

import (
	"encoding/xml"
	"fmt"
	"net/http"
)

// XML decodes an XML request body.
var XML Binding = xmlBinding{}

type xmlBinding struct{}

func (xmlBinding) Bind(req *http.Request, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}

	if err := xml.NewDecoder(req.Body).Decode(dst); err != nil {
		return wrapError(fmt.Errorf("binding: decode xml: %w", err))
	}

	return validate(dst)
}
