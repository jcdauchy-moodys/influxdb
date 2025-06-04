package httpd

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/influxdata/influxql"
)

// responseLogger is wrapper of http.ResponseWriter that keeps track of its HTTP status
// code and body size
type responseLogger struct {
	w      http.ResponseWriter
	status int
	size   int
}

func (l *responseLogger) CloseNotify() <-chan bool {
	if notifier, ok := l.w.(http.CloseNotifier); ok {
		return notifier.CloseNotify()
	}
	// needed for response recorder for testing
	return make(<-chan bool)
}

func (l *responseLogger) Header() http.Header {
	return l.w.Header()
}

func (l *responseLogger) Flush() {
	l.w.(http.Flusher).Flush()
}

func (l *responseLogger) Write(b []byte) (int, error) {
	if l.status == 0 {
		// Set status if WriteHeader has not been called
		l.status = http.StatusOK
	}

	size, err := l.w.Write(b)
	l.size += size
	return size, err
}

func (l *responseLogger) WriteHeader(s int) {
	l.w.WriteHeader(s)
	l.status = s
}

func (l *responseLogger) Status() int {
	if l.status == 0 {
		// This can happen if we never actually write data, but only set response headers.
		l.status = http.StatusOK
	}
	return l.status
}

func (l *responseLogger) Size() int {
	return l.size
}

// redact any occurrence of a password parameter, 'p'
func redactPassword(r *http.Request) {
	q := r.URL.Query()
	if p := q.Get("p"); p != "" {
		q.Set("p", "[REDACTED]")
		r.URL.RawQuery = q.Encode()
	}
}

// Common Log Format: http://en.wikipedia.org/wiki/Common_Log_Format

// buildLogLine creates a logfmt formatted log line
// This matches the existing InfluxDB logging format style
func buildLogLine(l *responseLogger, r *http.Request, start time.Time) string {

	redactPassword(r)

	username := parseUsername(r)

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	if xff := r.Header["X-Forwarded-For"]; xff != nil {
		addrs := append(xff, host)
		host = strings.Join(addrs, ",")
	}

	uri := r.URL.RequestURI()
	referer := r.Referer()
	userAgent := r.UserAgent()
	requestID := r.Header.Get("Request-Id")
	durationMicros := int64(time.Since(start) / time.Microsecond)

	// Generate a log_id similar to InfluxDB's format (simplified version)
	// In a real implementation, this should use the same ID generator as the main logger
	logID := fmt.Sprintf("httpd_%s", requestID[len(requestID)-8:]) // Use last 8 chars of request ID

	// Build logfmt formatted string matching InfluxDB style
	logParts := []string{
		fmt.Sprintf("ts=%s", start.Format("2006-01-02T15:04:05.000000Z")),
		"lvl=debug",
		fmt.Sprintf("msg=\"HTTP request\""),
		fmt.Sprintf("log_id=%s", logID),
		"service=httpd",
		fmt.Sprintf("client_ip=%s", host),
		fmt.Sprintf("method=%s", r.Method),
		fmt.Sprintf("path=%s", uri),
		fmt.Sprintf("protocol=%q", r.Proto),
		fmt.Sprintf("status=%d", l.Status()),
		fmt.Sprintf("response_size=%d", l.Size()),
		fmt.Sprintf("duration_us=%d", durationMicros),
	}

	// Add optional fields only if they have values
	if username != "" {
		logParts = append(logParts, fmt.Sprintf("username=%s", username))
	}
	if referer != "" {
		logParts = append(logParts, fmt.Sprintf("referer=%q", referer))
	}
	if userAgent != "" {
		logParts = append(logParts, fmt.Sprintf("user_agent=%q", userAgent))
	}
	if requestID != "" {
		logParts = append(logParts, fmt.Sprintf("request_id=%s", requestID))
	}

	// Handle POST form data if present
	if r.Method == "POST" && len(r.PostForm) > 0 {
		formFields := make([]string, 0, len(r.PostForm))
		for k, values := range r.PostForm {
			if k == "p" || k == "P" {
				// Redact password fields
				r.PostForm.Set(k, "[REDACTED]")
				values = r.PostForm[k]
			}
			// Join multiple values with comma
			joined := strings.Join(values, ",")
			formFields = append(formFields, fmt.Sprintf("%s=%s", k, joined))
		}
		if len(formFields) > 0 {
			logParts = append(logParts, fmt.Sprintf("form_data=%q", strings.Join(formFields, "&")))
		}
	}

	return strings.Join(logParts, " ")
}

// detect detects the first presence of a non blank string and returns it
func detect(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// parses the username either from the url or auth header
func parseUsername(r *http.Request) string {
	var (
		username = ""
		url      = r.URL
	)

	// get username from the url if passed there
	if url.User != nil {
		if name := url.User.Username(); name != "" {
			username = name
		}
	}

	// Try to get the username from the query param 'u'
	q := url.Query()
	if u := q.Get("u"); u != "" {
		username = u
	}

	// Try to get it from the authorization header if set there
	if username == "" {
		if u, _, ok := r.BasicAuth(); ok {
			username = u
		}
	}
	return username
}

// sanitize redacts passwords from query string for logging.
func sanitize(r *http.Request) {
	values := r.URL.Query()
	for i, q := range values["q"] {
		values["q"][i] = influxql.Sanitize(q)
	}
	r.URL.RawQuery = values.Encode()
}
