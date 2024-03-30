package rest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"io"
	"io/ioutil"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unsafe"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
)

// LoadHttpRequestFixture wrapper around the test helper to make it easier to use it with test table
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

// LoadHttpResponseFixture wrapper around the test helper to make it easier to use it with test table
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

// Used to get the logLvl equivalent for an inputted level string
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

// NewEtag This takes in a contentEntity and concatenates the weosID and SequenceID
func NewEtag(entity BasicResource) string {
	weosID := entity.GetID()
	SeqNo := entity.GetSequenceNo()
	strSeqNo := strconv.Itoa(int(SeqNo))
	return weosID + "." + strSeqNo
}

// SplitEtag This takes an Etag and returns the weosID and sequence number
func SplitEtag(Etag string) (string, string) {
	result := strings.Split(Etag, ".")
	if len(result) == 2 {
		return result[0], result[1]
	}
	return "", "-1"
}

// SplitFilters splits multiple filters into array of filters
func SplitFilters(filters string) []string {
	if filters == "" {
		return nil
	}
	result := strings.Split(filters, "&")
	return result
}

// SplitFilter splits a filter with a single value into the field, operator, value
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

// SplitQueryParameters is used to split key value pair query parameters
func SplitQueryParameters(query string, key string) *QueryProperties {
	var property *QueryProperties
	if query == "" {
		return nil
	}
	field := strings.Split(query, "[")
	if len(field) != 2 {
		return nil
	}
	if field[0] != key {
		return nil
	}
	field[1] = strings.Replace(field[1], "]", "", -1)
	queryProps := strings.Split(field[1], "=")
	if len(queryProps) != 2 {
		return nil
	}
	queryProps[1] = strings.Replace(queryProps[1], "+", " ", -1)
	property = &QueryProperties{
		Value: queryProps[1],
		Field: queryProps[0],
	}

	return property
}

// Deprecated: 06/20/2022 Use GetOpenIDConfig to get the map of the entire config
// GetJwkUrl fetches the jwk url from the open id connect url
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

// GetOpenIDConfig returns map of openID content
func GetOpenIDConfig(openIdUrl string) (map[string]interface{}, error) {
	//fetches the response from the url
	resp, err := http.Get(openIdUrl)
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected error fetching open id connect url")
	}
	defer resp.Body.Close()
	// reads the body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %v", err)
	}
	// unmarshal the body to a struct we can use to find the jwk uri
	var info map[string]interface{}
	err = json.Unmarshal(body, &info)
	return info, err
}

// JSONMarshal this marshals data without using html.escape
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

// ReturnContextValues pulls out all the values stored in the context and adds it to a map to be returned
func ReturnContextValues(ctxt interface{}) map[interface{}]interface{} {
	contextValues := map[interface{}]interface{}{}
	contextKeys := []interface{}{}
	contextV := reflect.ValueOf(ctxt).Elem()
	contextK := reflect.TypeOf(ctxt).Elem()
	if contextK.Kind() == reflect.Struct {
		for i := 0; i < contextV.NumField(); i++ {
			reflectValue := contextV.Field(i)
			reflectValue = reflect.NewAt(reflectValue.Type(), unsafe.Pointer(reflectValue.UnsafeAddr())).Elem()
			reflectField := contextK.Field(i)

			if reflectField.Name == "Context" {
				contextVals := ReturnContextValues(reflectValue.Interface())
				for key, value := range contextVals {
					contextValues[key] = value
				}
			} else if reflectField.Name == "key" {
				contextKeys = append(contextKeys, reflectValue.Interface())
			}
		}
	}
	for _, cKeys := range contextKeys {
		contextValues[cKeys] = ctxt.(context.Context).Value(cKeys)
	}
	return contextValues
}

// ConvertStringToType convert open api schema types to go data types
func ConvertStringToType(desiredType string, format string, value string) (interface{}, error) {
	var temporaryValue interface{}
	var err error
	switch desiredType {
	case "integer":
		temporaryValue, err = strconv.Atoi(value)
		if err == nil {
			//check the format and use that to convert to int32 vs int64
			switch format {
			case "int64":
				temporaryValue = int64(temporaryValue.(int))
			case "int32":
				temporaryValue = int32(temporaryValue.(int))
			}
		}

	case "number":
		tv, terr := strconv.ParseFloat(value, 64)
		if terr == nil {
			//check the format to determine the bit size. Default to 32 if none is specified
			if format != "float" {
				temporaryValue = math.Round(tv*100) / 100
			} else {
				temporaryValue = tv
			}
		}
		err = terr
	case "boolean":
		temporaryValue, err = strconv.ParseBool(value)
	default:
		temporaryValue = value
	}

	return temporaryValue, err
}

// SaveUploadedFiles this is a supporting function for ConvertFormtoJson
func SaveUploadedFiles(uploadFolder map[string]interface{}, file multipart.File, header *multipart.FileHeader) error {
	if float64(header.Size) > uploadFolder["limit"].(float64) {
		return fmt.Errorf("maximum file size allowed: %s, uploaded file size: %s", strconv.FormatFloat(uploadFolder["limit"].(float64), 'f', -1, 64), strconv.FormatFloat(float64(header.Size), 'f', -1, 64))
	}

	buf := bytes.NewBuffer(nil)
	if _, err := io.Copy(buf, file); err != nil {
		return err
	}

	//Checks if folder exists and creates it if not
	_, err := os.Stat(uploadFolder["folder"].(string))
	if os.IsNotExist(err) {
		err := os.MkdirAll(uploadFolder["folder"].(string), os.ModePerm)
		if err != nil {
			return err
		}
	}

	filePath := uploadFolder["folder"].(string) + "/" + header.Filename

	//Checks if file exists in folder and creates it if not
	_, err = os.Stat(filePath)

	if os.IsNotExist(err) {
		os.WriteFile(filePath, buf.Bytes(), os.ModePerm)
	} else if err == nil {
		return fmt.Errorf("the file : %s, already exists on path : %s. Please rename the file and try again", header.Filename, uploadFolder["folder"])
	}

	return nil
}

func ResolveResponseType(header string, content openapi3.Content) string {
	//TODO process the header string to make a list and order it based on the quality score @see https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Accept
	if header == "" {
		return ""
	}
	mimeTypes := strings.Split(header, ",")
	for _, mimeType := range mimeTypes {
		for contentType, _ := range content {
			mimeType = strings.ReplaceAll(mimeType, " ", "")
			mimeType = strings.ReplaceAll(mimeType, "+", "")

			match, _ := regexp.MatchString("^"+mimeType, strings.ReplaceAll(contentType, "+", ""))
			if match {
				return contentType
			}
		}
	}
	return ""
}
