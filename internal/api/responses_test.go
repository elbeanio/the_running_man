package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elbeanio/the_running_man/internal/storage"
	"github.com/elbeanio/the_running_man/internal/tracing"
)

// An empty result serialised as JSON null, because the query functions returned
// nil slices: {"count":0,"errors":null}. `jq '.errors[]'` fails on null, and the
// documented examples assume an array. Reproduced against a live instance.
func TestEmptyListsAreArraysNotNull(t *testing.T) {
	buffer := storage.NewRingBuffer(10, time.Minute, 1024)
	spans := tracing.NewSpanStorage(10, time.Minute)
	server := NewServer(buffer, "/projects/p", nil, nil, spans)
	handler := server.routes()

	for path, key := range map[string]string{
		"/logs":                    "logs",
		"/errors":                  "errors",
		"/traces":                  "traces",
		"/traces/nosuchtrace/logs": "logs",
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))

		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("%s: not JSON (%d): %s", path, rec.Code, rec.Body.String())
			continue
		}
		raw, ok := body[key]
		if !ok {
			t.Errorf("%s: no %q key in %s", path, key, rec.Body.String())
			continue
		}
		if strings.TrimSpace(string(raw)) != "[]" {
			t.Errorf("%s: %q is %s, want []", path, key, raw)
		}
	}
}

// /logs keeps the most recent N when limited, and says why: "the last 50
// errors" is what a caller means. /traces took spans[:limit], the oldest N --
// the two endpoints disagreed on what limit meant.
func TestTracesLimitKeepsTheMostRecent(t *testing.T) {
	spans := tracing.NewSpanStorage(100, time.Hour)
	for i := 0; i < 10; i++ {
		spans.Add(&tracing.SpanEntry{TraceID: string(rune('a' + i)), StartTime: time.Now()})
	}
	server := NewServer(storage.NewRingBuffer(10, time.Minute, 1024), "/p", nil, nil, spans)

	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/traces?limit=3", nil))

	var body struct {
		Traces []struct {
			TraceID string `json:"trace_id"`
		} `json:"traces"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	var got []string
	for _, s := range body.Traces {
		got = append(got, s.TraceID)
	}
	if strings.Join(got, "") != "hij" {
		t.Errorf("limit=3 returned %v, want the three most recent [h i j]", got)
	}
}
