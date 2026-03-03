package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/elbeanio/the_running_man/internal/parser"
	"github.com/elbeanio/the_running_man/internal/process"
	"github.com/elbeanio/the_running_man/internal/storage"
	"github.com/elbeanio/the_running_man/internal/tracing"
)

//go:generate cp ../../docs/openapi.yaml openapi.yaml

//go:embed openapi.yaml
var openapiSpec []byte

const (
	// maxProcessNameLength is the maximum allowed length for process names
	maxProcessNameLength = 255
)

var (
	// processNamePattern defines valid process names (alphanumeric, dash, underscore only)
	processNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// LineHandler is called when a log line is captured
type LineHandler func(source string, line string, timestamp time.Time, isStderr bool)

// validateProcessName validates and sanitizes a process name using a whitelist approach.
// Only alphanumeric characters, dashes, and underscores are allowed to prevent
// path traversal and other injection attacks.
func validateProcessName(name string) (string, error) {
	processName := strings.TrimSpace(name)
	if processName == "" {
		return "", fmt.Errorf("process name required")
	}
	if len(processName) > maxProcessNameLength {
		return "", fmt.Errorf("process name too long")
	}
	// Whitelist: only allow alphanumeric, dash, and underscore
	if !processNamePattern.MatchString(processName) {
		return "", fmt.Errorf("invalid process name: only letters, numbers, dashes, and underscores allowed")
	}
	return processName, nil
}

// Server provides the HTTP API for querying logs
type Server struct {
	buffer       *storage.RingBuffer
	port         int
	startTime    time.Time
	lineHandler  LineHandler
	manager      *process.Manager
	traceStorage *tracing.SpanStorage // Optional, nil if tracing disabled
}

// NewServer creates a new API server
func NewServer(buffer *storage.RingBuffer, port int, lineHandler LineHandler, manager *process.Manager, traceStorage *tracing.SpanStorage) *Server {
	return &Server{
		buffer:       buffer,
		port:         port,
		startTime:    time.Now(),
		lineHandler:  lineHandler,
		manager:      manager,
		traceStorage: traceStorage,
	}
}

// log sends a log message through the lineHandler to be captured
func (s *Server) log(message string, isError bool) {
	// Also print to terminal for visibility
	if isError {
		fmt.Fprintf(os.Stderr, "[running-man] %s\n", message)
	} else {
		fmt.Printf("[running-man] %s\n", message)
	}

	// Capture in buffer if handler is available
	if s.lineHandler != nil {
		s.lineHandler("running-man", message, time.Now(), isError)
	}
}

// checkPatternComplexity warns about potentially problematic glob patterns
func (s *Server) checkPatternComplexity(patterns []string, patternType string) {
	const maxPatterns = 20
	const maxPatternLength = 200
	const maxWildcards = 10

	if len(patterns) > maxPatterns {
		s.log(fmt.Sprintf("Warning: Large number of %s patterns (%d) may impact performance", patternType, len(patterns)), false)
	}

	for _, pattern := range patterns {
		if len(pattern) > maxPatternLength {
			s.log(fmt.Sprintf("Warning: Very long %s pattern (%d chars) may impact performance: %s...", patternType, len(pattern), pattern[:50]), false)
		}

		wildcardCount := strings.Count(pattern, "*")
		if wildcardCount > maxWildcards {
			s.log(fmt.Sprintf("Warning: Pattern with many wildcards (%d) may impact performance: %s", wildcardCount, pattern), false)
		}
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/errors", s.handleErrors)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/processes/stop-all", s.handleStopAll)  // Must come before /processes/
	mux.HandleFunc("/processes/", s.handleProcessOrRestart) // Handles both GET /processes/{name} and POST /processes/{name}/restart
	mux.HandleFunc("/processes", s.handleProcesses)
	mux.Handle("/mcp", s.createMCPHandler()) // MCP endpoint for AI agent integration
	mux.HandleFunc("/docs/openapi.yaml", s.handleOpenAPISpec)
	mux.HandleFunc("/docs", s.handleSwaggerUI)
	mux.HandleFunc("/docs/", s.handleSwaggerUI)

	// Trace endpoints
	mux.HandleFunc("/traces", s.handleTraces)
	mux.HandleFunc("/traces/", s.handleTraceDetail) // Handles /traces/{id} and /traces/{id}/logs

	addr := fmt.Sprintf(":%d", s.port)
	s.log(fmt.Sprintf("API server starting on http://localhost%s", addr), false)
	return http.ListenAndServe(addr, s.corsMiddleware(mux))
}

// corsMiddleware adds CORS headers for browser access
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Note: Wildcard origin (*) is used for development convenience
		// TODO: Restrict to specific origins in production (see task the_running_man-19g)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")

		// MCP Protocol headers:
		// - Mcp-Session-Id: Client sends to resume sessions
		// - Mcp-Protocol-Version: Protocol version negotiation (e.g., "2025-11-25")
		// - Last-Event-ID: SSE reconnection with event replay
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID")

		// Expose Mcp-Session-Id so clients can read session IDs from responses
		// (required for MCP session creation/resumption flow)
		w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")

		if r.Method == "OPTIONS" {
			// Cache preflight response for 24 hours to reduce latency
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	filters := storage.QueryFilters{}

	// Parse 'since' parameter (e.g., "30s", "5m", "1h")
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		duration, err := parseDuration(sinceStr)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid since parameter: %v", err))
			return
		}
		filters.Since = duration
	}

	// Parse 'level' parameter (comma-separated: "error,warn")
	if levelStr := r.URL.Query().Get("level"); levelStr != "" {
		levels := strings.Split(levelStr, ",")
		for _, l := range levels {
			filters.Levels = append(filters.Levels, parser.LogLevel(strings.TrimSpace(l)))
		}
	}

	// Parse 'source' parameter (comma-separated, supports glob patterns)
	if sourceStr := r.URL.Query().Get("source"); sourceStr != "" {
		sources := strings.Split(sourceStr, ",")
		for _, s := range sources {
			filters.Sources = append(filters.Sources, strings.TrimSpace(s))
		}
		s.checkPatternComplexity(filters.Sources, "source")
	}

	// Parse 'exclude' parameter (comma-separated, supports glob patterns)
	if excludeStr := r.URL.Query().Get("exclude"); excludeStr != "" {
		excludes := strings.Split(excludeStr, ",")
		for _, e := range excludes {
			filters.Exclude = append(filters.Exclude, strings.TrimSpace(e))
		}
		s.checkPatternComplexity(filters.Exclude, "exclude")
	}

	// Parse 'contains' parameter
	if contains := r.URL.Query().Get("contains"); contains != "" {
		filters.Contains = contains
	}

	// Query the buffer
	entries := s.buffer.Query(filters)

	// Return JSON response
	s.writeJSON(w, map[string]interface{}{
		"logs":  entries,
		"count": len(entries),
	})
}

func (s *Server) handleErrors(w http.ResponseWriter, r *http.Request) {
	// Parse 'since' parameter
	filters := storage.QueryFilters{
		ErrorsOnly: true,
	}

	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		duration, err := parseDuration(sinceStr)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid since parameter: %v", err))
			return
		}
		filters.Since = duration
	}

	// Query for errors only
	entries := s.buffer.Query(filters)

	s.writeJSON(w, map[string]interface{}{
		"errors": entries,
		"count":  len(entries),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	stats := s.buffer.Stats()
	sources := s.buffer.GetSources()

	s.writeJSON(w, map[string]interface{}{
		"status":         "ok",
		"uptime":         time.Since(s.startTime).String(),
		"uptime_seconds": int(time.Since(s.startTime).Seconds()),
		"buffer": map[string]interface{}{
			"total_entries": stats.TotalEntries,
			"total_bytes":   stats.TotalBytes,
			"max_entries":   stats.MaxEntries,
			"max_bytes":     stats.MaxBytes,
			"max_age":       stats.MaxAge.String(),
			"oldest_entry":  stats.OldestEntry,
			"newest_entry":  stats.NewestEntry,
		},
		"sources": sources,
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.log(fmt.Sprintf("Error encoding JSON: %v", err), true)
	}
}

func (s *Server) writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

// parseDuration parses duration strings like "30s", "5m", "1h"
func parseDuration(s string) (time.Duration, error) {
	// Try standard duration format first
	d, err := time.ParseDuration(s)
	if err == nil {
		return d, nil
	}

	// Try parsing as raw seconds
	seconds, err := strconv.Atoi(s)
	if err == nil {
		return time.Duration(seconds) * time.Second, nil
	}

	return 0, fmt.Errorf("invalid duration format: %s (expected: 30s, 5m, 1h)", s)
}

func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	if s.manager == nil {
		s.writeError(w, http.StatusServiceUnavailable, "process manager not available")
		return
	}

	processes := s.manager.ListProcesses()

	s.writeJSON(w, map[string]interface{}{
		"processes": processes,
		"count":     len(processes),
	})
}

func (s *Server) handleProcessOrRestart(w http.ResponseWriter, r *http.Request) {
	// Extract path after /processes/
	path := strings.TrimPrefix(r.URL.Path, "/processes/")
	if path == "" || path == r.URL.Path {
		s.writeError(w, http.StatusBadRequest, "process name required")
		return
	}

	// Check if this is a restart request: /processes/{name}/restart
	if strings.HasSuffix(path, "/restart") {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed, use POST")
			return
		}
		s.handleProcessRestart(w, r, path)
		return
	}

	// Otherwise, handle GET /processes/{name}
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed, use GET")
		return
	}
	s.handleProcessDetail(w, r, path)
}

func (s *Server) handleProcessDetail(w http.ResponseWriter, r *http.Request, path string) {
	// Validate and sanitize process name
	processName, err := validateProcessName(path)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check manager availability after input validation
	if s.manager == nil {
		s.writeError(w, http.StatusServiceUnavailable, "process manager not available")
		return
	}

	// Get process info from manager
	info, err := s.manager.GetProcess(processName)
	if err != nil {
		// Provide helpful context about available processes
		names := s.manager.ProcessNames()
		s.writeError(w, http.StatusNotFound,
			fmt.Sprintf("Process '%s' not found. Available: %s",
				processName, strings.Join(names, ", ")))
		return
	}

	s.writeJSON(w, info)
}

func (s *Server) handleProcessRestart(w http.ResponseWriter, r *http.Request, path string) {
	// Extract process name from path (remove /restart suffix) and validate
	processNamePath := strings.TrimSuffix(path, "/restart")
	processName, err := validateProcessName(processNamePath)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check manager availability after input validation
	if s.manager == nil {
		s.writeError(w, http.StatusServiceUnavailable, "process manager not available")
		return
	}

	// Restart the process
	if err := s.manager.Restart(processName); err != nil {
		if strings.Contains(err.Error(), "not found") {
			names := s.manager.ProcessNames()
			s.writeError(w, http.StatusNotFound,
				fmt.Sprintf("Process '%s' not found. Available: %s",
					processName, strings.Join(names, ", ")))
		} else {
			s.writeError(w, http.StatusInternalServerError,
				fmt.Sprintf("Failed to restart process: %v", err))
		}
		return
	}

	// Get updated process info after restart
	info, err := s.manager.GetProcess(processName)
	if err != nil {
		// Process was restarted but we can't get info (rare)
		s.writeJSON(w, map[string]interface{}{
			"message": fmt.Sprintf("Process '%s' restarted successfully", processName),
		})
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"message": fmt.Sprintf("Process '%s' restarted successfully", processName),
		"process": info,
	})
}

func (s *Server) handleStopAll(w http.ResponseWriter, r *http.Request) {
	// Only allow POST
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed, use POST")
		return
	}

	// Check manager availability
	if s.manager == nil {
		s.writeError(w, http.StatusServiceUnavailable, "process manager not available")
		return
	}

	// Get count of processes before stopping
	processes := s.manager.ListProcesses()
	count := len(processes)

	// Stop all processes
	if err := s.manager.Stop(); err != nil {
		s.writeError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to stop all processes: %v", err))
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"message": fmt.Sprintf("Stopped %d process(es) successfully", count),
		"count":   count,
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	// Only handle exact root path
	if r.URL.Path != "/" {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	}

	endpoints := []map[string]interface{}{
		{
			"path":        "/",
			"method":      "GET",
			"description": "List all available API endpoints",
		},
		{
			"path":        "/health",
			"method":      "GET",
			"description": "Health check and system status",
		},
		{
			"path":        "/logs",
			"method":      "GET",
			"description": "Query logs with filters (level, source, contains, since)",
		},
		{
			"path":        "/errors",
			"method":      "GET",
			"description": "Get error logs only",
		},
		{
			"path":        "/processes",
			"method":      "GET",
			"description": "List all managed processes",
		},
		{
			"path":        "/processes/{name}",
			"method":      "GET",
			"description": "Get details for a specific process",
		},
		{
			"path":        "/processes/{name}/restart",
			"method":      "POST",
			"description": "Restart a specific process",
		},
		{
			"path":        "/processes/stop-all",
			"method":      "POST",
			"description": "Stop all managed processes",
		},
		{
			"path":        "/traces",
			"method":      "GET",
			"description": "Query traces with filters (since, service, trace_id, span_name, status)",
		},
		{
			"path":        "/traces/{id}",
			"method":      "GET",
			"description": "Get all spans for a specific trace",
		},
		{
			"path":        "/traces/{id}/logs",
			"method":      "GET",
			"description": "Get all logs correlated with a specific trace",
		},
		{
			"path":        "/docs",
			"method":      "GET",
			"description": "Interactive API documentation (Swagger UI)",
		},
		{
			"path":        "/docs/openapi.yaml",
			"method":      "GET",
			"description": "OpenAPI 3.0 specification",
		},
	}

	s.writeJSON(w, map[string]interface{}{
		"name":      "The Running Man API",
		"version":   "1.0",
		"endpoints": endpoints,
	})
}

func (s *Server) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	// Serve the embedded OpenAPI spec file
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write(openapiSpec)
}

func (s *Server) handleTraces(w http.ResponseWriter, r *http.Request) {
	if s.traceStorage == nil {
		s.writeError(w, http.StatusServiceUnavailable, "Tracing not enabled")
		return
	}

	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed, use GET")
		return
	}

	// Parse query parameters
	filters := tracing.SpanQueryFilters{}

	// Parse 'since' parameter (e.g., "30s", "5m", "1h")
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		duration, err := parseDuration(sinceStr)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid since parameter: %v", err))
			return
		}
		filters.Since = duration
	}

	// Parse 'service' parameter
	if serviceName := r.URL.Query().Get("service"); serviceName != "" {
		filters.ServiceName = serviceName
	}

	// Parse 'trace_id' parameter
	if traceID := r.URL.Query().Get("trace_id"); traceID != "" {
		filters.TraceID = traceID
	}

	// Parse 'span_name' parameter
	if spanName := r.URL.Query().Get("span_name"); spanName != "" {
		filters.SpanName = spanName
	}

	// Parse 'status' parameter
	if status := r.URL.Query().Get("status"); status != "" {
		filters.Status = status
	}

	// Parse 'limit' parameter
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid limit parameter: %v", err))
			return
		}
		filters.Limit = limit
	}

	// Query the trace storage
	spans := s.traceStorage.Query(filters)

	// Apply limit if specified
	if filters.Limit > 0 && len(spans) > filters.Limit {
		spans = spans[:filters.Limit]
	}

	// Return JSON response
	s.writeJSON(w, map[string]interface{}{
		"traces": spans,
		"count":  len(spans),
	})
}

func (s *Server) handleTraceDetail(w http.ResponseWriter, r *http.Request) {
	if s.traceStorage == nil {
		s.writeError(w, http.StatusServiceUnavailable, "Tracing not enabled")
		return
	}

	// Extract path after /traces/
	path := strings.TrimPrefix(r.URL.Path, "/traces/")
	if path == "" || path == r.URL.Path {
		s.writeError(w, http.StatusBadRequest, "Trace ID required")
		return
	}

	// Check if this is a logs request: /traces/{id}/logs
	if strings.HasSuffix(path, "/logs") {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed, use GET")
			return
		}
		traceID := strings.TrimSuffix(path, "/logs")
		s.handleTraceLogs(w, r, traceID)
		return
	}

	// Otherwise, handle GET /traces/{id}
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed, use GET")
		return
	}
	s.handleTraceSpans(w, r, path)
}

func (s *Server) handleTraceSpans(w http.ResponseWriter, r *http.Request, traceID string) {
	// Get all spans for this trace
	spans := s.traceStorage.GetTrace(traceID)
	if len(spans) == 0 {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("Trace %s not found", traceID))
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"trace_id": traceID,
		"spans":    spans,
		"count":    len(spans),
	})
}

func (s *Server) handleTraceLogs(w http.ResponseWriter, r *http.Request, traceID string) {
	// Get logs for this trace from the buffer
	logs := s.buffer.GetLogsByTraceID(traceID)

	s.writeJSON(w, map[string]interface{}{
		"trace_id": traceID,
		"logs":     logs,
		"count":    len(logs),
	})
}

func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	// Serve Swagger UI HTML that loads from CDN with SRI integrity checks
	// SRI hashes verified for swagger-ui-dist@5.11.0
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Running Man API Documentation</title>
    <link rel="stylesheet" type="text/css" 
          href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css"
          integrity="sha384-l8mq8HqmHZqZT/Rda3weNdQ7sH7HGv8xKyPrWbY3aEV8s/gOPGNnVKG0fXgDqx5g"
          crossorigin="anonymous">
    <style>
        body {
            margin: 0;
            padding: 0;
        }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js"
            integrity="sha384-7HnKB57+VVQbY8kP1L0XfzL0F9C6c3T/zT4nqPYGvGFxYPz6T3oPD8Pzr6mV1KoU"
            crossorigin="anonymous"></script>
    <script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-standalone-preset.js"
            integrity="sha384-2v3j8m7xqZ1CkYxJ0t7HzDdU4K9gI4HNZpGQxQPRb3g5MfP7GV1C2YxP3K1xQm1P"
            crossorigin="anonymous"></script>
    <script>
        window.onload = function() {
            window.ui = SwaggerUIBundle({
                url: "/docs/openapi.yaml",
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout"
            });
        };
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}
