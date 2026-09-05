package binding

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
)

// XMLBinding 基于 XML body 的绑定器。
type XMLBinding struct{}

// Name 返回绑定器名称。
func (XMLBinding) Name() string {
	return "xml"
}

// Bind 将请求 XML body 绑定到 obj，并校验。
func (XMLBinding) Bind(req *http.Request, obj any) error {
	if req == nil || req.Body == nil {
		return errors.New("invalid request")
	}
	return decodeXML(req.Body, obj)
}

// BindBody 从字节数组绑定 XML 到 obj，并校验。
func (XMLBinding) BindBody(body []byte, obj any) error {
	return decodeXML(bytes.NewReader(body), obj)
}

// decodeXML 从 reader 解码 XML 到 obj，并校验。
func decodeXML(r io.Reader, obj any) error {
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(obj); err != nil {
		return err
	}
	return validate(obj)
}
