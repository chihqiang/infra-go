package binding

import (
	"bytes"
	"encoding/xml"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func jsonRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func formRequest(target, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestDefaultSelectsBinder(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		contentType string
		want        Binding
	}{
		{name: "get uses query", method: http.MethodGet, contentType: MIMEJSON, want: Query},
		{name: "head uses query", method: http.MethodHead, want: Query},
		{name: "json", method: http.MethodPost, contentType: MIMEJSON, want: JSON},
		{name: "json with charset", method: http.MethodPost, contentType: "application/json; charset=utf-8", want: JSON},
		{name: "xml", method: http.MethodPost, contentType: MIMEXML, want: XML},
		{name: "text xml", method: http.MethodPost, contentType: MIMEXML2, want: XML},
		{name: "multipart", method: http.MethodPost, contentType: MIMEMultipartPOSTForm, want: FormMultipart},
		{name: "urlencoded", method: http.MethodPost, contentType: MIMEPOSTForm, want: Form},
		{name: "unknown falls back to form", method: http.MethodPost, contentType: "text/plain", want: Form},
	}

	for _, test := range tests {
		if got := Default(test.method, test.contentType); got != test.want {
			t.Errorf("%s: binder = %s, want %s", test.name, got.Name(), test.want.Name())
		}
	}
}

type jsonPayload struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestJSONBindAndBindBody(t *testing.T) {
	body := `{"name":"ada","age":36}`

	var fromRequest jsonPayload
	if err := JSON.Bind(jsonRequest(body), &fromRequest); err != nil {
		t.Fatal(err)
	}
	if fromRequest.Name != "ada" || fromRequest.Age != 36 {
		t.Fatalf("payload = %+v", fromRequest)
	}

	var fromBody jsonPayload
	if err := JSON.BindBody([]byte(body), &fromBody); err != nil {
		t.Fatal(err)
	}
	if fromBody != fromRequest {
		t.Fatalf("BindBody = %+v, want %+v", fromBody, fromRequest)
	}
}

func TestJSONRejectsBadInput(t *testing.T) {
	for _, body := range []string{
		`{"name":`, "", `{"name":"a"}{"name":"b"}`,
		`{"name":"a"}}`, `{"name":"a"}]`,
	} {
		err := JSON.Bind(jsonRequest(body), &jsonPayload{})
		if err == nil {
			t.Fatalf("body %q should fail", body)
		}
		if statusOf(t, err) != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want 400", body, statusOf(t, err))
		}
	}

	if err := JSON.Bind(jsonRequest(`{}`), nil); err == nil {
		t.Fatal("nil destination should fail")
	}
}

func TestDefaultValidatorInitializationIsConcurrentSafe(t *testing.T) {
	previous := Validator
	Validator = &defaultValidator{}
	defer func() { Validator = previous }()

	payload := struct {
		Name string `json:"name" binding:"required"`
	}{}

	const goroutines = 32
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			target := payload
			if err := JSON.BindBody([]byte(`{"name":"ada"}`), &target); err != nil {
				t.Errorf("bind: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestJSONTooLargeBodyMapsTo413(t *testing.T) {
	req := jsonRequest(`{"name":"ada"}`)
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 4)

	err := JSON.Bind(req, &jsonPayload{})
	if err == nil {
		t.Fatal("oversized body should fail")
	}
	if statusOf(t, err) != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", statusOf(t, err))
	}
}

type xmlPayload struct {
	XMLName xml.Name `xml:"payload"`
	Name    string   `xml:"name"`
}

func TestXMLBind(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`<payload><name>ada</name></payload>`))
	req.Header.Set("Content-Type", MIMEXML2)

	var payload xmlPayload
	if err := XML.Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "ada" {
		t.Fatalf("name = %q", payload.Name)
	}

	if err := XML.BindBody([]byte(`<payload>`), &xmlPayload{}); err == nil {
		t.Fatal("malformed xml should fail")
	}

	trailing := httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader(`<payload><name>ada</name></payload><payload><name>bob</name></payload>`))
	trailing.Header.Set("Content-Type", MIMEXML)
	if err := XML.Bind(trailing, &xmlPayload{}); err == nil {
		t.Fatal("trailing XML document should fail")
	}
}

type formPayload struct {
	Name   string   `form:"name"`
	Limit  int      `form:"limit"`
	Tags   []string `form:"tag"`
	Active bool     `form:"active"`
}

func TestFormMergesQueryAndBody(t *testing.T) {
	var payload formPayload
	req := formRequest("/?name=query&limit=5", "name=body&tag=go&tag=http&active=true")

	if err := Form.Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "body" {
		t.Fatalf("name = %q, want the body value", payload.Name)
	}
	if payload.Limit != 5 || !payload.Active {
		t.Fatalf("payload = %+v", payload)
	}
	if len(payload.Tags) != 2 || payload.Tags[1] != "http" {
		t.Fatalf("tags = %v", payload.Tags)
	}
}

func TestFormPostIgnoresQuery(t *testing.T) {
	var payload formPayload
	if err := FormPost.Bind(formRequest("/?name=query", "limit=3"), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "" || payload.Limit != 3 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestMultipartBindReadsValuesAndFiles(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("name", "ada"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("attachment", "report.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("content")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Name       string                  `form:"name"`
		Attachment *multipart.FileHeader   `form:"attachment"`
		Extra      []*multipart.FileHeader `form:"extra"`
	}
	req := httptest.NewRequest(http.MethodPost, "/?name=query", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	if err := FormMultipart.Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "ada" {
		t.Fatalf("name = %q", payload.Name)
	}
	if payload.Attachment != nil {
		t.Fatalf("single file header should only be set for slice fields, got %+v", payload.Attachment)
	}
	if len(payload.Extra) != 0 {
		t.Fatalf("unexpected extra files: %v", payload.Extra)
	}

	var withFile struct {
		Attachment []*multipart.FileHeader `form:"attachment"`
	}
	var second bytes.Buffer
	secondWriter := multipart.NewWriter(&second)
	part, err = secondWriter.CreateFormFile("attachment", "report.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("content")); err != nil {
		t.Fatal(err)
	}
	if err := secondWriter.Close(); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/", &second)
	req.Header.Set("Content-Type", secondWriter.FormDataContentType())
	if err := FormMultipart.Bind(req, &withFile); err != nil {
		t.Fatal(err)
	}
	if len(withFile.Attachment) != 1 || withFile.Attachment[0].Filename != "report.txt" {
		t.Fatalf("file = %+v", withFile.Attachment)
	}
}

type EmbeddedQuery struct {
	City string `form:"city"`
}

func TestQueryBindTypes(t *testing.T) {
	var payload struct {
		EmbeddedQuery
		Name     string        `form:"name"`
		Attempts uint8         `form:"attempts"`
		Ratio    float64       `form:"ratio"`
		Timeout  time.Duration `form:"timeout"`
		Deadline time.Time     `form:"deadline"`
		Optional *int          `form:"optional"`
		Missing  string        `form:"missing"`
		Skipped  string        `form:"-"`
		Alias    string        `json:"alias"`
	}
	req := httptest.NewRequest(http.MethodGet,
		"/?name=ada&attempts=3&ratio=0.5&timeout=90s&deadline=2024-05-06&optional=7&city=paris&alias=a&skipped=x", nil)

	if err := Query.Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "ada" || payload.Attempts != 3 || payload.Ratio != 0.5 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Timeout != 90*time.Second {
		t.Fatalf("timeout = %v", payload.Timeout)
	}
	if payload.Deadline.Format("2006-01-02") != "2024-05-06" {
		t.Fatalf("deadline = %v", payload.Deadline)
	}
	if payload.Optional == nil || *payload.Optional != 7 {
		t.Fatalf("optional = %v", payload.Optional)
	}
	if payload.City != "paris" {
		t.Fatalf("embedded field = %q", payload.City)
	}
	if payload.Skipped != "" {
		t.Fatalf("skipped field was set: %q", payload.Skipped)
	}
	if payload.Alias != "a" {
		t.Fatalf("json tag fallback = %q", payload.Alias)
	}
}

func TestQueryBindRejectsInvalidValues(t *testing.T) {
	type target struct {
		Count int `form:"count"`
	}
	req := httptest.NewRequest(http.MethodGet, "/?count=abc", nil)

	err := Query.Bind(req, &target{})
	if err == nil || statusOf(t, err) != http.StatusBadRequest {
		t.Fatalf("error = %v, want 400", err)
	}
}

func TestHeaderBindIsCaseInsensitive(t *testing.T) {
	var payload struct {
		Token string `header:"X-Token"`
		Agent string `header:"user-agent"`
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-token", "secret")
	req.Header.Set("User-Agent", "rest-test")

	if err := Header.Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Token != "secret" || payload.Agent != "rest-test" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestPathBind(t *testing.T) {
	var payload struct {
		ID     string `path:"id"`
		Action string `path:"action"`
	}
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	req.SetPathValue("id", "42")

	if err := Path.Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID != "42" {
		t.Fatalf("id = %q", payload.ID)
	}

}

func TestBindRejectsUnsupportedMediaType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("plain"))
	req.Header.Set("Content-Type", "text/plain")

	err := Bind(req, &jsonPayload{})
	if statusOf(t, err) != http.StatusUnsupportedMediaType {
		t.Fatalf("error = %v, want 415", err)
	}
}

func TestBindUsesQueryWithoutBody(t *testing.T) {
	var payload formPayload
	req := httptest.NewRequest(http.MethodGet, "/?name=ada&limit=1", nil)

	if err := Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "ada" || payload.Limit != 1 {
		t.Fatalf("payload = %+v", payload)
	}
}

type validatedPayload struct {
	Name string `json:"name" binding:"required"`
}

func TestValidationTags(t *testing.T) {
	err := Bind(jsonRequest(`{}`), &validatedPayload{})
	if err == nil {
		t.Fatal("missing required field should fail validation")
	}
	if statusOf(t, err) != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", statusOf(t, err))
	}

	if err := Bind(jsonRequest(`{"name":"ada"}`), &validatedPayload{}); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
}

type selfValidating struct {
	Name string `json:"name"`
}

func (s *selfValidating) Validate() error {
	if s.Name == "" {
		return errors.New("name is empty")
	}
	return nil
}

func TestValidateHook(t *testing.T) {
	if err := Bind(jsonRequest(`{}`), &selfValidating{}); err == nil {
		t.Fatal("Validate() should have rejected the payload")
	}
	if err := Bind(jsonRequest(`{"name":"ada"}`), &selfValidating{}); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
}

func TestSignedHelperParsesFormBody(t *testing.T) {
	values := url.Values{"name": {"ada"}}
	req := formRequest("/", values.Encode())

	var payload formPayload
	if err := Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "ada" {
		t.Fatalf("name = %q", payload.Name)
	}
}

func statusOf(t *testing.T, err error) int {
	t.Helper()

	var bindErr *Error
	if !errors.As(err, &bindErr) {
		t.Fatalf("error = %T, want *binding.Error", err)
	}
	return bindErr.StatusCode()
}

func TestBinderNames(t *testing.T) {
	binders := map[string]Binding{
		"json":                JSON,
		"xml":                 XML,
		"form":                Form,
		"form-urlencoded":     FormPost,
		"multipart/form-data": FormMultipart,
		"query":               Query,
		"header":              Header,
		"path":                Path,
	}
	for want, binder := range binders {
		if got := binder.Name(); got != want {
			t.Errorf("Name() = %q, want %q", got, want)
		}
	}
}

func TestPackageHelpers(t *testing.T) {
	queryDst := struct {
		Name string `form:"name"`
	}{}
	if err := BindQuery(httptest.NewRequest(http.MethodGet, "/?name=ada", nil), &queryDst); err != nil {
		t.Fatal(err)
	}
	if queryDst.Name != "ada" {
		t.Fatalf("BindQuery name = %q", queryDst.Name)
	}

	headerDst := struct {
		Token string `header:"X-Token"`
	}{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Token", "secret")
	if err := BindHeader(req, &headerDst); err != nil {
		t.Fatal(err)
	}
	if headerDst.Token != "secret" {
		t.Fatalf("BindHeader token = %q", headerDst.Token)
	}

	pathDst := struct {
		ID string `path:"id"`
	}{}
	pathReq := httptest.NewRequest(http.MethodGet, "/users/1", nil)
	pathReq.SetPathValue("id", "1")
	if err := BindPath(pathReq, &pathDst); err != nil {
		t.Fatal(err)
	}
	if pathDst.ID != "1" {
		t.Fatalf("BindPath id = %q", pathDst.ID)
	}
}

func TestBindErrors(t *testing.T) {
	bindErr := &Error{Status: http.StatusTeapot, Err: errors.New("boom")}
	if bindErr.Error() != "boom" {
		t.Fatalf("Error() = %q", bindErr.Error())
	}
	if !errors.Is(bindErr, bindErr.Err) {
		t.Fatal("Unwrap() did not expose the cause")
	}
	if bindErr.StatusCode() != http.StatusTeapot {
		t.Fatalf("StatusCode() = %d", bindErr.StatusCode())
	}

	empty := &Error{}
	if empty.StatusCode() != http.StatusBadRequest {
		t.Fatalf("default StatusCode() = %d, want 400", empty.StatusCode())
	}
	if empty.Error() == "" {
		t.Fatal("Error() should not be empty")
	}
}

type textValue string

func (t *textValue) UnmarshalText(data []byte) error {
	if string(data) == "bad" {
		return errors.New("bad text value")
	}
	*t = textValue(strings.ToUpper(string(data)))
	return nil
}

func TestMappingBranches(t *testing.T) {
	type inner struct {
		City string `form:"city"`
	}
	var payload struct {
		Pointer  *inner        `form:"location"`
		Text     textValue     `form:"text"`
		Duration time.Duration `form:"duration"`
		Bytes    []byte        `form:"bytes"`
		When     time.Time     `form:"when"`
	}
	req := httptest.NewRequest(http.MethodGet,
		"/?city=paris&text=abc&duration=2s&bytes=raw&when=2024-01-02T03:04:05Z", nil)

	if err := Query.Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Text != "ABC" {
		t.Fatalf("text = %q", payload.Text)
	}
	if payload.Duration != 2*time.Second || string(payload.Bytes) != "raw" {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.When.UTC().Format(time.RFC3339) != "2024-01-02T03:04:05Z" {
		t.Fatalf("when = %v", payload.When)
	}
}

func TestTextUnmarshalerFailure(t *testing.T) {
	var payload struct {
		Text textValue `form:"text"`
	}
	err := Query.Bind(httptest.NewRequest(http.MethodGet, "/?text=bad", nil), &payload)
	if err == nil || statusOf(t, err) != http.StatusBadRequest {
		t.Fatalf("error = %v, want 400", err)
	}
}

func TestInvalidTimeAndUnsupportedType(t *testing.T) {
	var withTime struct {
		When time.Time `form:"when"`
	}
	if err := Query.Bind(httptest.NewRequest(http.MethodGet, "/?when=yesterday", nil), &withTime); err == nil {
		t.Fatal("invalid time should fail")
	}

	var unsupported struct {
		Value struct{} `form:"value"`
	}
	if err := Query.Bind(httptest.NewRequest(http.MethodGet, "/?value=x", nil), &unsupported); err == nil {
		t.Fatal("struct field from a scalar should fail")
	}
}

func TestBindEdgeCases(t *testing.T) {
	if err := Bind(nil, &jsonPayload{}); err == nil {
		t.Fatal("nil request should fail")
	}

	noType := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=ada"))
	var payload formPayload
	if err := Bind(noType, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "ada" {
		t.Fatalf("name = %q", payload.Name)
	}

	brokenType := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	brokenType.Header.Set("Content-Type", "application/json; charset")
	if err := Bind(brokenType, &jsonPayload{}); err == nil {
		t.Fatal("unparsable content type should fall through to unsupported-format handling")
	}
}

func TestJSONBindNilBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Body = nil
	req.Header.Set("Content-Type", MIMEJSON)

	if err := JSON.Bind(req, &jsonPayload{}); err == nil {
		t.Fatal("nil body should fail")
	}
	if err := JSON.BindBody([]byte(`{"name":`), &jsonPayload{}); err == nil {
		t.Fatal("malformed BindBody input should fail")
	}
}

func TestFormBindErrors(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=ada"))
	req.Header.Set("Content-Type", MIMEMultipartPOSTForm)

	if err := Form.Bind(req, &formPayload{}); err == nil {
		t.Fatal("invalid multipart body should fail")
	}

	multipartReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not multipart"))
	multipartReq.Header.Set("Content-Type", MIMEMultipartPOSTForm)
	if err := FormMultipart.Bind(multipartReq, &formPayload{}); err == nil {
		t.Fatal("invalid multipart bind should fail")
	}
}

func TestXMLBindBodySuccess(t *testing.T) {
	var payload xmlPayload
	if err := XML.BindBody([]byte(`<payload><name>ada</name></payload>`), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "ada" {
		t.Fatalf("name = %q", payload.Name)
	}
}

func TestBooleanParsing(t *testing.T) {
	cases := map[string]bool{
		"1":     true,
		"true":  true,
		"YES":   true,
		"on":    true,
		"0":     false,
		"false": false,
		"off":   false,
		"":      false,
	}
	for raw, want := range cases {
		var payload struct {
			Flag bool `form:"flag"`
		}
		req := httptest.NewRequest(http.MethodGet, "/?flag="+raw, nil)
		if err := Query.Bind(req, &payload); err != nil {
			t.Fatalf("value %q: %v", raw, err)
		}
		if payload.Flag != want {
			t.Errorf("value %q parsed as %v, want %v", raw, payload.Flag, want)
		}
	}

	invalid := httptest.NewRequest(http.MethodGet, "/?flag=maybe", nil)
	if err := Query.Bind(invalid, &struct {
		Flag bool `form:"flag"`
	}{}); err == nil {
		t.Fatal("invalid boolean should fail")
	}
}

func TestMappingRequiresStructDestination(t *testing.T) {
	if err := mapByTag(nil, nil, "form"); err == nil {
		t.Fatal("nil destination should fail")
	}
	if err := mapByTag(42, map[string][]string{"a": {"b"}}, "form"); err != nil {
		t.Fatalf("non-struct destination should be ignored, got %v", err)
	}
	if err := mapWithFiles(nil, nil, nil, "form"); err == nil {
		t.Fatal("nil destination should fail for files mapping")
	}
}

func TestSliceOfPointers(t *testing.T) {
	var payload struct {
		Values []*int `form:"v"`
	}
	req := httptest.NewRequest(http.MethodGet, "/?v=1&v=2", nil)

	if err := Query.Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Values) != 2 || *payload.Values[1] != 2 {
		t.Fatalf("values = %v", payload.Values)
	}
}

func TestBindRejectsTypedNilDestination(t *testing.T) {
	type target struct {
		Name string `json:"name"`
	}
	var typedNil *target

	if err := Query.Bind(httptest.NewRequest(http.MethodGet, "/?name=ada", nil), typedNil); err == nil {
		t.Fatal("typed nil destination should fail")
	} else if statusOf(t, err) != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", statusOf(t, err))
	}

	if err := JSON.Bind(jsonRequest(`{"name":"ada"}`), typedNil); err == nil {
		t.Fatal("typed nil destination should fail for JSON")
	}
}

func TestNestedStructBinding(t *testing.T) {
	type profile struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	var payload struct {
		Profile profile  `json:"profile"`
		Owner   *profile `json:"owner"`
		Absent  *profile `json:"absent"`
	}

	req := httptest.NewRequest(http.MethodGet,
		"/?profile.name=ada&profile.age=36&owner.name=bob", nil)
	if err := Query.Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Profile.Name != "ada" || payload.Profile.Age != 36 {
		t.Fatalf("profile = %+v", payload.Profile)
	}
	if payload.Owner == nil || payload.Owner.Name != "bob" {
		t.Fatalf("owner = %+v", payload.Owner)
	}
	if payload.Absent != nil {
		t.Fatalf("absent = %+v, want nil", payload.Absent)
	}
}

func TestTimeVariantsAcceptDateOnly(t *testing.T) {
	var payload struct {
		Times []time.Time `json:"times"`
		Ptr   *time.Time  `json:"ptr"`
	}
	req := httptest.NewRequest(http.MethodGet, "/?times=2024-01-02&times=2024-01-03&ptr=2024-01-04", nil)
	if err := Query.Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Times) != 2 || payload.Times[0].Format("2006-01-02") != "2024-01-02" ||
		payload.Times[1].Format("2006-01-02") != "2024-01-03" {
		t.Fatalf("times = %v", payload.Times)
	}
	if payload.Ptr == nil || payload.Ptr.Format("2006-01-02") != "2024-01-04" {
		t.Fatalf("ptr = %v", payload.Ptr)
	}
}

func TestNestedStructDoesNotAcceptFlatKey(t *testing.T) {
	var payload struct {
		Profile struct {
			Name string `json:"name"`
		} `json:"profile"`
	}
	err := Query.Bind(httptest.NewRequest(http.MethodGet, "/?profile=ada", nil), &payload)
	if err == nil || statusOf(t, err) != http.StatusBadRequest {
		t.Fatalf("error = %v, want 400", err)
	}
}

func TestFormTooLargeBodyMapsTo413(t *testing.T) {
	req := formRequest("/", "name=ada&limit=3")
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 4)

	err := Form.Bind(req, &formPayload{})
	if err == nil {
		t.Fatal("oversized form body should fail")
	}
	if statusOf(t, err) != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", statusOf(t, err))
	}
}
