package process

import (
	"os"
	"testing"

	"github.com/elbeanio/the_running_man/internal/termout"
)

// Process output is echoed to the terminal unless something else owns it, which
// in a test run means the test output. Silenced for the whole package rather
// than per-test: no test here wants the processes it starts writing over the
// results.
//
// This replaced a `silent` field on the wrapper, which said the same thing
// termout does. Two mechanisms for one condition is how the container streamer
// came to have no gate at all: it was assumed to share the wrapper's.
func TestMain(m *testing.M) {
	restore := termout.Silence()
	code := m.Run()
	restore()
	os.Exit(code)
}
