package rest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	weosContext "github.com/wepala/weos/context"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"github.com/wepala/weos/model"
)

//LoadHttpRequestFixture wrapper around the test helper to make it easier to use it with test table
func LoadHttpRequestFixture(filename string) (*http.Request, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	reader := bufio.NewReader(bytes.NewReader(data))
	request, err := http.ReadRequest(reader)
	if err == io.EOF {
		return request, nil
	}

	if err != nil {
		return nil, err
	}

	actualRequest, err := http.NewRequest(request.Method, request.URL.String(), reader)
	if err != nil {
		return nil, err
	}
	return actualRequest, nil
}

//LoadHttpResponseFixture wrapper around the test helper to make it easier to use it with test table
func LoadHttpResponseFixture(filename string, req *http.Request) (*http.Response, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReader(bytes.NewReader(data))
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		return nil, err
	}
	//save response body
	b := new(bytes.Buffer)
	io.Copy(b, resp.Body)
	resp.Body.Close()
	resp.Body = ioutil.NopCloser(b)

	return resp, err
}

// NewRespBodyFromBytes creates an io.ReadCloser from a byte slice that is suitable for use as an
// http response body.
func NewRespBodyFromBytes(body []byte) io.ReadCloser {
	return &dummyReadCloser{bytes.NewReader(body)}
}

type dummyReadCloser struct {
	body io.ReadSeeker
}

func (d *dummyReadCloser) Read(p []byte) (n int, err error) {
	n, err = d.body.Read(p)
	if err == io.EOF {
		d.body.Seek(0, 0)
	}
	return n, err
}

func (d *dummyReadCloser) Close() error {
	return nil
}

type multiWriter struct {
	writers []http.ResponseWriter
}

func (t *multiWriter) Write(p []byte) (n int, err error) {
	for _, w := range t.writers {
		n, err = w.Write(p)
		if err != nil {
			return
		}
		if n != len(p) {
			err = io.ErrShortWrite
			return
		}
	}
	return len(p), nil
}

func (t *multiWriter) Header() http.Header {
	header := make(http.Header)
	for _, w := range t.writers {
		for k, v := range w.Header() {
			for _, val := range v {
				header.Add(k, val)
			}
		}
	}
	return header
}

func (t *multiWriter) WriteHeader(statusCode int) {
	for _, w := range t.writers {
		w.WriteHeader(statusCode)
	}
}

var _ io.StringWriter = (*multiWriter)(nil)

func (t *multiWriter) WriteString(s string) (n int, err error) {
	var p []byte // lazily initialized if/when needed
	for _, w := range t.writers {
		if sw, ok := w.(io.StringWriter); ok {
			n, err = sw.WriteString(s)
		} else {
			if p == nil {
				p = []byte(s)
			}
			n, err = w.Write(p)
		}
		if err != nil {
			return
		}
		if n != len(s) {
			err = io.ErrShortWrite
			return
		}
	}
	return len(s), nil
}

// MultiWriter creates a writer that duplicates its writes to all the
// provided writers, similar to the Unix tee(1) command.
//
// Each write is written to each listed writer, one at a time.
// If a listed writer returns an error, that overall write operation
// stops and returns the error; it does not continue down the list.
func MultiWriter(writers ...http.ResponseWriter) http.ResponseWriter {
	allWriters := make([]http.ResponseWriter, 0, len(writers))
	for _, w := range writers {
		if mw, ok := w.(*multiWriter); ok {
			allWriters = append(allWriters, mw.writers...)
		} else {
			allWriters = append(allWriters, w)
		}
	}
	return &multiWriter{allWriters}
}

//Used to get the logLvl equivalent for an inputted level string
func LogLevels(level string) (log.Lvl, error) {
	switch level {
	case "debug":
		return log.DEBUG, nil
	case "info":
		return log.INFO, nil
	case "warn":
		return log.WARN, nil
	case "error":
		return log.ERROR, nil
	default:
		return log.ERROR, fmt.Errorf("invalid level, expected debug, info, warn or error. got: %s", level)
	}
}

func NewControllerError(message string, err error, code int) *echo.HTTPError {
	return &echo.HTTPError{
		Code:     code,
		Message:  message,
		Internal: err,
	}
}

//NewEtag: This takes in a contentEntity and concatenates the weosID and SequenceID
func NewEtag(entity *model.ContentEntity) string {
	weosID := entity.ID
	SeqNo := entity.SequenceNo
	strSeqNo := strconv.Itoa(int(SeqNo))
	return weosID + "." + strSeqNo
}

//SplitEtag: This takes an Etag and returns the weosID and sequence number
func SplitEtag(Etag string) (string, string) {
	result := strings.Split(Etag, ".")
	if len(result) == 2 {
		return result[0], result[1]
	}
	return "", "-1"
}

func GetContentBySequenceNumber(eventRepository model.EventRepository, id string, sequence_no int64) (*model.ContentEntity, error) {
	entity := &model.ContentEntity{}
	events, err := eventRepository.GetByAggregateAndSequenceRange(id, 0, sequence_no)
	if err != nil {
		return nil, err
	}
	err = entity.ApplyEvents(events)
	return entity, err
}

//ConvertFormToJson: This function is used for "application/x-www-form-urlencoded" content-type to convert req body to json
func ConvertFormToJson(r *http.Request, contentType string) (json.RawMessage, error) {
	var parsedPayload []byte

	switch contentType {
	case "application/x-www-form-urlencoded":
		parsedForm := map[string]interface{}{}

		err := r.ParseForm()
		if err != nil {
			return nil, err
		}

		for k, v := range r.PostForm {
			for _, value := range v {
				parsedForm[k] = value
			}
		}

		parsedPayload, err = json.Marshal(parsedForm)
		if err != nil {
			return nil, err
		}
	case "multipart/form-data":
		parsedForm := map[string]interface{}{}

		err := r.ParseMultipartForm(1024) //Revisit
		if err != nil {
			return nil, err
		}

		for k, v := range r.MultipartForm.Value {
			for _, value := range v {
				parsedForm[k] = value
			}
		}

		parsedPayload, err = json.Marshal(parsedForm)
		if err != nil {
			return nil, err
		}
	}

	return parsedPayload, nil
}

//SplitFilters splits multiple filters into array of filters
func SplitFilters(filters string) []string {
	if filters == "" {
		return nil
	}
	result := strings.Split(filters, "&")
	return result
}

//SplitFilter splits a filter with a single value into the field, operator, value
func SplitFilter(filter string) *FilterProperties {
	var property *FilterProperties
	if filter == "" {
		return nil
	}
	field := strings.Split(filter, "[")
	if len(field) != 3 {
		return nil
	}
	field[1] = strings.Replace(field[1], "]", "", -1)
	operator := strings.Split(field[2], "=")
	if len(operator) != 2 {
		return nil
	}
	operator[0] = strings.Replace(operator[0], "]", "", -1)
	//checks if the there are more than one values specified by checking if there is a comma
	if strings.Contains(operator[1], ",") {
		values := strings.Split(operator[1], ",")
		vals := []interface{}{}
		for _, val := range values {
			vals = append(vals, val)
		}
		property = &FilterProperties{
			Field:    field[1],
			Operator: operator[0],
			Values:   vals,
		}

	} else {
		property = &FilterProperties{
			Field:    field[1],
			Operator: operator[0],
			Value:    operator[1],
		}
	}

	return property
}

//GetJwkUrl fetches the jwk url from the open id connect url
func GetJwkUrl(openIdUrl string) (string, error) {
	//fetches the response from the connect id url
	resp, err := http.Get(openIdUrl)
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected error fetching open id connect url")
	}
	defer resp.Body.Close()
	// reads the body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("unable to read response body: %v", err)
	}
	// unmarshall the body to a struct we can use to find the jwk uri
	var info map[string]interface{}
	err = json.Unmarshal(body, &info)
	if err != nil {
		return "", fmt.Errorf("unexpected error unmarshalling open id connect url response %s", err)
	}
	if info["jwks_uri"] == nil || info["jwks_uri"].(string) == "" {
		return "", fmt.Errorf("no jwks uri found")
	}
	return info["jwks_uri"].(string), nil
}

//JSONMarshal this marshals data without using html.escape
func JSONMarshal(t interface{}) ([]byte, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(t)
	result := bytes.ReplaceAll(buffer.Bytes(), []byte(`\n`), []byte(""))
	result = bytes.ReplaceAll(result, []byte(`"`), []byte(""))
	result = bytes.ReplaceAll(result, []byte(`\r`), []byte(""))
	result = bytes.ReplaceAll(result, []byte(`\t`), []byte(""))
	return result, err
}

//ReturnContextValues pulls out the values stored in the context and adds it to a map to be returned
func ReturnContextValues(ctxt context.Context, operation *openapi3.Operation) (map[interface{}]interface{}, error) {
	contextValues := map[interface{}]interface{}{}
	//all known special weos context names are added
	contextNames := []interface{}{weosContext.ACCEPT, weosContext.ACCOUNT_ID, weosContext.AUTHORIZATION, weosContext.BASIC_RESPONSE, weosContext.CONTENT_TYPE, weosContext.CONTENT_TYPE_RESPONSE, weosContext.ENTITY, weosContext.ENTITY_COLLECTION, weosContext.ENTITY_ID, weosContext.ERROR, weosContext.FILTERS, weosContext.HeaderXAccountID, weosContext.HeaderXLogLevel, weosContext.LOG_LEVEL, weosContext.PAYLOAD, weosContext.REQUEST_ID, weosContext.RESPONSE_PREFIX, weosContext.SEQUENCE_NO, weosContext.SORTS, weosContext.USER_ID, weosContext.WEOS_ID}
	for _, cName := range contextNames {
		if ctxt.Value(cName) != nil {
			contextValues[cName] = ctxt.Value(cName)
		}
	}
	for _, param := range operation.Parameters { //get parameter name to get from the context and add to map
		name := param.Value.Name
		if ctxt.Value(name) == nil {
			if tcontextName, ok := param.Value.ExtensionProps.Extensions[AliasExtension]; ok {
				err := json.Unmarshal(tcontextName.(json.RawMessage), &name)
				if err != nil {
					return nil, NewControllerError(fmt.Sprintf("unexpected error finding parameter alias %s: %s ", name, err), err, http.StatusBadRequest)
				}
			} else {
				return nil, NewControllerError(fmt.Sprintf("unexpected error finding parameter alias %s ", name), nil, http.StatusBadRequest)
			}
		}
		if ctxt.Value(name) == nil {
			return nil, NewControllerError(fmt.Sprintf("unexpected error parameter %s not found ", name), nil, http.StatusBadRequest)
		}
		contextValues[name] = ctxt.Value(name)
	}
	if tcontextParams, ok := operation.ExtensionProps.Extensions[ContextExtension]; ok { //gets context names that was explicitly added to specification file using x-context
		var contextParams map[string]interface{}
		err := json.Unmarshal(tcontextParams.(json.RawMessage), &contextParams)
		if err != nil {
			return nil, NewControllerError(fmt.Sprintf("unexpected error unmarshalling x-context values: %s ", err), err, http.StatusBadRequest)
		}
		for key, _ := range contextParams {
			if ctxt.Value(key) != nil {
				contextValues[key] = ctxt.Value(key)
			}
		}
	}
	return contextValues, nil
}
