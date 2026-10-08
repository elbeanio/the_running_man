package tracing

import (
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The trace API comes in three levels of detail, so a caller -- an agent
// especially -- spends its context only on what it asks for: a list of trace
// summaries; one trace with its spans, attribute values cut short; one span in
// full. A single trace from an LLM application carried 3 MB of attributes,
// mostly whole conversations and the images in them.

// TraceSummary describes one trace: enough to choose which to open.
type TraceSummary struct {
	TraceID    string    `json:"trace_id"`
	RootSpan   string    `json:"root_span"`
	Summary    string    `json:"summary,omitempty"`
	StartTime  time.Time `json:"start_time"`
	Duration   string    `json:"duration"`
	SpanCount  int       `json:"span_count"`
	ErrorCount int       `json:"error_count"`
	Status     string    `json:"status"` // "error" when any span failed, else "ok"
	Services   []string  `json:"services"`
}

// TraceFilters select traces. A trace matches a span-level filter when any of
// its spans does, and is then summarised whole.
type TraceFilters struct {
	Since       time.Duration
	ServiceName string
	SpanName    string
	Status      string // "error" or "ok"; empty for any
	Limit       int    // newest N traces; 0 for all
}

// summaryKeys are the root-span attributes tried, in order, for a trace's
// summary: the request as the application saw it.
var summaryKeys = []string{"input.value", "http.route", "http.target", "db.statement"}

// maxSummary is the summary's length in characters.
const maxSummary = 120

// Summarise describes one trace from its spans.
func Summarise(spans []*SpanEntry) TraceSummary {
	if len(spans) == 0 {
		return TraceSummary{Services: []string{}}
	}
	ids := make(map[string]bool, len(spans))
	for _, sp := range spans {
		ids[sp.SpanID] = true
	}

	var root *SpanEntry
	start, end := spans[0].StartTime, spans[0].EndTime
	services := map[string]bool{}
	errors := 0
	for _, sp := range spans {
		// The root is a span whose parent is not in the trace -- usually it has
		// none, but an exporter can drop a parent -- the earliest if several.
		if !ids[sp.ParentSpanID] && (root == nil || sp.StartTime.Before(root.StartTime)) {
			root = sp
		}
		if sp.StartTime.Before(start) {
			start = sp.StartTime
		}
		if sp.EndTime.After(end) {
			end = sp.EndTime
		}
		if sp.ServiceName != "" {
			services[sp.ServiceName] = true
		}
		if sp.Status == "error" {
			errors++
		}
	}
	if root == nil { // a cycle of parents; pick the earliest span
		root = spans[0]
		for _, sp := range spans {
			if sp.StartTime.Before(root.StartTime) {
				root = sp
			}
		}
	}

	s := TraceSummary{
		TraceID:    root.TraceID,
		RootSpan:   root.Name,
		Summary:    summaryOf(root),
		StartTime:  start,
		Duration:   end.Sub(start).String(),
		SpanCount:  len(spans),
		ErrorCount: errors,
		Status:     "ok",
		Services:   make([]string, 0, len(services)),
	}
	if errors > 0 {
		s.Status = "error"
	}
	for name := range services {
		s.Services = append(s.Services, name)
	}
	sort.Strings(s.Services)
	return s
}

// summaryOf is the first non-empty summary attribute, first line only, cut to
// maxSummary characters.
func summaryOf(root *SpanEntry) string {
	for _, k := range summaryKeys {
		v := strings.TrimSpace(root.Attributes[k])
		if v == "" {
			continue
		}
		line, _, _ := strings.Cut(v, "\n")
		line = strings.TrimSpace(line)
		if utf8.RuneCountInString(line) > maxSummary {
			r := []rune(line)
			line = string(r[:maxSummary-1]) + "…"
		}
		return line
	}
	return ""
}

// Traces returns the summaries of the traces matching f, newest first.
func (s *SpanStorage) Traces(f TraceFilters) []TraceSummary {
	s.mu.RLock()
	byTrace := map[string][]*SpanEntry{}
	for _, sp := range s.spans {
		byTrace[sp.TraceID] = append(byTrace[sp.TraceID], sp)
	}
	s.mu.RUnlock()

	cutoff := time.Now().Add(-f.Since)
	result := []TraceSummary{}
	for _, spans := range byTrace {
		if !anySpan(spans, func(sp *SpanEntry) bool { return f.Since <= 0 || !sp.StartTime.Before(cutoff) }) ||
			!anySpan(spans, func(sp *SpanEntry) bool { return f.ServiceName == "" || sp.ServiceName == f.ServiceName }) ||
			!anySpan(spans, func(sp *SpanEntry) bool { return f.SpanName == "" || strings.Contains(sp.Name, f.SpanName) }) {
			continue
		}
		sum := Summarise(spans)
		if f.Status != "" && sum.Status != f.Status {
			continue
		}
		result = append(result, sum)
	}

	slices.SortFunc(result, func(a, b TraceSummary) int { return b.StartTime.Compare(a.StartTime) })
	if f.Limit > 0 && len(result) > f.Limit {
		result = result[:f.Limit]
	}
	return result
}

func anySpan(spans []*SpanEntry, match func(*SpanEntry) bool) bool {
	return slices.ContainsFunc(spans, match)
}

// SpansOf returns one trace's spans in start order, or nil if it is unknown.
func (s *SpanStorage) SpansOf(traceID string) []*SpanEntry {
	spans := s.GetTrace(traceID)
	slices.SortStableFunc(spans, func(a, b *SpanEntry) int { return a.StartTime.Compare(b.StartTime) })
	return spans
}

// Span returns one span in full, or nil.
func (s *SpanStorage) Span(traceID, spanID string) *SpanEntry {
	for _, sp := range s.GetTrace(traceID) {
		if sp.SpanID == spanID {
			return sp
		}
	}
	return nil
}

// TrimmedSpan is a span with long attribute values cut short, and the original
// sizes of the ones that were.
type TrimmedSpan struct {
	Span      SpanEntry
	Truncated map[string]int // attribute key (events as events.N.key) → original bytes
}

// MarshalJSON is the span's own JSON, plus "truncated" when anything was cut.
func (t TrimmedSpan) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(t.Span)
	if err != nil || len(t.Truncated) == 0 {
		return b, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m["truncated"], err = json.Marshal(t.Truncated); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// Trim copies sp with every attribute value, and every event attribute value,
// cut to at most limit bytes on a character boundary. The stored span is not
// changed.
func Trim(sp *SpanEntry, limit int) TrimmedSpan {
	out := TrimmedSpan{Span: *sp}
	note := func(key string, n int) {
		if out.Truncated == nil {
			out.Truncated = map[string]int{}
		}
		out.Truncated[key] = n
	}
	out.Span.Attributes = trimValues(sp.Attributes, limit, func(k string, n int) { note(k, n) })
	if len(sp.Events) > 0 {
		out.Span.Events = make([]SpanEvent, len(sp.Events))
		for i, ev := range sp.Events {
			out.Span.Events[i] = ev
			prefix := "events." + strconv.Itoa(i) + "."
			out.Span.Events[i].Attributes = trimValues(ev.Attributes, limit, func(k string, n int) { note(prefix+k, n) })
		}
	}
	return out
}

func trimValues(attrs map[string]string, limit int, cut func(key string, original int)) map[string]string {
	if attrs == nil {
		return nil
	}
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		if len(v) <= limit {
			out[k] = v
			continue
		}
		i := limit
		for i > 0 && !utf8.RuneStart(v[i]) {
			i--
		}
		out[k] = v[:i] + "…"
		cut(k, len(v))
	}
	return out
}
