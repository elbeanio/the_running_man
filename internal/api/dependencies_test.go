package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/elbeanio/the_running_man/internal/process"
	"github.com/elbeanio/the_running_man/internal/storage"
)

// /processes carries the Compose services processes depend on, so a client
// can show their state without a second endpoint -- and an empty list, not
// null, when there are none.
func TestProcesses_ReportsDependencies(t *testing.T) {
	for _, tc := range []struct {
		name    string
		configs []process.ProcessConfig
		want    int
	}{
		{"none", []process.ProcessConfig{{Name: "a", Command: "sleep 30"}}, 0},
		{"one service", []process.ProcessConfig{{Name: "a", Command: "sleep 30", DependsOn: []string{"db"}}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := process.NewManager(tc.configs, nil)
			m.SetServiceReadiness(func(ctx context.Context, string string) error {
				<-ctx.Done()
				return ctx.Err()
			})
			if err := m.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = m.Stop() }()

			srv := NewServer(storage.NewRingBuffer(10, time.Minute, 1<<20), testProjectDir, nil, m, nil)
			rec := httptest.NewRecorder()
			srv.handleProcesses(rec, httptest.NewRequest("GET", "/processes", nil))

			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("not JSON: %s", rec.Body.String())
			}
			var deps []process.DependencyInfo
			if raw, ok := body["dependencies"]; !ok || string(raw) == "null" {
				t.Fatalf("dependencies missing or null: %s", rec.Body.String())
			} else if err := json.Unmarshal(raw, &deps); err != nil {
				t.Fatal(err)
			}
			if len(deps) != tc.want {
				t.Errorf("dependencies = %+v, want %d", deps, tc.want)
			}
		})
	}
}
