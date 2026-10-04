package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func scrollModel(n int) model {
	m := fixtureModel(100, 30)
	m.logs = benchLogs(n)
	m.autoScroll = true
	m.refreshMatches()
	return m
}

func press(m model, k tea.KeyType) model {
	next, _ := m.updateNormalMode(tea.KeyMsg{Type: k})
	return next.(model)
}

// Home set the offset to math.MaxInt and PgUp added a page to it, which
// overflowed to a large negative number -- rendered as the bottom. Found by
// driving the TUI in a terminal: Home then PgUp jumped to the newest output.
func TestHomeThenPgUpStaysAtTheTop(t *testing.T) {
	m := press(scrollModel(500), tea.KeyHome)
	top := m.scrollOffset

	m = press(m, tea.KeyPgUp)
	if m.scrollOffset != top {
		t.Errorf("PgUp from the top moved the offset from %d to %d", top, m.scrollOffset)
	}
	if m.autoScroll {
		t.Error("PgUp from the top jumped back to following the tail")
	}
}

// From Home, PgDn subtracted a page from MaxInt and still rendered as the top,
// so it appeared to do nothing for an unbounded number of presses.
func TestHomeThenPgDnMovesDown(t *testing.T) {
	m := press(scrollModel(500), tea.KeyHome)
	top := m.scrollOffset

	m = press(m, tea.KeyPgDown)
	if want := top - m.pageSize(); m.scrollOffset != want {
		t.Errorf("PgDn from the top: offset %d, want %d", m.scrollOffset, want)
	}
}

// Up past the top kept counting invisibly, so it took as many downs to come
// back as ups had been pressed.
func TestUpPastTheTopDoesNotAccumulate(t *testing.T) {
	m := press(scrollModel(500), tea.KeyHome)
	top := m.scrollOffset
	for i := 0; i < 100; i++ {
		m = press(m, tea.KeyUp)
	}
	m = press(m, tea.KeyDown)
	if m.scrollOffset != top-1 {
		t.Errorf("one down after 100 extra ups: offset %d, want %d", m.scrollOffset, top-1)
	}
}

// With nothing above the screen there is nowhere to scroll, and the view
// stays on the tail.
func TestScrollingWithNothingToScroll(t *testing.T) {
	m := press(scrollModel(3), tea.KeyPgUp)
	if m.scrollOffset != 0 || !m.autoScroll {
		t.Errorf("offset %d autoScroll %v, want 0 and still following", m.scrollOffset, m.autoScroll)
	}
}

// Home pins the view to the oldest line. When the full history arrives after
// leaving the live tail, the top is further up -- and Home used to stop at the
// top of the live window instead of the top of the log.
func TestHomeStaysAtTheTopAsHistoryLoads(t *testing.T) {
	m := press(scrollModel(200), tea.KeyHome)

	next, _ := m.Update(logsMsg{source: m.currentSource(), logs: benchLogs(2000)})
	m = next.(model)

	if m.scrollOffset != m.maxScroll() {
		t.Errorf("after more history loaded, offset %d, want the new top %d", m.scrollOffset, m.maxScroll())
	}
}

// A reply used not to say what it answered, so a slow one could overwrite a
// newer one.
func TestStaleRepliesAreDropped(t *testing.T) {
	t.Run("another tab", func(t *testing.T) {
		m := scrollModel(10)
		next, _ := m.Update(logsMsg{source: "some-other-tab", logs: benchLogs(99)})
		if got := len(next.(model).logs); got != 10 {
			t.Errorf("a reply for another tab replaced this one's logs (%d entries)", got)
		}
	})

	t.Run("live window after leaving the tail", func(t *testing.T) {
		m := scrollModel(10)
		m.autoScroll = false // scrolled up: wants the whole buffer
		next, _ := m.Update(logsMsg{source: m.currentSource(), limited: true, logs: benchLogs(99)})
		if got := len(next.(model).logs); got != 10 {
			t.Errorf("a stale live-window reply replaced the full history (%d entries)", got)
		}
	})
}
