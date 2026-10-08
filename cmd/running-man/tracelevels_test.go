package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A span as the full-span endpoint returns it, for the span and value levels.
func fixtureFullSpan() *fullSpan {
	d4 := fixtureSpans()[3]
	return &fullSpan{SpanID: d4.SpanID, Name: d4.Name, Kind: "SPAN_KIND_CLIENT", Status: "ok",
		ServiceName: d4.ServiceName, Duration: d4.Duration.String(), StartTime: d4.StartTime,
		Attributes: d4.Attributes}
}

func spanLevelModel(mode Mode) model {
	m := fixtureModel(100, 20)
	m.selectedSource = 2
	m.selectedTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	m.traceSpans = fixtureSpans()
	m.traceCursor = 3 // ChatCompletion
	m.spanFull = fixtureFullSpan()
	m.mode = mode
	m.attrCursor = 0 // input.value
	m.valueKey = "input.value"
	return m
}

func TestGoldenTraceLevels(t *testing.T) {
	assertGolden(t, "span-detail", spanLevelModel(ModeSpanDetail).View())
	assertGolden(t, "value-view", spanLevelModel(ModeValueView).View())
}

// Images inside a value are shown by type and size: a terminal cannot draw
// them, and their base64 is unreadable.
func TestImagePlaceholders(t *testing.T) {
	data := strings.Repeat("A", 4000) // 3000 bytes decoded
	got := withImagePlaceholders(`{"url":"data:image/png;base64,` + data + `"} and data:image/jpeg;base64,QUJD`)
	want := `{"url":"[image/png, 2.9 KB]"} and [image/jpeg, 3 B]`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if strings.Contains(ansi.Strip(spanLevelModel(ModeValueView).View()), "iVBORw0KGgo") {
		t.Error("the value view shows base64")
	}
}

// A span's bar sits at its start within the trace and is as long as it ran.
func TestWaterfall(t *testing.T) {
	start := time.Unix(0, 0)
	end := start.Add(100 * time.Millisecond)
	s := spanDetail{StartTime: start.Add(50 * time.Millisecond), Duration: 25 * time.Millisecond}
	if got := waterfall(s, start, end, 20); got != "          █████     " {
		t.Errorf("bar %q", got)
	}
	// Too short to see is still one cell.
	s.Duration = time.Microsecond
	if got := waterfall(s, start, end, 20); strings.Count(got, "█") != 1 {
		t.Errorf("a tiny span drew %q", got)
	}
}

// A span whose parent is missing is drawn as a root rather than lost.
func TestSpanRows_OrphanIsARoot(t *testing.T) {
	rows := spanRows([]spanDetail{
		{SpanID: "a", ParentSpanID: "gone", Name: "orphan"},
		{SpanID: "b", ParentSpanID: "a", Name: "child"},
	})
	if len(rows) != 2 || rows[0].span.Name != "orphan" || rows[1].prefix == "" {
		t.Errorf("rows = %+v", rows)
	}
}

// Enter goes down a level, fetching the span in full; Esc comes back up one
// level at a time.
func TestTraceLevels_EnterAndEsc(t *testing.T) {
	m := spanLevelModel(ModeTraceDetail)
	m.spanFull = nil
	key := func(k tea.KeyType) tea.Cmd {
		next, cmd := m.updateNormalMode(tea.KeyMsg{Type: k})
		m = next.(model)
		return cmd
	}

	if cmd := key(tea.KeyEnter); m.mode != ModeSpanDetail || cmd == nil {
		t.Fatalf("Enter on a span: mode %v, fetch %v", m.mode, cmd != nil)
	}
	next, _ := m.Update(spanMsg{traceID: m.selectedTraceID, span: *fixtureFullSpan()})
	m = next.(model)
	if m.spanFull == nil {
		t.Fatal("the full span was not taken")
	}
	if key(tea.KeyEnter); m.mode != ModeValueView || m.valueKey != "input.value" {
		t.Fatalf("Enter on an attribute: mode %v key %q", m.mode, m.valueKey)
	}
	key(tea.KeyEsc)
	if m.mode != ModeSpanDetail {
		t.Errorf("Esc from a value: mode %v", m.mode)
	}
	key(tea.KeyEsc)
	if m.mode != ModeTraceDetail {
		t.Errorf("Esc from a span: mode %v", m.mode)
	}
	key(tea.KeyEsc)
	if m.mode != ModeNormal {
		t.Errorf("Esc from a trace: mode %v", m.mode)
	}
}

// ↑/↓ move the cursor through the spans and stop at the ends.
func TestTraceLevels_CursorMoves(t *testing.T) {
	m := spanLevelModel(ModeTraceDetail)
	m.traceCursor = 0
	for range 10 {
		next, _ := m.updateNormalMode(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(model)
	}
	if m.traceCursor != len(m.traceSpans)-1 {
		t.Errorf("cursor %d after 10 downs, want the last span %d", m.traceCursor, len(m.traceSpans)-1)
	}
	if s, _ := m.selectedSpan(); s.Name != "ChatCompletion" {
		t.Errorf("selected %q", s.Name)
	}
}

// Every line of the span level fits its width -- the highlighted attribute
// row was one column over, and wrapped its size onto the next line.
func TestSpanViewLines_FitTheWidth(t *testing.T) {
	sp := fixtureFullSpan()
	for _, w := range []int{40, 60, 98, 140} {
		for cursor := range 2 {
			lines, _ := spanViewLines(*sp, true, sp.StartTime, w, cursor)
			for i, l := range lines {
				if got := displayWidth(l); got > w {
					t.Errorf("width %d, cursor %d: line %d is %d wide: %q", w, cursor, i, got, ansi.Strip(l))
				}
			}
		}
	}
}

// The same for the trace level's span rows and the value level.
func TestTraceAndValueLines_FitTheWidth(t *testing.T) {
	spans := fixtureSpans()
	for _, w := range []int{20, 40, 79, 80, 98, 140} {
		for cursor := -1; cursor < len(spans); cursor++ {
			for i, l := range spanLines(spans, w, cursor) {
				if got := displayWidth(l); got > w {
					t.Errorf("span rows, width %d cursor %d: row %d is %d wide: %q", w, cursor, i, got, ansi.Strip(l))
				}
			}
		}
		for i, l := range valueViewLines("input.value", spans[3].Attributes["input.value"], w) {
			if got := displayWidth(l); got > w {
				t.Errorf("value, width %d: line %d is %d wide", w, i, got)
			}
		}
	}
}

// OpenInference numbers each message in its attribute keys, and a text sort
// put message 10 between 1 and 2: the conversation read out of order.
func TestAttrKeys_NumbersInOrder(t *testing.T) {
	keys := attrKeys(map[string]string{
		"llm.input_messages.10.message.role": "", "llm.input_messages.2.message.role": "",
		"llm.input_messages.1.message.role": "", "input.value": "",
	})
	want := []string{"input.value", "llm.input_messages.1.message.role",
		"llm.input_messages.2.message.role", "llm.input_messages.10.message.role"}
	if strings.Join(keys, " ") != strings.Join(want, " ") {
		t.Errorf("order %v", keys)
	}
}
