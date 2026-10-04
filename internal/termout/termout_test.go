package termout

import "testing"

// The point of the package: while a TUI owns the screen, nothing else writes to
// it. internal/tracing printed a line per OTLP request, so an instrumented
// application corrupted the display continuously.
func TestSilenceSuppressesAndRestores(t *testing.T) {
	if Quiet() {
		t.Fatal("writes should not start suppressed")
	}

	restore := Silence()
	if !Quiet() {
		t.Error("Silence did not suppress writes")
	}
	if w := Writer(); w == nil {
		t.Error("Writer returned nil while suppressed")
	}

	restore()
	if Quiet() {
		t.Error("restore did not re-enable writes")
	}
}

// Suppression has to hold for the goroutines that actually do the writing --
// API handlers, OTLP handlers, process watchers -- none of which share a lock
// with the TUI.
func TestSilenceIsSafeUnderConcurrency(t *testing.T) {
	restore := Silence()
	defer restore()

	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			for j := 0; j < 200; j++ {
				Printf("should not appear %d\n", j)
				Errorf("should not appear %d\n", j)
				Debugf("should not appear %d\n", j)
				_ = Quiet()
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
