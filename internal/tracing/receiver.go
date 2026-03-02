package tracing

import (
	"context"
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
		Handler: mux,
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
	w.Write(responseData)

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
