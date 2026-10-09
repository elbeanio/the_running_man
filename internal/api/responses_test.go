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
	spans := tracing.NewSpanStorage(10, time.Minute, 1<<30)
	server := NewServer(buffer, "/projects/p", nil, nil, spans)
	handler := server.routes()

	for path, key := range map[string]string{
		"/logs":                    "logs",
		"/errors":                  "errors",
		"/traces":                  "traces",
		"/traces/nosuchtrace/logs": "logs",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		// As Running Man's own client, so the request is not logged: an
		// agent's call is logged into the buffer, and the earlier paths would
		// leave /logs not empty, depending on map order.
		req.Header.Set("User-Agent", InternalUserAgent)
		handler.ServeHTTP(rec, req)

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
	spans := tracing.NewSpanStorage(100, time.Hour, 1<<30)
	base := time.Now().Add(-time.Minute)
	for i := 0; i < 10; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		spans.Add(&tracing.SpanEntry{TraceID: string(rune('a' + i)), SpanID: "s", StartTime: at, EndTime: at})
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
	// Traces come newest first, so the three most recent are j, i, h.
	if strings.Join(got, "") != "jih" {
		t.Errorf("limit=3 returned %v, want the three most recent, newest first [j i h]", got)
	}
}
