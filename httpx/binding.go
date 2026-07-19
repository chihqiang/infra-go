package httpx

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chihqiang/infra-go/cast"
	"github.com/go-playground/validator/v10"
)

// --- MIME 类型常量 ---

// 常见的 Content-Type MIME 类型。
const (
	MIMEJSON              = "application/json"
	MIMEXML               = "application/xml"
	MIMEXML2              = "text/xml"
	MIMEPlain             = "text/plain"
	MIMEPOSTForm          = "application/x-www-form-urlencoded"
	MIMEMultipartPOSTForm = "multipart/form-data"
)

// --- 绑定器接口 ---

// Binding 描述将请求数据绑定到结构体的接口。
// 不同数据来源（JSON body、Query 参数、Form 表单等）实现此接口。
type Binding interface {
	// Name 返回绑定器名称。
	Name() string
	// Bind 将请求数据绑定到 obj 结构体。
	Bind(*http.Request, any) error
}

// BindingBody 扩展 Binding 接口，支持从原始字节绑定。
// 用于 JSON、XML 等基于 body 的绑定器。
type BindingBody interface {
	Binding
	// BindBody 从字节数组绑定到 obj 结构体。
	BindBody([]byte, any) error
}

// BindingUri 扩展接口，支持从 URI 路径参数绑定。
type BindingUri interface {
	Name() string
	// BindUri 从路径参数 map 绑定到 obj 结构体。
	BindUri(map[string][]string, any) error
}

// --- 验证器 ---

// StructValidator 结构体验证接口。
type StructValidator interface {
	// ValidateStruct 验证结构体，验证通过返回 nil。
	ValidateStruct(any) error
	// Engine 返回底层的验证引擎。
	Engine() any
}

// Validator 全局验证器实例。
var Validator StructValidator = &defaultValidator{}

// Validate 调用全局验证器验证结构体。
func Validate(obj any) error {
	if Validator == nil {
		return nil
	}
	return Validator.ValidateStruct(obj)
}

// --- 内置绑定器实例 ---

var (
	// JSON 基于 JSON body 的绑定器。
	JSON BindingBody = jsonBinding{}
	// XML 基于 XML body 的绑定器。
	XML BindingBody = xmlBinding{}
	// Form 基于 Form 表单的绑定器（包含 query 和 post form）。
	Form Binding = formBinding{}
	// Query 基于 URL query 参数的绑定器。
	Query Binding = queryBinding{}
	// Header 基于 HTTP header 的绑定器。
	Header Binding = headerBinding{}
	// Uri 基于 URI 路径参数的绑定器。
	Uri BindingUri = uriBinding{}
)

// Default 根据 HTTP 方法和 Content-Type 返回合适的绑定器。
func Default(method, contentType string) Binding {
	if method == http.MethodGet {
		return Form
	}

	switch contentType {
	case MIMEJSON:
		return JSON
	case MIMEXML, MIMEXML2:
		return XML
	case MIMEMultipartPOSTForm:
		return Form
	default: // case MIMEPOSTForm:
		return Form
	}
}

// --- JSON 绑定 ---

// jsonBinding 基于 JSON body 的绑定器。
type jsonBinding struct{}

func (jsonBinding) Name() string {
	return "json"
}

func (jsonBinding) Bind(req *http.Request, obj any) error {
	if req == nil || req.Body == nil {
		return errors.New("invalid request")
	}
	return decodeJSON(req.Body, obj)
}

func (jsonBinding) BindBody(body []byte, obj any) error {
	return decodeJSON(bytes.NewReader(body), obj)
}

// decodeJSON 从 reader 解码 JSON 到 obj，并验证。
func decodeJSON(r io.Reader, obj any) error {
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(obj); err != nil {
		return err
	}
	return Validate(obj)
}

// --- XML 绑定 ---

// xmlBinding 基于 XML body 的绑定器。
type xmlBinding struct{}

func (xmlBinding) Name() string {
	return "xml"
}

func (xmlBinding) Bind(req *http.Request, obj any) error {
	if req == nil || req.Body == nil {
		return errors.New("invalid request")
	}
	return decodeXML(req.Body, obj)
}

func (xmlBinding) BindBody(body []byte, obj any) error {
	return decodeXML(bytes.NewReader(body), obj)
}

// decodeXML 从 reader 解码 XML 到 obj，并验证。
func decodeXML(r io.Reader, obj any) error {
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(obj); err != nil {
		return err
	}
	return Validate(obj)
}

// --- Form 绑定 ---

// defaultMemory 表单解析的最大内存（32MB）。
const defaultMemory = 32 << 20

// formBinding 基于 Form 表单的绑定器（包含 query 和 post form）。
type formBinding struct{}

func (formBinding) Name() string {
	return "form"
}

func (formBinding) Bind(req *http.Request, obj any) error {
	if err := req.ParseForm(); err != nil {
		return err
	}
	if err := req.ParseMultipartForm(defaultMemory); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return err
	}
	if err := mapForm(obj, req.Form); err != nil {
		return err
	}
	return Validate(obj)
}

// --- Query 绑定 ---

// queryBinding 基于 URL query 参数的绑定器。
type queryBinding struct{}

func (queryBinding) Name() string {
	return "query"
}

func (queryBinding) Bind(req *http.Request, obj any) error {
	if err := mapForm(obj, req.URL.Query()); err != nil {
		return err
	}
	return Validate(obj)
}

// --- Header 绑定 ---

// headerBinding 基于 HTTP header 的绑定器。
type headerBinding struct{}

func (headerBinding) Name() string {
	return "header"
}

func (headerBinding) Bind(req *http.Request, obj any) error {
	if err := mapHeader(obj, req.Header); err != nil {
		return err
	}
	return Validate(obj)
}

// headerSource HTTP header 数据源。
type headerSource map[string][]string

var _ setter = headerSource(nil)

// TrySet 从 header 数据源设置值，key 转为 Canonical MIME 格式。
func (hs headerSource) TrySet(value reflect.Value, field reflect.StructField, key string, opt setOptions) (bool, error) {
	return setByForm(value, field, hs, textproto.CanonicalMIMEHeaderKey(key), opt)
}

// --- URI 绑定 ---

// uriBinding 基于 URI 路径参数的绑定器。
type uriBinding struct{}

func (uriBinding) Name() string {
	return "uri"
}

func (uriBinding) BindUri(m map[string][]string, obj any) error {
	if err := mapURI(obj, m); err != nil {
		return err
	}
	return Validate(obj)
}

// --- Form 映射核心逻辑 ---

// 错误定义。
var (
	// errUnknownType 未知类型，无法设置值。
	errUnknownType = errors.New("unknown type")
)

// mapURI 将 URI 路径参数映射到结构体。
// 使用 `uri` 标签匹配字段名。
func mapURI(ptr any, m map[string][]string) error {
	return mapFormByTag(ptr, m, "uri")
}

// mapForm 将表单数据映射到结构体。
// 使用 `form` 标签匹配字段名。
func mapForm(ptr any, form map[string][]string) error {
	return mapFormByTag(ptr, form, "form")
}

// mapHeader 将 HTTP header 映射到结构体。
// 使用 `header` 标签匹配字段名。
func mapHeader(ptr any, h map[string][]string) error {
	return mappingByPtr(ptr, headerSource(h), "header")
}

// mapFormByTag 按指定标签将 map[string][]string 映射到结构体。
func mapFormByTag(ptr any, form map[string][]string, tag string) error {
	ptrVal := reflect.ValueOf(ptr)
	var pointed any
	if ptrVal.Kind() == reflect.Ptr {
		ptrVal = ptrVal.Elem()
		pointed = ptrVal.Interface()
	}
	// 如果目标本身是 map[string]string 或 map[string][]string，直接填充
	if ptrVal.Kind() == reflect.Map && ptrVal.Type().Key().Kind() == reflect.String {
		if pointed != nil {
			ptr = pointed
		}
		return setFormMap(ptr, form)
	}

	return mappingByPtr(ptr, formSource(form), tag)
}

// setter 尝试为结构体字段设置值的接口。
type setter interface {
	// TrySet 尝试设置值，返回是否已设置及错误。
	TrySet(value reflect.Value, field reflect.StructField, key string, opt setOptions) (bool, error)
}

// formSource 表单数据源。
type formSource map[string][]string

var _ setter = formSource(nil)

// TrySet 从表单数据源设置值。
func (form formSource) TrySet(value reflect.Value, field reflect.StructField, key string, opt setOptions) (bool, error) {
	return setByForm(value, field, form, key, opt)
}

// setOptions 字段设置选项。
type setOptions struct {
	isDefaultExists bool   // 是否有默认值
	defaultValue    string // 默认值
}

// mappingByPtr 通过反射遍历指针指向的结构体，逐字段设置值。
func mappingByPtr(ptr any, s setter, tag string) error {
	_, err := mapping(reflect.ValueOf(ptr), emptyField, s, tag)
	return err
}

var emptyField = reflect.StructField{}

// mapping 递归遍历结构体字段进行值映射。
func mapping(value reflect.Value, field reflect.StructField, s setter, tag string) (bool, error) {
	// 跳过显式忽略的字段
	if field.Tag.Get(tag) == "-" {
		return false, nil
	}

	vKind := value.Kind()

	// 处理指针类型
	if vKind == reflect.Ptr {
		var isNew bool
		vPtr := value
		if value.IsNil() {
			isNew = true
			vPtr = reflect.New(value.Type().Elem())
		}
		isSet, err := mapping(vPtr.Elem(), field, s, tag)
		if err != nil {
			return false, err
		}
		if isNew && isSet {
			value.Set(vPtr)
		}
		return isSet, nil
	}

	// 非匿名结构体，尝试直接设置值
	if vKind != reflect.Struct || !field.Anonymous {
		ok, err := tryToSetValue(value, field, s, tag)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}

	// 递归处理结构体字段
	if vKind == reflect.Struct {
		tValue := value.Type()
		var isSet bool
		for i := 0; i < value.NumField(); i++ {
			sf := tValue.Field(i)
			if sf.PkgPath != "" && !sf.Anonymous { // 未导出字段
				continue
			}
			ok, err := mapping(value.Field(i), sf, s, tag)
			if err != nil {
				return false, err
			}
			isSet = isSet || ok
		}
		return isSet, nil
	}
	return false, nil
}

// tryToSetValue 尝试从数据源设置单个字段的值。
func tryToSetValue(value reflect.Value, field reflect.StructField, s setter, tag string) (bool, error) {
	var tagValue string
	var setOpt setOptions

	tagValue = field.Tag.Get(tag)
	tagValue, opts := head(tagValue, ",")

	// 标签为空时使用字段名
	if tagValue == "" {
		tagValue = field.Name
	}
	if tagValue == "" {
		return false, nil
	}

	// 解析标签选项
	var opt string
	for len(opts) > 0 {
		opt, opts = head(opts, ",")
		if k, v := head(opt, "="); k == "default" {
			setOpt.isDefaultExists = true
			setOpt.defaultValue = v
		}
	}

	return s.TrySet(value, field, tagValue, setOpt)
}

// setByForm 从 map[string][]string 数据源设置字段值。
func setByForm(value reflect.Value, field reflect.StructField, form map[string][]string, tagValue string, opt setOptions) (bool, error) {
	vs, ok := form[tagValue]
	if !ok && !opt.isDefaultExists {
		return false, nil
	}

	switch value.Kind() {
	case reflect.Slice:
		if len(vs) == 0 {
			if !opt.isDefaultExists {
				return false, nil
			}
			vs = strings.Split(opt.defaultValue, ",")
		} else if len(vs) == 1 && strings.Contains(vs[0], ",") {
			// 单值含逗号时，自动按逗号分割（适用于 query 参数 tags=a,b,c 的场景）
			vs = strings.Split(vs[0], ",")
		}
		return true, setSlice(vs, value, field, opt)
	default:
		var val string
		if !ok || len(vs) == 0 || (len(vs) > 0 && vs[0] == "") {
			val = opt.defaultValue
		} else if len(vs) > 0 {
			val = vs[0]
		}
		return true, setWithProperType(val, value, field, opt)
	}
}

// setWithProperType 根据目标类型设置值。
// 布尔类型使用 cast.ToBoolE，Duration 类型使用 cast.ToDurationE，
// 整数和浮点类型保留 strconv 以支持位宽溢出检查。
func setWithProperType(val string, value reflect.Value, field reflect.StructField, opt setOptions) error {
	// 字符串类型不去除空格，保留原始数据
	if value.Kind() != reflect.String {
		val = strings.TrimSpace(val)
	}

	switch value.Kind() {
	case reflect.Int:
		return setIntField(val, 0, value)
	case reflect.Int8:
		return setIntField(val, 8, value)
	case reflect.Int16:
		return setIntField(val, 16, value)
	case reflect.Int32:
		return setIntField(val, 32, value)
	case reflect.Int64:
		// time.Duration 底层是 int64
		switch value.Interface().(type) {
		case time.Duration:
			return setTimeDuration(val, value)
		}
		return setIntField(val, 64, value)
	case reflect.Uint:
		return setUintField(val, 0, value)
	case reflect.Uint8:
		return setUintField(val, 8, value)
	case reflect.Uint16:
		return setUintField(val, 16, value)
	case reflect.Uint32:
		return setUintField(val, 32, value)
	case reflect.Uint64:
		return setUintField(val, 64, value)
	case reflect.Bool:
		return setBoolField(val, value)
	case reflect.Float32:
		return setFloatField(val, 32, value)
	case reflect.Float64:
		return setFloatField(val, 64, value)
	case reflect.String:
		value.SetString(val)
	case reflect.Struct:
		switch value.Interface().(type) {
		case time.Time:
			return setTimeField(val, field, value)
		case multipart.FileHeader:
			return nil
		}
		// 其他结构体尝试 JSON 解析
		return json.Unmarshal([]byte(val), value.Addr().Interface())
	case reflect.Map:
		return json.Unmarshal([]byte(val), value.Addr().Interface())
	case reflect.Ptr:
		if !value.Elem().IsValid() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return setWithProperType(val, value.Elem(), field, opt)
	default:
		return errUnknownType
	}
	return nil
}

// setIntField 设置有符号整数字段。
// 保留 strconv.ParseInt 以支持位宽溢出检查。
func setIntField(val string, bitSize int, field reflect.Value) error {
	if val == "" {
		val = "0"
	}
	intVal, err := strconv.ParseInt(val, 10, bitSize)
	if err == nil {
		field.SetInt(intVal)
	}
	return err
}

// setUintField 设置无符号整数字段。
// 保留 strconv.ParseUint 以支持位宽溢出检查。
func setUintField(val string, bitSize int, field reflect.Value) error {
	if val == "" {
		val = "0"
	}
	uintVal, err := strconv.ParseUint(val, 10, bitSize)
	if err == nil {
		field.SetUint(uintVal)
	}
	return err
}

// setBoolField 设置布尔字段。
// 使用 cast.ToBoolE 进行类型转换。
func setBoolField(val string, field reflect.Value) error {
	if val == "" {
		field.SetBool(false)
		return nil
	}
	b, err := cast.ToBoolE(val)
	if err != nil {
		return err
	}
	field.SetBool(b)
	return nil
}

// setFloatField 设置浮点字段。
// 保留 strconv.ParseFloat 以支持位宽溢出检查。
func setFloatField(val string, bitSize int, field reflect.Value) error {
	if val == "" {
		val = "0.0"
	}
	floatVal, err := strconv.ParseFloat(val, bitSize)
	if err == nil {
		field.SetFloat(floatVal)
	}
	return err
}

// setTimeField 设置 time.Time 字段。
// 支持通过 `time_format` 标签指定格式，默认 RFC3339。
func setTimeField(val string, structField reflect.StructField, value reflect.Value) error {
	timeFormat := structField.Tag.Get("time_format")
	if timeFormat == "" {
		timeFormat = time.RFC3339
	}

	if val == "" {
		value.Set(reflect.ValueOf(time.Time{}))
		return nil
	}

	// 支持 unix 时间戳
	switch tf := strings.ToLower(timeFormat); tf {
	case "unix", "unixmilli", "unixmicro", "unixnano":
		tv, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return err
		}
		var t time.Time
		switch tf {
		case "unix":
			t = time.Unix(tv, 0)
		case "unixmilli":
			t = time.UnixMilli(tv)
		case "unixmicro":
			t = time.UnixMicro(tv)
		default:
			t = time.Unix(0, tv)
		}
		value.Set(reflect.ValueOf(t))
		return nil
	}

	l := time.Local
	if isUTC, _ := strconv.ParseBool(structField.Tag.Get("time_utc")); isUTC {
		l = time.UTC
	}
	if locTag := structField.Tag.Get("time_location"); locTag != "" {
		loc, err := time.LoadLocation(locTag)
		if err != nil {
			return err
		}
		l = loc
	}

	t, err := time.ParseInLocation(timeFormat, val, l)
	if err != nil {
		return err
	}
	value.Set(reflect.ValueOf(t))
	return nil
}

// setTimeDuration 设置 time.Duration 字段。
// 使用 cast.ToDurationE 进行类型转换。
func setTimeDuration(val string, value reflect.Value) error {
	if val == "" {
		value.Set(reflect.ValueOf(time.Duration(0)))
		return nil
	}
	d, err := cast.ToDurationE(val)
	if err != nil {
		return err
	}
	value.Set(reflect.ValueOf(d))
	return nil
}

// setSlice 设置切片字段。
func setSlice(vals []string, value reflect.Value, field reflect.StructField, opt setOptions) error {
	slice := reflect.MakeSlice(value.Type(), len(vals), len(vals))
	for i, s := range vals {
		if err := setWithProperType(s, slice.Index(i), field, opt); err != nil {
			return err
		}
	}
	value.Set(slice)
	return nil
}

// setFormMap 将表单数据直接填充到 map 类型目标。
func setFormMap(ptr any, form map[string][]string) error {
	el := reflect.TypeOf(ptr).Elem()

	if el.Kind() == reflect.Slice {
		ptrMap, ok := ptr.(map[string][]string)
		if !ok {
			return errors.New("can not convert to map slices of strings")
		}
		for k, v := range form {
			ptrMap[k] = append(ptrMap[k], v...)
		}
		return nil
	}

	ptrMap, ok := ptr.(map[string]string)
	if !ok {
		return errors.New("can not convert to map of strings")
	}
	for k, v := range form {
		if len(v) > 0 {
			ptrMap[k] = v[len(v)-1] // 取最后一个值
		}
	}
	return nil
}

// head 返回分隔符前的部分和剩余部分。
func head(str, sep string) (head string, tail string) {
	head, tail, _ = strings.Cut(str, sep)
	return head, tail
}

// --- 默认验证器 ---

// defaultValidator 默认验证器，基于 go-playground/validator/v10。
type defaultValidator struct {
	once     sync.Once
	validate *validator.Validate
}

var _ StructValidator = (*defaultValidator)(nil)

// ValidateStruct 验证结构体。
// 支持 struct、指针指向的 struct、以及 slice/array（逐元素验证）。
func (v *defaultValidator) ValidateStruct(obj any) error {
	if obj == nil {
		return nil
	}

	value := reflect.ValueOf(obj)
	switch value.Kind() {
	case reflect.Ptr:
		if value.Elem().Kind() != reflect.Struct {
			return v.ValidateStruct(value.Elem().Interface())
		}
		return v.validateStruct(obj)
	case reflect.Struct:
		return v.validateStruct(obj)
	case reflect.Slice, reflect.Array:
		var errs validator.ValidationErrors
		for i := 0; i < value.Len(); i++ {
			if err := v.ValidateStruct(value.Index(i).Interface()); err != nil {
				var ve validator.ValidationErrors
				if errors.As(err, &ve) {
					errs = append(errs, ve...)
				} else {
					return err
				}
			}
		}
		if len(errs) > 0 {
			return errs
		}
		return nil
	default:
		return nil
	}
}

// validateStruct 验证单个结构体。
func (v *defaultValidator) validateStruct(obj any) error {
	v.lazyInit()
	return v.validate.Struct(obj)
}

// Engine 返回底层的验证引擎。
func (v *defaultValidator) Engine() any {
	v.lazyInit()
	return v.validate
}

// lazyInit 延迟初始化验证器。
func (v *defaultValidator) lazyInit() {
	v.once.Do(func() {
		v.validate = validator.New()
		v.validate.SetTagName("binding")
	})
}
