package main

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elbeanio/the_running_man/internal/parser"
)

// The trace detail view, a level at a time, as the API serves them:
//
//	trace  -- every span as a tree with a waterfall bar; a cursor picks one
//	span   -- that span's attributes and events, fetched in full
//	value  -- one attribute value, whole, wrapped and scrollable
//
// Esc steps back a level. Kept deliberately plain: a generic attribute
// browser, with images shown by type and size, since a terminal cannot draw
// them and their base64 is unreadable.

// Two more levels below ModeTraceDetail.
const (
	ModeSpanDetail Mode = iota + 100
	ModeValueView
)

// waterfallMinWidth is the narrowest content width that gets waterfall bars;
// below it the names and durations need the room.
const waterfallMinWidth = 80

// spanRow is one span in tree order, with its drawn prefix.
type spanRow struct {
	span   spanDetail
	prefix string
}

// spanRows flattens the spans into tree order: parents before children,
// siblings by start time. A span whose parent is not in the trace is a root,
// as the API treats it -- an exporter can drop a parent.
func spanRows(spans []spanDetail) []spanRow {
	ids := make(map[string]bool, len(spans))
	for _, s := range spans {
		ids[s.SpanID] = true
	}
	children := map[string][]spanDetail{}
	var roots []spanDetail
	for _, s := range spans {
		if !ids[s.ParentSpanID] {
			roots = append(roots, s)
		} else {
			children[s.ParentSpanID] = append(children[s.ParentSpanID], s)
		}
	}
	byStart := func(ss []spanDetail) {
		sort.SliceStable(ss, func(i, j int) bool { return ss[i].StartTime.Before(ss[j].StartTime) })
	}
	byStart(roots)

	var rows []spanRow
	var walk func(s spanDetail, prefix, childPrefix string)
	walk = func(s spanDetail, prefix, childPrefix string) {
		rows = append(rows, spanRow{span: s, prefix: prefix})
		kids := children[s.SpanID]
		byStart(kids)
		for i, k := range kids {
			if i == len(kids)-1 {
				walk(k, childPrefix+"└─ ", childPrefix+"   ")
			} else {
				walk(k, childPrefix+"├─ ", childPrefix+"│  ")
			}
		}
	}
	for _, r := range roots {
		walk(r, "", "")
	}
	return rows
}

// traceBounds is the earliest start and latest end across the spans.
func traceBounds(spans []spanDetail) (time.Time, time.Time) {
	var start, end time.Time
	for i, s := range spans {
		e := s.EndTime
		if e.IsZero() {
			e = s.StartTime.Add(s.Duration)
		}
		if i == 0 || s.StartTime.Before(start) {
			start = s.StartTime
		}
		if i == 0 || e.After(end) {
			end = e
		}
	}
	return start, end
}

var (
	waterfallStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("63"))  // blue-violet
	waterfallErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203")) // red
	cursorRowStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("57"))
	dimStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// waterfall draws a span's place in the trace as a bar width cells wide.
func waterfall(s spanDetail, start, end time.Time, width int) string {
	total := end.Sub(start)
	if width <= 0 || total <= 0 {
		return strings.Repeat(" ", max(0, width))
	}
	from := int(float64(s.StartTime.Sub(start)) / float64(total) * float64(width))
	length := int(float64(s.Duration) / float64(total) * float64(width))
	from = min(max(from, 0), width-1)
	length = min(max(length, 1), width-from)
	return strings.Repeat(" ", from) + strings.Repeat("█", length) + strings.Repeat(" ", width-from-length)
}

// spanLines renders the span rows: status, tree, name, duration, and a
// waterfall bar when there is room. The row at cursor is highlighted.
func spanLines(spans []spanDetail, width, cursor int) []string {
	rows := spanRows(spans)
	start, end := traceBounds(spans)

	const durW = 10
	nameW := 0
	for _, r := range rows {
		nameW = max(nameW, displayWidth(r.prefix)+displayWidth(r.span.Name)+2)
	}
	barW := 0
	if width >= waterfallMinWidth {
		nameW = min(nameW, width*45/100)
		barW = width - nameW - durW - 2
	} else {
		nameW = width - durW - 1
	}

	lines := make([]string, len(rows))
	for i, r := range rows {
		mark := "✓"
		if spanFailed(r.span.Status) {
			mark = "✗"
		}
		head := pad(mark+" "+r.prefix+parser.SanitiseLine(r.span.Name), nameW) + " " +
			pad(r.span.Duration.String(), durW)
		var line string
		if barW > 0 {
			bar := waterfall(r.span, start, end, barW)
			if i == cursor {
				line = cursorRowStyle.Render(head + " " + bar)
			} else if spanFailed(r.span.Status) {
				line = waterfallErrorStyle.Render(head) + " " + waterfallErrorStyle.Render(bar)
			} else {
				line = head + " " + waterfallStyle.Render(bar)
			}
		} else {
			line = head
			if i == cursor {
				line = cursorRowStyle.Render(head)
			} else if spanFailed(r.span.Status) {
				line = waterfallErrorStyle.Render(head)
			}
		}
		lines[i] = line
	}
	return lines
}

// --- images ----------------------------------------------------------------

// imagePattern finds a base64 data URI image, anywhere in a value: an LLM
// span's input carries the images sent to the model inline.
var imagePattern = regexp.MustCompile(`data:image/([A-Za-z0-9.+-]+);base64,[A-Za-z0-9+/=]+`)

// withImagePlaceholders replaces each inline image with its type and size.
// A terminal cannot draw it, and its base64 is unreadable.
func withImagePlaceholders(v string) string {
	return imagePattern.ReplaceAllStringFunc(v, func(m string) string {
		kind := imagePattern.FindStringSubmatch(m)[1]
		_, data, _ := strings.Cut(m, ",")
		return fmt.Sprintf("[image/%s, %s]", kind, humanBytes(len(data)*3/4))
	})
}

// humanBytes renders a size: 512 B, 1.5 KB, 3.0 MB.
func humanBytes(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

// --- the span level --------------------------------------------------------

// fullSpan is a span as /traces/{id}/spans/{span_id} returns it: nothing cut.
type fullSpan struct {
	SpanID      string            `json:"span_id"`
	Name        string            `json:"name"`
	Kind        string            `json:"kind"`
	Status      string            `json:"status"`
	StatusCode  string            `json:"status_code"`
	ServiceName string            `json:"service_name"`
	Duration    string            `json:"duration"`
	StartTime   time.Time         `json:"start_time"`
	Attributes  map[string]string `json:"attributes"`
	Events      []struct {
		Name       string            `json:"name"`
		Timestamp  time.Time         `json:"timestamp"`
		Attributes map[string]string `json:"attributes"`
	} `json:"events"`
}

// spanMsg delivers a span in full.
type spanMsg struct {
	traceID string
	span    fullSpan
}

func fetchSpan(apiURL, traceID, spanID string) tea.Cmd {
	return func() tea.Msg {
		var resp struct {
			Span fullSpan `json:"span"`
		}
		if err := getJSON(apiURL+"/traces/"+url.PathEscape(traceID)+"/spans/"+url.PathEscape(spanID), &resp); err != nil {
			return errMsg{err}
		}
		return spanMsg{traceID: traceID, span: resp.Span}
	}
}

// attrKeys is a span's attribute keys in order.
func attrKeys(attrs map[string]string) []string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return naturalLess(keys[i], keys[j]) })
	return keys
}

// naturalLess orders strings with runs of digits compared as numbers, so
// llm.input_messages.2 comes before llm.input_messages.10.
func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		da, db := digitRun(a), digitRun(b)
		if da > 0 && db > 0 {
			na, nb := strings.TrimLeft(a[:da], "0"), strings.TrimLeft(b[:db], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			a, b = a[da:], b[db:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

// digitRun is the length of the run of ASCII digits at the start of s.
func digitRun(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

// spanViewLines renders the span level. The attribute at cursor is
// highlighted; the returned index is that attribute's line, to keep in view.
func spanViewLines(sp fullSpan, loaded bool, traceStart time.Time, width, cursor int) ([]string, int) {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("57"))
	lines := []string{title.Render(" Span: " + parser.SanitiseLine(sp.Name) + " ")}
	status := sp.Status
	if spanFailed(status) {
		status = waterfallErrorStyle.Render("error")
	}
	lines = append(lines, truncate(fmt.Sprintf("%s · %s · %s · %s · +%s into the trace",
		sp.ServiceName, strings.TrimPrefix(sp.Kind, "SPAN_KIND_"), status, sp.Duration,
		sp.StartTime.Sub(traceStart).Round(time.Millisecond)), width))
	if !loaded {
		lines = append(lines, dimStyle.Render("loading the full span…"))
	}
	lines = append(lines, "")

	keys := attrKeys(sp.Attributes)
	lines = append(lines, fmt.Sprintf("Attributes (%d):", len(keys)))
	keyW := 0
	for _, k := range keys {
		keyW = max(keyW, displayWidth(k))
	}
	keyW = min(keyW, width*40/100)
	const sizeW = 10
	// Two for the cursor mark, two between key and value, one before the size.
	valW := max(0, width-keyW-sizeW-5)

	cursorLine := -1
	for i, k := range keys {
		v := sp.Attributes[k]
		first, _, multi := strings.Cut(withImagePlaceholders(v), "\n")
		size := ""
		if multi || displayWidth(first) > valW {
			size = humanBytes(len(v))
		}
		line := "  " + pad(k, keyW) + "  " + pad(parser.SanitiseLine(first), valW) + " " + dimStyle.Render(fmt.Sprintf("%*s", sizeW, size))
		if i == cursor {
			cursorLine = len(lines)
			line = cursorRowStyle.Render("› " + pad(k, keyW) + "  " + pad(parser.SanitiseLine(first), valW) + " " + fmt.Sprintf("%*s", sizeW, size))
		}
		lines = append(lines, line)
	}

	if len(sp.Events) > 0 {
		lines = append(lines, "", fmt.Sprintf("Events (%d):", len(sp.Events)))
		for _, ev := range sp.Events {
			var parts []string
			for _, k := range attrKeys(ev.Attributes) {
				parts = append(parts, k+"="+withImagePlaceholders(ev.Attributes[k]))
			}
			lines = append(lines, truncate("  "+parser.SanitiseLine(ev.Name+"  "+strings.Join(parts, " ")), width))
		}
	}
	return lines, cursorLine
}

// --- the value level -------------------------------------------------------

// valueViewLines renders one attribute value whole: images as placeholders,
// sanitised, wrapped to the width.
func valueViewLines(key, value string, width int) []string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("57"))
	lines := []string{title.Render(" " + key + " "), dimStyle.Render(humanBytes(len(value))), ""}
	for _, raw := range strings.Split(withImagePlaceholders(value), "\n") {
		line := parser.SanitiseLine(raw)
		for displayWidth(line) > width && width > 0 {
			cut := truncateRunes(line, width)
			lines = append(lines, cut)
			line = line[len(cut):]
		}
		lines = append(lines, line)
	}
	return lines
}

// truncateRunes is the longest prefix of s that fits width columns, without
// an ellipsis, for wrapping.
func truncateRunes(s string, width int) string {
	w := 0
	for i, r := range s {
		rw := displayWidth(string(r))
		if w+rw > width {
			if i == 0 {
				return string(r)
			}
			return s[:i]
		}
		w += rw
	}
	return s
}

// window shows height rows of lines from offset, clamped, padded.
func window(lines []string, height, offset int) string {
	if height <= 0 {
		return ""
	}
	offset = min(max(offset, 0), max(0, len(lines)-height))
	end := min(offset+height, len(lines))
	shown := append([]string(nil), lines[offset:end]...)
	for len(shown) < height {
		shown = append(shown, "")
	}
	return strings.Join(shown, "\n")
}

// keepVisible returns the offset that keeps line in a window of height.
func keepVisible(offset, line, height int) int {
	switch {
	case line < 0:
		return offset
	case line < offset:
		return line
	case line >= offset+height:
		return line - height + 1
	}
	return offset
}

// --- the model side --------------------------------------------------------

// selectedSpan is the span under the trace view's cursor.
func (m model) selectedSpan() (spanDetail, bool) {
	rows := spanRows(m.traceSpans)
	if m.traceCursor < 0 || m.traceCursor >= len(rows) {
		return spanDetail{}, false
	}
	return rows[m.traceCursor].span, true
}

// spanForView is the span to show at the span level: the full one once it has
// arrived, the trimmed one until then.
func (m model) spanForView() (fullSpan, bool) {
	if m.spanFull != nil {
		return *m.spanFull, true
	}
	s, ok := m.selectedSpan()
	if !ok {
		return fullSpan{}, false
	}
	return fullSpan{SpanID: s.SpanID, Name: s.Name, Kind: s.Kind, Status: s.Status, StatusCode: s.StatusCode,
		ServiceName: s.ServiceName, Duration: s.Duration.String(), StartTime: s.StartTime, Attributes: s.Attributes}, false
}

// updateTraceLevels handles the keys that differ in the trace, span and value
// views. It reports false for keys it leaves to updateNormalMode.
func (m model) updateTraceLevels(key string) (model, tea.Cmd, bool) {
	h := m.contentHeight()
	switch m.mode {
	case ModeTraceDetail:
		switch key {
		case "up", "k":
			m.traceCursor = max(0, m.traceCursor-1)
		case "down", "j":
			m.traceCursor = min(len(m.traceSpans)-1, m.traceCursor+1)
		case "enter":
			s, ok := m.selectedSpan()
			if !ok {
				return m, nil, true
			}
			m.mode, m.spanFull, m.attrCursor, m.spanScroll = ModeSpanDetail, nil, 0, 0
			return m, fetchSpan(m.apiURL, m.selectedTraceID, s.SpanID), true
		default:
			return m, nil, false
		}
		// Keep the cursor's row in view: it follows the header lines.
		m.traceDetailScrollOffset = keepVisible(m.traceDetailScrollOffset, traceDetailHeaderRows+m.traceCursor, h)
		return m, nil, true

	case ModeSpanDetail:
		sp, _ := m.spanForView()
		keys := attrKeys(sp.Attributes)
		switch key {
		case "esc", "escape":
			m.mode = ModeTraceDetail
		case "up", "k":
			m.attrCursor = max(0, m.attrCursor-1)
		case "down", "j":
			m.attrCursor = min(len(keys)-1, m.attrCursor+1)
		case "pgup":
			m.attrCursor = max(0, m.attrCursor-h)
		case "pgdown":
			m.attrCursor = min(len(keys)-1, m.attrCursor+h)
		case "home":
			m.attrCursor = 0
		case "end":
			m.attrCursor = len(keys) - 1
		case "enter":
			if m.attrCursor >= 0 && m.attrCursor < len(keys) {
				m.mode, m.valueKey, m.valueScroll = ModeValueView, keys[m.attrCursor], 0
			}
			return m, nil, true
		default:
			return m, nil, false
		}
		start, _ := traceBounds(m.traceSpans)
		_, cursorLine := spanViewLines(sp, m.spanFull != nil, start, m.contentWidth(), m.attrCursor)
		m.spanScroll = keepVisible(m.spanScroll, cursorLine, h)
		return m, nil, true

	case ModeValueView:
		sp, _ := m.spanForView()
		total := len(valueViewLines(m.valueKey, sp.Attributes[m.valueKey], m.contentWidth()))
		maxScroll := max(0, total-h)
		switch key {
		case "esc", "escape":
			m.mode = ModeSpanDetail
		case "up", "k":
			m.valueScroll = max(0, m.valueScroll-1)
		case "down", "j":
			m.valueScroll = min(maxScroll, m.valueScroll+1)
		case "pgup":
			m.valueScroll = max(0, m.valueScroll-h)
		case "pgdown":
			m.valueScroll = min(maxScroll, m.valueScroll+h)
		case "home":
			m.valueScroll = 0
		case "end":
			m.valueScroll = maxScroll
		default:
			return m, nil, false
		}
		return m, nil, true
	}
	return m, nil, false
}

// renderTraceLevel draws the span or value level.
func (m model) renderTraceLevel(height, width int) string {
	sp, loaded := m.spanForView()
	start, _ := traceBounds(m.traceSpans)
	switch m.mode {
	case ModeSpanDetail:
		lines, _ := spanViewLines(sp, loaded, start, width, m.attrCursor)
		return window(lines, height, m.spanScroll)
	case ModeValueView:
		return window(valueViewLines(m.valueKey, sp.Attributes[m.valueKey], width), height, m.valueScroll)
	}
	return ""
}
