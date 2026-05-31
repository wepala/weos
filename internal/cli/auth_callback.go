package cli

import (
	"bytes"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"

	"github.com/wepala/weos/v3/application"
)

// authCallbackHandler wraps pericarp's OAuth callback so it works for every
// configured provider and signals first-time signups to the frontend.
//
// Two things the bare pericarp handler doesn't do on its own:
//
//   - Apple sign-in uses response_mode=form_post, so the authorization code and
//     state arrive as an HTTP POST body rather than GET query params. We copy
//     those fields onto the request's query string before delegating, so the
//     downstream handler (which reads r.URL.Query()) is provider-agnostic.
//
//   - We install a new-account flag in the request context (consumed by the
//     decorated AuthenticationService in application.newAccountSignalService).
//     When the callback creates a brand new account, we append ?new_account=1
//     to the redirect Location so the SPA can route the user into onboarding.
//
// The response is buffered so the redirect can be rewritten after the inner
// handler has run and the flag is known; on the JSON error path it's passed
// through untouched.
func authCallbackHandler(inner http.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		r := c.Request()

		// Apple form_post: promote the posted code/state to query params so the
		// inner handler finds them where it expects.
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err == nil {
				q := r.URL.Query()
				for _, field := range []string{"code", "state", "error", "error_description"} {
					if v := r.PostForm.Get(field); v != "" {
						q.Set(field, v)
					}
				}
				r.URL.RawQuery = q.Encode()
			}
		}

		isNew := new(bool)
		r = r.WithContext(application.WithNewAccountFlag(r.Context(), isNew))

		rec := &redirectCapturingWriter{header: http.Header{}}
		inner(rec, r)

		if *isNew {
			rec.appendRedirectQuery("new_account", "1")
		}
		return rec.flush(c.Response())
	}
}

// redirectCapturingWriter buffers a handler's response so a 3xx Location can be
// rewritten before anything reaches the client. It implements just enough of
// http.ResponseWriter for the callback's redirect-or-JSON-error responses.
type redirectCapturingWriter struct {
	header      http.Header
	status      int
	body        bytes.Buffer
	wroteHeader bool
}

func (w *redirectCapturingWriter) Header() http.Header { return w.header }

func (w *redirectCapturingWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
}

func (w *redirectCapturingWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(b)
}

// appendRedirectQuery adds a query param to a buffered redirect's Location.
// It's a no-op when the response isn't a redirect or carries no Location, so a
// JSON error response is never mangled.
func (w *redirectCapturingWriter) appendRedirectQuery(key, value string) {
	if w.status < 300 || w.status >= 400 {
		return
	}
	loc := w.header.Get("Location")
	if loc == "" {
		return
	}
	u, err := url.Parse(loc)
	if err != nil {
		return
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	w.header.Set("Location", u.String())
}

// flush copies the buffered status, headers, and body to the real writer.
func (w *redirectCapturingWriter) flush(dst http.ResponseWriter) error {
	for key, values := range w.header {
		for _, v := range values {
			dst.Header().Add(key, v)
		}
	}
	if !w.wroteHeader {
		w.status = http.StatusOK
	}
	dst.WriteHeader(w.status)
	_, err := dst.Write(w.body.Bytes())
	return err
}
