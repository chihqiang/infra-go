package binding

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// JSONBinding 基于 JSON body 的绑定器。
type JSONBinding struct{}

// Name 返回绑定器名称。
func (JSONBinding) Name() string {
	return "json"
}

// Bind 将请求 JSON body 绑定到 obj，并校验。
func (JSONBinding) Bind(req *http.Request, obj any) error {
	if req == nil || req.Body == nil {
		return errors.New("invalid request")
	}
	return decodeJSON(req.Body, obj)
}

// BindBody 从字节数组绑定 JSON 到 obj，并校验。
func (JSONBinding) BindBody(body []byte, obj any) error {
	return decodeJSON(bytes.NewReader(body), obj)
}

// decodeJSON 从 reader 解码 JSON 到 obj，并校验。
func decodeJSON(r io.Reader, obj any) error {
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(obj); err != nil {
		return err
	}
	return validate(obj)
}
