package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/elbeanio/the_running_man/internal/parser"
)

// InternalUserAgent is the User-Agent of Running Man's own API client -- the
// TUI and anything else in this binary -- so that its requests are not logged
// as an agent's.
const InternalUserAgent = "running-man"

// logCalls records every request from outside Running Man in its own source,
// as one line: method, path and query, status, response size and time.
//
// It is there so the developer can see what an agent is asking for, and how
// much each answer costs its context: the trace endpoints come in levels of
// detail, and whether those are the right levels is judged from this.
//
// Running Man's own client is skipped: the TUI polls several endpoints every
// second, and would bury everything else.
func (s *Server) logCalls(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() == InternalUserAgent {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &countingWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logCall(r, rec.status, rec.bytes, time.Since(start))
	})
}

// logCall appends the line to the buffer directly, with its level set rather
// than parsed: an agent asking for /logs?level=error would otherwise make the
// call itself an error, by the parser's explicit-level rule. A 4xx or 5xx
// answer is warn -- an agent getting errors back is worth noticing, but it is
// not the application failing.
func (s *Server) logCall(r *http.Request, status, bytes int, took time.Duration) {
	if s.buffer == nil {
		return
	}
	level := parser.LevelInfo
	if status >= 400 {
		level = parser.LevelWarn
	}
	msg := fmt.Sprintf("api %s %s %d %s %s", r.Method, r.URL.RequestURI(), status, formatBytes(bytes), formatTook(took))
	s.buffer.Append(&parser.LogEntry{
		Timestamp:  time.Now(),
		Level:      level,
		Source:     "running-man",
		SourceType: "system",
		Message:    parser.SanitiseLine(msg),
		Raw:        msg,
	})
}

// countingWriter records the status and the number of body bytes written.
type countingWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *countingWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *countingWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// formatBytes renders a size for a human: 512 B, 1.5 KB, 3.0 MB.
func formatBytes(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

// formatTook renders a request time without noise digits.
func formatTook(d time.Duration) string {
	if d < time.Millisecond {
		return d.Round(time.Microsecond).String()
	}
	return d.Round(100 * time.Microsecond).String()
}
