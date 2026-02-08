package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/iangeorge/the_running_man/internal/parser"
	"github.com/iangeorge/the_running_man/internal/process"
	"github.com/iangeorge/the_running_man/internal/storage"
)

const (
	// maxProcessNameLength is the maximum allowed length for process names
	maxProcessNameLength = 255
)

// LineHandler is called when a log line is captured
type LineHandler func(source string, line string, timestamp time.Time, isStderr bool)

// validateProcessName validates and sanitizes a process name
func validateProcessName(name string) (string, error) {
	processName := strings.TrimSpace(name)
	if processName == "" {
		return "", fmt.Errorf("Process name required")
	}
	if strings.Contains(processName, "/") || strings.Contains(processName, "..") {
		return "", fmt.Errorf("Invalid process name")
	}
	if len(processName) > maxProcessNameLength {
		return "", fmt.Errorf("Process name too long")
	}
	return processName, nil
}

// Server provides the HTTP API for querying logs
type Server struct {
	buffer      *storage.RingBuffer
	port        int
	startTime   time.Time
	lineHandler LineHandler
	manager     *process.Manager
}

// NewServer creates a new API server
func NewServer(buffer *storage.RingBuffer, port int, lineHandler LineHandler, manager *process.Manager) *Server {
	return &Server{
		buffer:      buffer,
		port:        port,
		startTime:   time.Now(),
		lineHandler: lineHandler,
		manager:     manager,
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

	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/errors", s.handleErrors)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/processes/stop-all", s.handleStopAll)  // Must come before /processes/
	mux.HandleFunc("/processes/", s.handleProcessOrRestart) // Handles both GET /processes/{name} and POST /processes/{name}/restart
	mux.HandleFunc("/processes", s.handleProcesses)

	addr := fmt.Sprintf(":%d", s.port)
	s.log(fmt.Sprintf("API server starting on http://localhost%s", addr), false)
	return http.ListenAndServe(addr, s.corsMiddleware(mux))
}

// corsMiddleware adds CORS headers for browser access
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
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
		s.writeError(w, http.StatusServiceUnavailable, "Process manager not available")
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
		s.writeError(w, http.StatusBadRequest, "Process name required")
		return
	}

	// Check if this is a restart request: /processes/{name}/restart
	if strings.HasSuffix(path, "/restart") {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed, use POST")
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
		s.writeError(w, http.StatusServiceUnavailable, "Process manager not available")
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
		s.writeError(w, http.StatusServiceUnavailable, "Process manager not available")
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
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed, use POST")
		return
	}

	// Check manager availability
	if s.manager == nil {
		s.writeError(w, http.StatusServiceUnavailable, "Process manager not available")
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
