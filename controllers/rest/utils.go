package rest

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"github.com/wepala/weos-service/model"
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
	weosID := result[0]
	seqNo := result[1]
	return weosID, seqNo
}
