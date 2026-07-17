package tracing

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	tracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Receiver handles OTLP trace ingestion
type Receiver struct {
	server  *http.Server
	storage *SpanStorage
	mu      sync.RWMutex
	port    int
	started bool
}

// NewReceiver creates a new OTLP trace receiver
func NewReceiver(storage *SpanStorage, port int) *Receiver {
	return &Receiver{
		storage: storage,
		port:    port,
	}
}

// Start starts the OTLP HTTP receiver server
func (r *Receiver) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return fmt.Errorf("receiver already started")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", r.handleTraces)
	mux.HandleFunc("/health", r.handleHealth)

	r.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", r.port),
		Handler: withCORS(mux),
	}

	go func() {
		if err := r.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[tracing] Failed to start OTLP receiver: %v\n", err)
		}
	}()

	r.started = true
	fmt.Printf("[tracing] OTLP receiver starting on http://localhost:%d\n", r.port)
	return nil
}

// Stop gracefully stops the receiver
func (r *Receiver) Stop(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started || r.server == nil {
		return nil
	}

	r.started = false
	return r.server.Shutdown(ctx)
}

// withCORS wraps the OTLP receiver mux with permissive CORS + preflight
// handling so a browser can export traces/logs directly to the receiver. The
// :9000 API server does this already (internal/api/server.go corsMiddleware);
// the receiver, a separate http.Server, never got the same treatment, so a
// browser preflight was rejected 405 with no CORS headers. Dev convenience —
// wildcard origin, same as the API server (see its TODO on restricting origins).
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleTraces handles OTLP trace ingestion requests
func (r *Receiver) handleTraces(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	contentType := req.Header.Get("Content-Type")
	var traceRequest tracev1.ExportTraceServiceRequest

	switch contentType {
	case "application/x-protobuf":
		data, err := readRequestBody(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to read request: %v", err), http.StatusBadRequest)
			return
		}
		if err := proto.Unmarshal(data, &traceRequest); err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse protobuf: %v", err), http.StatusBadRequest)
			return
		}

	case "application/json":
		data, err := readRequestBody(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to read request: %v", err), http.StatusBadRequest)
			return
		}
		unmarshaler := protojson.UnmarshalOptions{
			DiscardUnknown: true,
		}
		if err := unmarshaler.Unmarshal(data, &traceRequest); err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse JSON: %v", err), http.StatusBadRequest)
			return
		}
		fixHexTraceIDs(&traceRequest)

	default:
		http.Error(w, "Unsupported content type", http.StatusUnsupportedMediaType)
		return
	}

	// Process the trace spans
	spansProcessed := r.processTraceRequest(&traceRequest)

	// Send response
	response := &tracev1.ExportTraceServiceResponse{
		// OTLP spec: partial_success is optional, we can omit it for now
	}

	var responseData []byte
	var err error

	if contentType == "application/x-protobuf" {
		responseData, err = proto.Marshal(response)
	} else {
		marshaler := protojson.MarshalOptions{
			UseProtoNames: true,
		}
		responseData, err = marshaler.Marshal(response)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(responseData); err != nil {
		fmt.Printf("[tracing] Failed to write response: %v\n", err)
	}

	fmt.Printf("[tracing] Processed %d spans from OTLP request\n", spansProcessed)
}

// processTraceRequest extracts spans from OTLP request and stores them
func (r *Receiver) processTraceRequest(req *tracev1.ExportTraceServiceRequest) int {
	spansProcessed := 0

	for _, resourceSpans := range req.ResourceSpans {
		for _, scopeSpans := range resourceSpans.ScopeSpans {
			for _, span := range scopeSpans.Spans {
				// Convert OTLP span to our internal format
				spanEntry := convertOTLPSpan(span, resourceSpans.Resource)

				// Store the span
				r.storage.Add(spanEntry)
				spansProcessed++
			}
		}
	}

	return spansProcessed
}

// fixHexTraceIDs repairs trace/span ids on an OTLP/JSON request that protojson
// decoded as base64.
//
// The OTLP/JSON spec mandates HEX for trace_id/span_id — a deliberate deviation
// from the standard protobuf-JSON mapping, and what every conformant OTel
// exporter emits. But vanilla protojson decodes protobuf `bytes` fields as
// base64, so a conformant hex id (32/16 chars) is silently base64-decoded into
// 24/12 bytes of garbage and stored wrong — HTTP 200, no warning. A browser is
// the first client to hit this, because TRM injects http/protobuf into managed
// server processes.
//
// The repair is exact: hex is valid base64, so re-encoding the mis-decoded
// bytes to base64 reproduces the original hex string, which we then hex-decode
// to the correct id. A genuinely base64 id (already the right byte length) is
// left untouched, so this is backward compatible.
func fixHexTraceIDs(req *tracev1.ExportTraceServiceRequest) {
	for _, rs := range req.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, span := range ss.Spans {
				span.TraceId = hexDecodeID(span.TraceId, 16)
				span.SpanId = hexDecodeID(span.SpanId, 8)
				span.ParentSpanId = hexDecodeID(span.ParentSpanId, 8)
				for _, link := range span.Links {
					link.TraceId = hexDecodeID(link.TraceId, 16)
					link.SpanId = hexDecodeID(link.SpanId, 8)
				}
			}
		}
	}
}

// hexDecodeID returns b unchanged if it's already the wanted byte length (a real
// base64 id, or protobuf) or empty; otherwise it treats b as protojson's base64
// decoding of a hex string, recovers that string, and hex-decodes it. A 32-char
// hex trace id always base64-decodes to 24 bytes (16-char span id → 12), so the
// length check cleanly distinguishes the two encodings with no collision.
func hexDecodeID(b []byte, want int) []byte {
	if len(b) == want || len(b) == 0 {
		return b
	}
	if decoded, err := hex.DecodeString(base64.StdEncoding.EncodeToString(b)); err == nil && len(decoded) == want {
		return decoded
	}
	return b
}

// handleHealth provides a health check endpoint
func (r *Receiver) handleHealth(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status": "ok", "service": "otlp-receiver"}`)
}

// WaitForReady waits for the receiver to be ready by polling the health endpoint
func (r *Receiver) WaitForReady(timeout time.Duration) error {
	start := time.Now()
	url := fmt.Sprintf("http://localhost:%d/health", r.port)

	for time.Since(start) < timeout {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("receiver not ready after %v", timeout)
}

// readRequestBody reads and returns the request body
func readRequestBody(req *http.Request) ([]byte, error) {
	defer req.Body.Close()
	return io.ReadAll(req.Body)
}
