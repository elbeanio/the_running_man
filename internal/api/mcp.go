package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iangeorge/the_running_man/internal/parser"
	"github.com/iangeorge/the_running_man/internal/process"
	"github.com/iangeorge/the_running_man/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// createMCPHandler creates an HTTP handler for the Model Context Protocol (MCP) endpoint.
// MCP provides a standardized interface for AI agents to interact with running-man.
//
// IMPORTANT: running-man is a process manager that:
// 1. Manages processes (starts, stops, restarts, monitors them)
// 2. Automatically captures ALL output (stdout/stderr) from managed processes
// 3. Aggregates logs in a centralized buffer with parsing and filtering
// 4. Provides real-time status and health monitoring
//
// Key differences from traditional approaches:
// - No need to grep log files - all logs are already captured
// - No need to start separate monitoring services - running-man does it
// - Logs are parsed and structured (timestamps, levels, stacktraces)
// - Process management is centralized through running-man
//
// The handler uses StreamableHTTPHandler which provides:
// - Session management with Mcp-Session-Id header
// - SSE (Server-Sent Events) for streaming responses
// - Support for GET (SSE stream), POST (messages), and DELETE (session termination)
// - Automatic message routing and JSON-RPC protocol handling
//
// Note: This endpoint is currently unauthenticated. See task the_running_man-19g for adding auth.
func (s *Server) createMCPHandler() http.Handler {
	// Create MCP server with implementation details
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "the-running-man",
		Version: "0.1.0",
	}, nil)

	// Register MCP tools
	s.registerSearchLogsTool(server)
	s.registerGetRecentErrorsTool(server)
	s.registerGetProcessStatusTool(server)
	s.registerGetStartupLogsTool(server)
	s.registerRestartProcessTool(server)
	s.registerStopAllProcessesTool(server)
	s.registerGetProcessDetailTool(server)
	s.registerGetHealthStatusTool(server)

	// Log that MCP endpoint is being initialized
	s.log("Initializing MCP endpoint with streamable HTTP handler", false)

	// Return streamable HTTP handler that handles full MCP protocol
	// The SDK automatically handles:
	// - Session lifecycle (create/resume/terminate)
	// - SSE streaming for server-to-client messages
	// - JSON-RPC message routing
	// - Protocol version negotiation
	return mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		// Return the same server instance for all sessions since all tools
		// are stateless queries against shared resources (RingBuffer, ProcessManager).
		// If per-session state is needed in the future (e.g., user preferences,
		// session-specific auth context), create a new server instance per session.
		return server
	}, nil)
}

// SearchLogsArgs defines the parameters for the search_logs MCP tool
type SearchLogsArgs struct {
	Source   string `json:"source,omitempty" jsonschema:"Filter by running-man process name. Supports glob patterns. Example: 'web-server' or 'app-*' to match all app processes"`
	Since    string `json:"since,omitempty" jsonschema:"Search logs from this time window. Duration format: '5m' (5 min), '1h' (1 hour), '30s' (30 sec). Example: '2h' for last 2 hours"`
	Level    string `json:"level,omitempty" jsonschema:"Filter by log level captured by running-man. Options: 'error', 'warn', 'info', 'debug'. Example: 'error' for only errors"`
	Contains string `json:"contains,omitempty" jsonschema:"Search for text in log messages captured by running-man. Case-sensitive. Example: 'connection failed'"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum log entries to return from running-man's buffer. Default: 50, Max: 1000. Example: 100"`
}

// registerSearchLogsTool registers the search_logs MCP tool
func (s *Server) registerSearchLogsTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_logs",
		Description: `Search and filter log entries from running-man's centralized log buffer.

IMPORTANT: running-man is a process manager that automatically captures and aggregates logs from ALL managed processes. You do NOT need to grep log files or start separate services - all logs are already being collected.

Use this tool to search across logs from ALL running-man managed processes. You can filter by:
- Source (process name managed by running-man) using glob patterns like "app-*" or "database"
- Time window using duration strings like "5m" (5 minutes), "1h" (1 hour), "30s" (30 seconds)
- Log level: "error", "warn", "info", "debug"
- Text content within log messages

Examples:
- Find all errors in the last hour: {"since": "1h", "level": "error"}
- Search for "connection failed" in database logs: {"source": "database", "contains": "connection failed"}
- Get recent info logs from web services: {"source": "web-*", "level": "info", "limit": 20}

Default limit is 50 entries, maximum is 1000. Results show most recent entries first.`,
	}, s.searchLogsHandler)

	s.log("Registered MCP tool: search_logs", false)
}

// searchLogsHandler implements the search_logs MCP tool
func (s *Server) searchLogsHandler(ctx context.Context, req *mcp.CallToolRequest, args *SearchLogsArgs) (*mcp.CallToolResult, any, error) {
	// Build query filters from args
	filters := storage.QueryFilters{}

	// Parse time window
	if args.Since != "" {
		duration, err := parseDuration(args.Since)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid 'since' parameter: %v", err)
		}
		if duration < 0 {
			return nil, nil, fmt.Errorf("invalid 'since' parameter: duration cannot be negative")
		}
		filters.Since = duration
	}

	// Parse and validate log level
	if args.Level != "" {
		level := parser.LogLevel(strings.ToLower(args.Level))
		validLevels := map[parser.LogLevel]bool{
			parser.LevelDebug: true,
			parser.LevelInfo:  true,
			parser.LevelWarn:  true,
			parser.LevelError: true,
		}
		if !validLevels[level] {
			return nil, nil, fmt.Errorf("invalid 'level' parameter: must be one of: debug, info, warn, error")
		}
		filters.Levels = []parser.LogLevel{level}
	}

	// Parse source filter with complexity validation (DoS protection)
	if args.Source != "" {
		if strings.Count(args.Source, "*") > 5 || strings.Count(args.Source, "[") > 3 {
			return nil, nil, fmt.Errorf("invalid 'source' parameter: pattern too complex")
		}
		filters.Sources = []string{args.Source}
	}

	// Parse contains filter (case-sensitive)
	if args.Contains != "" {
		filters.Contains = args.Contains
	}

	// Query the buffer directly
	entries := s.buffer.Query(filters)
	totalFound := len(entries)

	// Apply limit (default 50, max 1000)
	limit := args.Limit
	if limit == 0 {
		limit = 50
	} else if limit > 1000 {
		limit = 1000
	}

	// Truncate to limit if needed (keeps most recent entries)
	truncated := false
	if totalFound > limit {
		entries = entries[totalFound-limit:]
		truncated = true
	}

	// Format results as readable text
	var result strings.Builder
	if totalFound == 0 {
		result.WriteString("No log entries found matching the criteria.\n")
	} else {
		if truncated {
			result.WriteString(fmt.Sprintf("Found %d log entries (showing most recent %d):\n\n", totalFound, limit))
		} else {
			result.WriteString(fmt.Sprintf("Found %d log entries:\n\n", totalFound))
		}
		for _, entry := range entries {
			timestamp := entry.Timestamp.Format("15:04:05.000")
			level := string(entry.Level)
			if level == "" {
				level = "-"
			}
			result.WriteString(fmt.Sprintf("[%s] [%s] [%s] %s\n",
				timestamp, entry.Source, level, entry.Message))
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: result.String()},
		},
	}, nil, nil
}

// GetRecentErrorsArgs defines the parameters for the get_recent_errors MCP tool
type GetRecentErrorsArgs struct {
	Source  string `json:"source,omitempty" jsonschema:"Filter by running-man process name. Supports glob patterns. Example: 'database' or 'app-*' for all app processes"`
	Since   string `json:"since,omitempty" jsonschema:"Time window to search for errors captured by running-man. Duration format: '30m', '2h', '5s'. Example: '1h' for last hour"`
	Context int    `json:"context,omitempty" jsonschema:"Number of log lines before/after each error to include from running-man's buffer. Default: 10, Max: 50. Example: 15"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum errors to return from running-man. Default: 20, Max: 100. Example: 50"`
}

// registerGetRecentErrorsTool registers the get_recent_errors MCP tool
func (s *Server) registerGetRecentErrorsTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_recent_errors",
		Description: `Get recent error log entries from running-man's centralized log buffer with optional surrounding context.

IMPORTANT: running-man automatically captures errors from ALL managed processes. You do NOT need to check individual log files - all errors are already aggregated here.

This tool is specifically designed for troubleshooting errors across all running-man managed processes. It shows:
- Error messages with timestamps and source (which running-man process)
- Stacktraces when available (automatically parsed from logs)
- Configurable number of log lines before/after each error (default: 10)
- Error count and filtering options

Parameters:
- source: Filter by running-man process name (supports glob patterns)
- since: Time window (e.g., "5m", "1h", "30s")
- context: Number of log lines before/after each error to include (default: 10, max: 50)
- limit: Maximum number of errors to return (default: 20, max: 100)

Examples:
- Get all errors from the last 30 minutes: {"since": "30m"}
- See errors from web services with context: {"source": "web-*", "context": 15}
- Check database errors only: {"source": "database", "limit": 10}

Each error is shown with its context to help understand what led to the failure.`,
	}, s.getRecentErrorsHandler)

	s.log("Registered MCP tool: get_recent_errors", false)
}

// getRecentErrorsHandler implements the get_recent_errors MCP tool
func (s *Server) getRecentErrorsHandler(ctx context.Context, req *mcp.CallToolRequest, args *GetRecentErrorsArgs) (*mcp.CallToolResult, any, error) {
	// Build query filters from args
	filters := storage.QueryFilters{
		ErrorsOnly: true,
	}

	// Parse time window
	if args.Since != "" {
		duration, err := parseDuration(args.Since)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid 'since' parameter: %v", err)
		}
		if duration < 0 {
			return nil, nil, fmt.Errorf("invalid 'since' parameter: duration cannot be negative")
		}
		filters.Since = duration
	}

	// Parse source filter with complexity validation (DoS protection)
	if args.Source != "" {
		if strings.Count(args.Source, "*") > 5 || strings.Count(args.Source, "[") > 3 {
			return nil, nil, fmt.Errorf("invalid 'source' parameter: pattern too complex")
		}
		filters.Sources = []string{args.Source}
	}

	// Query the buffer for errors
	errorEntries := s.buffer.Query(filters)
	totalErrors := len(errorEntries)

	// Apply limit (default 20, max 100)
	limit := args.Limit
	if limit == 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	// Truncate to limit if needed (keeps most recent errors)
	truncated := false
	if totalErrors > limit {
		errorEntries = errorEntries[totalErrors-limit:]
		truncated = true
	}

	// Get context lines (default 10)
	contextLines := args.Context
	if contextLines == 0 {
		contextLines = 10
	} else if contextLines > 50 {
		contextLines = 50
	}

	// Format results as readable text
	var result strings.Builder
	if totalErrors == 0 {
		result.WriteString("No error log entries found matching the criteria.\n")
	} else {
		if truncated {
			result.WriteString(fmt.Sprintf("Found %d error log entries (showing most recent %d):\n\n", totalErrors, limit))
		} else {
			result.WriteString(fmt.Sprintf("Found %d error log entries:\n\n", totalErrors))
		}

		// Get all entries for context lookup if needed
		var allEntries []*parser.LogEntry
		if contextLines > 0 {
			allEntries = s.buffer.Query(storage.QueryFilters{})
		}

		for i, entry := range errorEntries {
			// Add separator between errors
			if i > 0 {
				result.WriteString("\n" + strings.Repeat("-", 80) + "\n\n")
			}

			// Format error header
			timestamp := entry.Timestamp.Format("15:04:05.000")
			result.WriteString(fmt.Sprintf("ERROR #%d [%s] [%s]\n", i+1, timestamp, entry.Source))
			result.WriteString(fmt.Sprintf("Message: %s\n\n", entry.Message))

			// Add stacktrace if available
			if entry.Stacktrace != "" {
				result.WriteString("Stacktrace:\n")
				result.WriteString(entry.Stacktrace)
				result.WriteString("\n\n")
			}

			// Add context if requested and available
			if contextLines > 0 && len(allEntries) > 0 {
				// Find the index of this error in all entries
				errorIdx := -1
				for j, e := range allEntries {
					if e.Timestamp.Equal(entry.Timestamp) && e.Source == entry.Source && e.Message == entry.Message {
						errorIdx = j
						break
					}
				}

				if errorIdx >= 0 {
					// Calculate context range
					startIdx := max(0, errorIdx-contextLines)
					endIdx := min(len(allEntries)-1, errorIdx+contextLines)

					result.WriteString(fmt.Sprintf("Context (%d lines before/after):\n", contextLines))

					// Show context lines
					for j := startIdx; j <= endIdx; j++ {
						ctxEntry := allEntries[j]
						ctxTimestamp := ctxEntry.Timestamp.Format("15:04:05.000")
						ctxLevel := string(ctxEntry.Level)
						if ctxLevel == "" {
							ctxLevel = "-"
						}

						// Mark the error line
						marker := "  "
						if j == errorIdx {
							marker = ">>"
						}

						result.WriteString(fmt.Sprintf("%s [%s] [%s] [%s] %s\n",
							marker, ctxTimestamp, ctxEntry.Source, ctxLevel, ctxEntry.Message))
					}
					result.WriteString("\n")
				}
			}
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: result.String()},
		},
	}, nil, nil
}

// GetProcessStatusArgs defines the parameters for the get_process_status MCP tool
type GetProcessStatusArgs struct {
	Name string `json:"name,omitempty" jsonschema:"Filter by running-man process name (exact match). Example: 'database' for specific process"`
}

// registerGetProcessStatusTool registers the get_process_status MCP tool
func (s *Server) registerGetProcessStatusTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_process_status",
		Description: `Get status of processes managed by running-man (the process manager).

IMPORTANT: running-man is actively managing these processes - starting them, monitoring them, capturing their logs, and restarting them if configured. These are NOT just processes running on the system.

This tool provides a comprehensive view of what processes running-man is managing:
- Process name and command line (as configured in running-man)
- PID (process ID) if currently running
- Status: "running", "stopped", or "failed" (as tracked by running-man)
- Uptime (how long running-man has kept this process running)
- Exit code (for stopped processes, -1 for running)

Parameters:
- name: Optional process name to filter (exact match only)

Examples:
- Get status of all running-man managed processes: {} (empty parameters)
- Check specific running-man process: {"name": "database"}
- Monitor web service managed by running-man: {"name": "web-server"}

Use this tool to monitor process health within running-man's management system. If a process name is specified but not found, the tool will list all available processes managed by running-man.`,
	}, s.getProcessStatusHandler)

	s.log("Registered MCP tool: get_process_status", false)
}

// getProcessStatusHandler implements the get_process_status MCP tool
func (s *Server) getProcessStatusHandler(ctx context.Context, req *mcp.CallToolRequest, args *GetProcessStatusArgs) (*mcp.CallToolResult, any, error) {
	// Check if process manager is available
	if s.manager == nil {
		return nil, nil, fmt.Errorf("process manager not available")
	}

	// Get all processes
	processes := s.manager.ListProcesses()
	totalProcesses := len(processes)

	// Filter by name if specified
	var filteredProcesses []process.ProcessInfo
	if args.Name != "" {
		for _, p := range processes {
			if p.Name == args.Name {
				filteredProcesses = append(filteredProcesses, p)
			}
		}
	} else {
		filteredProcesses = processes
	}

	// Format results as readable text
	var result strings.Builder
	if len(filteredProcesses) == 0 {
		if args.Name != "" {
			result.WriteString(fmt.Sprintf("No process found with name '%s'.\n", args.Name))
			result.WriteString("Available processes:\n")
			for _, p := range processes {
				result.WriteString(fmt.Sprintf("  - %s\n", p.Name))
			}
		} else {
			result.WriteString("No managed processes found.\n")
		}
	} else {
		if args.Name != "" {
			result.WriteString(fmt.Sprintf("Process status for '%s':\n\n", args.Name))
		} else {
			result.WriteString(fmt.Sprintf("Managed processes (%d total):\n\n", totalProcesses))
		}

		for i, p := range filteredProcesses {
			// Add separator between processes
			if i > 0 {
				result.WriteString("\n" + strings.Repeat("-", 80) + "\n\n")
			}

			// Calculate uptime
			uptime := "not started"
			if !p.StartTime.IsZero() {
				uptime = time.Since(p.StartTime).Round(time.Second).String()
			}

			// Format PID
			pidStr := "N/A"
			if p.PID > 0 {
				pidStr = fmt.Sprintf("%d", p.PID)
			}

			// Format exit code
			exitCodeStr := "N/A"
			if p.ExitCode >= 0 {
				exitCodeStr = fmt.Sprintf("%d", p.ExitCode)
			} else if p.ExitCode == -1 {
				exitCodeStr = "running"
			}

			// Write process info
			result.WriteString(fmt.Sprintf("Process: %s\n", p.Name))
			result.WriteString(fmt.Sprintf("Command: %s\n", p.Command))
			result.WriteString(fmt.Sprintf("PID: %s\n", pidStr))
			result.WriteString(fmt.Sprintf("Status: %s\n", p.Status))
			result.WriteString(fmt.Sprintf("Uptime: %s\n", uptime))
			result.WriteString(fmt.Sprintf("Exit Code: %s\n", exitCodeStr))
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: result.String()},
		},
	}, nil, nil
}

// GetStartupLogsArgs defines the parameters for the get_startup_logs MCP tool
type GetStartupLogsArgs struct {
	Source string `json:"source" jsonschema:"Running-man process name (required). Example: 'database' for database process managed by running-man"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum log entries to return from running-man's startup capture. Default: 50, Max: 200. Example: 100"`
}

// registerGetStartupLogsTool registers the get_startup_logs MCP tool
func (s *Server) registerGetStartupLogsTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_startup_logs",
		Description: `Get log entries from when a running-man managed process started.

IMPORTANT: This shows logs that running-man captured when it started the process. You don't need to check startup scripts or init logs - running-man automatically captures all output from process startup.

This tool helps answer "why won't this running-man process start?" by showing logs captured by running-man from the moment it launched the process. It displays logs in chronological order from startup, with timestamps relative to when running-man started the process.

Parameters:
- source: REQUIRED - Process name managed by running-man
- limit: Maximum number of log entries to return (default: 50, max: 200)

Examples:
- Debug why database won't start under running-man: {"source": "database"}
- See startup logs for web service managed by running-man: {"source": "web-server", "limit": 100}
- Check initial configuration errors: {"source": "config-loader"}

The tool shows logs with timestamps relative to process start (+0.5s, +1.2s, etc.) to help identify timing issues. If the process hasn't started or doesn't exist in running-man, you'll get a clear error message with available process names.`,
	}, s.getStartupLogsHandler)

	s.log("Registered MCP tool: get_startup_logs", false)
}

// getStartupLogsHandler implements the get_startup_logs MCP tool
func (s *Server) getStartupLogsHandler(ctx context.Context, req *mcp.CallToolRequest, args *GetStartupLogsArgs) (*mcp.CallToolResult, any, error) {
	// Validate required source parameter
	if args.Source == "" {
		return nil, nil, fmt.Errorf("'source' parameter is required (process name)")
	}

	// Check if process manager is available
	if s.manager == nil {
		return nil, nil, fmt.Errorf("process manager not available")
	}

	// Get process information to find start time
	processes := s.manager.ListProcesses()
	var targetProcess *process.ProcessInfo
	for i, p := range processes {
		if p.Name == args.Source {
			targetProcess = &processes[i]
			break
		}
	}

	if targetProcess == nil {
		// List available processes for helpful error message
		var availableNames []string
		for _, p := range processes {
			availableNames = append(availableNames, p.Name)
		}
		if len(availableNames) == 0 {
			return nil, nil, fmt.Errorf("process '%s' not found (no processes are managed)", args.Source)
		}
		return nil, nil, fmt.Errorf("process '%s' not found. Available processes: %s", args.Source, strings.Join(availableNames, ", "))
	}

	// Check if process has started
	if targetProcess.StartTime.IsZero() {
		return nil, nil, fmt.Errorf("process '%s' has not started yet", args.Source)
	}

	// Calculate time since process started
	startTime := targetProcess.StartTime
	now := time.Now()
	if startTime.After(now) {
		return nil, nil, fmt.Errorf("process '%s' has invalid start time in the future", args.Source)
	}

	// Build query filters for logs since process started
	filters := storage.QueryFilters{
		Sources: []string{args.Source},
		Since:   now.Sub(startTime),
	}

	// Query logs since process started
	entries := s.buffer.Query(filters)
	totalFound := len(entries)

	// Apply limit (default 50, max 200)
	limit := args.Limit
	if limit == 0 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}

	// Sort entries chronologically (oldest first for startup logs)
	// Note: buffer.Query returns newest first, so we need to reverse
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	// Truncate to limit if needed (keeps earliest entries for startup context)
	truncated := false
	if totalFound > limit {
		entries = entries[:limit]
		truncated = true
	}

	// Format results as readable text
	var result strings.Builder
	if totalFound == 0 {
		result.WriteString(fmt.Sprintf("No log entries found for process '%s' since it started at %s.\n", args.Source, startTime.Format("15:04:05.000")))
	} else {
		result.WriteString(fmt.Sprintf("Startup logs for process '%s' (started at %s):\n", args.Source, startTime.Format("15:04:05.000")))
		if truncated {
			result.WriteString(fmt.Sprintf("Found %d log entries (showing first %d):\n\n", totalFound, limit))
		} else {
			result.WriteString(fmt.Sprintf("Found %d log entries:\n\n", totalFound))
		}

		for _, entry := range entries {
			// Calculate time since process start
			timeSinceStart := entry.Timestamp.Sub(startTime).Round(time.Millisecond)
			timestamp := entry.Timestamp.Format("15:04:05.000")
			level := string(entry.Level)
			if level == "" {
				level = "-"
			}

			result.WriteString(fmt.Sprintf("[+%s] [%s] [%s] %s\n",
				timeSinceStart, timestamp, level, entry.Message))
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: result.String()},
		},
	}, nil, nil
}

// RestartProcessArgs defines the parameters for the restart_process MCP tool
type RestartProcessArgs struct {
	Name string `json:"name" jsonschema:"Running-man process name to restart (required). Example: 'database'"`
}

// registerRestartProcessTool registers the restart_process MCP tool
func (s *Server) registerRestartProcessTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "restart_process",
		Description: `Restart a specific process managed by running-man.

IMPORTANT: This tells running-man to stop and restart the specified process. The process will be gracefully stopped and a new instance started with the same configuration.

Parameters:
- name: REQUIRED - Process name managed by running-man

Examples:
- Restart database process: {"name": "database"}
- Restart web server: {"name": "web-server"}

The tool returns confirmation with process details before and after restart. If the process doesn't exist or running-man's manager is unavailable, you'll get a clear error message.`,
	}, s.restartProcessHandler)

	s.log("Registered MCP tool: restart_process", false)
}

// restartProcessHandler implements the restart_process MCP tool
func (s *Server) restartProcessHandler(ctx context.Context, req *mcp.CallToolRequest, args *RestartProcessArgs) (*mcp.CallToolResult, any, error) {
	// Validate required name parameter
	if args.Name == "" {
		return nil, nil, fmt.Errorf("'name' parameter is required (process name)")
	}

	// Check if process manager is available
	if s.manager == nil {
		return nil, nil, fmt.Errorf("process manager not available")
	}

	// Get process info before restart
	processes := s.manager.ListProcesses()
	var targetProcess *process.ProcessInfo
	for i, p := range processes {
		if p.Name == args.Name {
			targetProcess = &processes[i]
			break
		}
	}

	if targetProcess == nil {
		// List available processes for helpful error message
		var availableNames []string
		for _, p := range processes {
			availableNames = append(availableNames, p.Name)
		}
		if len(availableNames) == 0 {
			return nil, nil, fmt.Errorf("process '%s' not found (no processes are managed by running-man)", args.Name)
		}
		return nil, nil, fmt.Errorf("process '%s' not found. Available processes: %s", args.Name, strings.Join(availableNames, ", "))
	}

	// Format process info before restart
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Restarting process '%s'...\n\n", args.Name))
	result.WriteString("Before restart:\n")
	result.WriteString(fmt.Sprintf("  Name:      %s\n", targetProcess.Name))
	result.WriteString(fmt.Sprintf("  Command:   %s\n", targetProcess.Command))
	result.WriteString(fmt.Sprintf("  Status:    %s\n", targetProcess.Status))

	if targetProcess.PID > 0 {
		result.WriteString(fmt.Sprintf("  PID:       %d\n", targetProcess.PID))
	}

	if !targetProcess.StartTime.IsZero() {
		uptime := time.Since(targetProcess.StartTime).Round(time.Second)
		result.WriteString(fmt.Sprintf("  Uptime:    %s\n", uptime))
	}

	// Restart the process
	err := s.manager.Restart(args.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to restart process '%s': %v", args.Name, err)
	}

	// Get updated process info
	updatedProcesses := s.manager.ListProcesses()
	var updatedProcess *process.ProcessInfo
	for i, p := range updatedProcesses {
		if p.Name == args.Name {
			updatedProcess = &updatedProcesses[i]
			break
		}
	}

	result.WriteString("\nRestart completed successfully!\n\n")
	result.WriteString("After restart:\n")

	if updatedProcess != nil {
		result.WriteString(fmt.Sprintf("  Name:      %s\n", updatedProcess.Name))
		result.WriteString(fmt.Sprintf("  Command:   %s\n", updatedProcess.Command))
		result.WriteString(fmt.Sprintf("  Status:    %s\n", updatedProcess.Status))

		if updatedProcess.PID > 0 {
			result.WriteString(fmt.Sprintf("  PID:       %d\n", updatedProcess.PID))
		}

		if !updatedProcess.StartTime.IsZero() {
			result.WriteString(fmt.Sprintf("  Start Time: %s\n", updatedProcess.StartTime.Format("15:04:05.000")))
		}
	} else {
		result.WriteString("  (Process info not available after restart)\n")
	}

	result.WriteString("\nNote: Check process logs with 'get_startup_logs' or 'search_logs' to see restart output.")

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: result.String()},
		},
	}, nil, nil
}

// StopAllProcessesArgs defines the parameters for the stop_all_processes MCP tool
type StopAllProcessesArgs struct {
	Confirm bool `json:"confirm,omitempty" jsonschema:"Safety confirmation. Must be true to proceed. Example: true"`
}

// registerStopAllProcessesTool registers the stop_all_processes MCP tool
func (s *Server) registerStopAllProcessesTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "stop_all_processes",
		Description: `Stop ALL processes managed by running-man.

IMPORTANT: This is a destructive operation that stops every process running-man is managing. Use with caution!

Parameters:
- confirm: REQUIRED safety confirmation. Must be set to true to proceed.

Examples:
- Stop all processes: {"confirm": true}

The tool returns a list of all processes that were stopped. If no processes are managed or the manager is unavailable, you'll get an appropriate message.`,
	}, s.stopAllProcessesHandler)

	s.log("Registered MCP tool: stop_all_processes", false)
}

// stopAllProcessesHandler implements the stop_all_processes MCP tool
func (s *Server) stopAllProcessesHandler(ctx context.Context, req *mcp.CallToolRequest, args *StopAllProcessesArgs) (*mcp.CallToolResult, any, error) {
	// Validate safety confirmation
	if !args.Confirm {
		return nil, nil, fmt.Errorf("safety confirmation required. Set 'confirm': true to stop all processes")
	}

	// Check if process manager is available
	if s.manager == nil {
		return nil, nil, fmt.Errorf("process manager not available")
	}

	// Get process info before stopping
	processes := s.manager.ListProcesses()
	totalProcesses := len(processes)

	if totalProcesses == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "No processes are currently managed by running-man. Nothing to stop."},
			},
		}, nil, nil
	}

	// Format process list before stopping
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Stopping %d process(es) managed by running-man:\n\n", totalProcesses))

	for i, p := range processes {
		result.WriteString(fmt.Sprintf("%d. %s", i+1, p.Name))
		if p.Status == "running" && p.PID > 0 {
			result.WriteString(fmt.Sprintf(" (PID: %d)", p.PID))
		}
		result.WriteString(fmt.Sprintf(" - %s\n", p.Status))

		if !p.StartTime.IsZero() {
			uptime := time.Since(p.StartTime).Round(time.Second)
			result.WriteString(fmt.Sprintf("   Uptime: %s\n", uptime))
		}
		result.WriteString("\n")
	}

	// Stop all processes
	err := s.manager.Stop()
	if err != nil {
		// Even if some processes failed to stop, report what happened
		result.WriteString(fmt.Sprintf("\nWarning: Some processes may not have stopped cleanly: %v\n", err))
	} else {
		result.WriteString("\nAll processes stopped successfully.\n")
	}

	result.WriteString("\nNote: Processes can be restarted using the 'restart_process' tool if needed.")

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: result.String()},
		},
	}, nil, nil
}

// GetProcessDetailArgs defines the parameters for the get_process_detail MCP tool
type GetProcessDetailArgs struct {
	Name string `json:"name" jsonschema:"Running-man process name to get detailed info for (required). Example: 'database'"`
}

// registerGetProcessDetailTool registers the get_process_detail MCP tool
func (s *Server) registerGetProcessDetailTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_process_detail",
		Description: `Get detailed information about a specific process managed by running-man.

This tool provides comprehensive details about a single process, including:
- Basic info (name, command, PID, status, exit code)
- Timing information (start time, uptime)
- Recent log statistics from running-man's buffer
- Configuration context

Parameters:
- name: REQUIRED - Process name managed by running-man

Examples:
- Get detailed info for database: {"name": "database"}
- Debug web server issues: {"name": "web-server"}

Use this when you need more detail than 'get_process_status' provides. If the process doesn't exist, you'll get a list of available processes.`,
	}, s.getProcessDetailHandler)

	s.log("Registered MCP tool: get_process_detail", false)
}

// getProcessDetailHandler implements the get_process_detail MCP tool
func (s *Server) getProcessDetailHandler(ctx context.Context, req *mcp.CallToolRequest, args *GetProcessDetailArgs) (*mcp.CallToolResult, any, error) {
	// Validate required name parameter
	if args.Name == "" {
		return nil, nil, fmt.Errorf("'name' parameter is required (process name)")
	}

	// Check if process manager is available
	if s.manager == nil {
		return nil, nil, fmt.Errorf("process manager not available")
	}

	// Get process information
	processes := s.manager.ListProcesses()
	var targetProcess *process.ProcessInfo
	for i, p := range processes {
		if p.Name == args.Name {
			targetProcess = &processes[i]
			break
		}
	}

	if targetProcess == nil {
		// List available processes for helpful error message
		var availableNames []string
		for _, p := range processes {
			availableNames = append(availableNames, p.Name)
		}
		if len(availableNames) == 0 {
			return nil, nil, fmt.Errorf("process '%s' not found (no processes are managed by running-man)", args.Name)
		}
		return nil, nil, fmt.Errorf("process '%s' not found. Available processes: %s", args.Name, strings.Join(availableNames, ", "))
	}

	// Get log statistics for this process
	logFilters := storage.QueryFilters{
		Sources: []string{args.Name},
		Since:   time.Hour, // Last hour of logs
	}
	recentLogs := s.buffer.Query(logFilters)

	// Count log levels
	errorCount := 0
	warnCount := 0
	infoCount := 0
	debugCount := 0
	otherCount := 0

	for _, log := range recentLogs {
		switch log.Level {
		case parser.LevelError:
			errorCount++
		case parser.LevelWarn:
			warnCount++
		case parser.LevelInfo:
			infoCount++
		case parser.LevelDebug:
			debugCount++
		default:
			otherCount++
		}
	}

	// Format detailed process information
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Detailed information for process '%s':\n\n", targetProcess.Name))

	result.WriteString("Basic Information:\n")
	result.WriteString(fmt.Sprintf("  Name:       %s\n", targetProcess.Name))
	result.WriteString(fmt.Sprintf("  Command:    %s\n", targetProcess.Command))
	result.WriteString(fmt.Sprintf("  Status:     %s\n", targetProcess.Status))

	if targetProcess.PID > 0 {
		result.WriteString(fmt.Sprintf("  PID:        %d\n", targetProcess.PID))
	} else {
		result.WriteString("  PID:        Not running\n")
	}

	if targetProcess.ExitCode >= 0 {
		result.WriteString(fmt.Sprintf("  Exit Code:  %d\n", targetProcess.ExitCode))
	} else if targetProcess.ExitCode == -1 {
		result.WriteString("  Exit Code:  Running (-1)\n")
	} else {
		result.WriteString("  Exit Code:  N/A\n")
	}

	result.WriteString("\nTiming Information:\n")
	if !targetProcess.StartTime.IsZero() {
		result.WriteString(fmt.Sprintf("  Start Time: %s\n", targetProcess.StartTime.Format("2006-01-02 15:04:05.000")))
		uptime := time.Since(targetProcess.StartTime).Round(time.Second)
		result.WriteString(fmt.Sprintf("  Uptime:     %s\n", uptime))

		// Calculate human-readable uptime
		hours := int(uptime.Hours())
		minutes := int(uptime.Minutes()) % 60
		seconds := int(uptime.Seconds()) % 60
		if hours > 0 {
			result.WriteString(fmt.Sprintf("             (%d hours, %d minutes, %d seconds)\n", hours, minutes, seconds))
		} else if minutes > 0 {
			result.WriteString(fmt.Sprintf("             (%d minutes, %d seconds)\n", minutes, seconds))
		}
	} else {
		result.WriteString("  Start Time: Not started\n")
		result.WriteString("  Uptime:     N/A\n")
	}

	result.WriteString("\nRecent Log Statistics (last hour):\n")
	result.WriteString(fmt.Sprintf("  Total logs: %d\n", len(recentLogs)))
	if len(recentLogs) > 0 {
		result.WriteString(fmt.Sprintf("  Errors:     %d\n", errorCount))
		result.WriteString(fmt.Sprintf("  Warnings:   %d\n", warnCount))
		result.WriteString(fmt.Sprintf("  Info:       %d\n", infoCount))
		result.WriteString(fmt.Sprintf("  Debug:      %d\n", debugCount))
		if otherCount > 0 {
			result.WriteString(fmt.Sprintf("  Other:      %d\n", otherCount))
		}

		// Show most recent log timestamp
		latestLog := recentLogs[0] // buffer.Query returns newest first
		timeSinceLatest := time.Since(latestLog.Timestamp).Round(time.Second)
		result.WriteString(fmt.Sprintf("  Latest log: %s ago\n", timeSinceLatest))
	}

	result.WriteString("\nRelated Tools:\n")
	result.WriteString("  - Use 'get_startup_logs' to see logs from when this process started\n")
	result.WriteString("  - Use 'search_logs' to search this process's logs\n")
	if targetProcess.Status == "running" {
		result.WriteString("  - Use 'restart_process' to restart this process\n")
	}
	if errorCount > 0 {
		result.WriteString("  - Use 'get_recent_errors' to see error details\n")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: result.String()},
		},
	}, nil, nil
}

// GetHealthStatusArgs defines the parameters for the get_health_status MCP tool
type GetHealthStatusArgs struct {
	Detailed bool `json:"detailed,omitempty" jsonschema:"Include detailed system information. Example: true"`
}

// registerGetHealthStatusTool registers the get_health_status MCP tool
func (s *Server) registerGetHealthStatusTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_health_status",
		Description: `Get health status of the running-man system.

This tool provides a comprehensive health report including:
- Running-man server uptime and status
- Log buffer statistics (size, age, entries)
- Process manager status and process counts
- System resource usage (if available)

Parameters:
- detailed: Optional flag for more detailed system information

Examples:
- Basic health check: {} (empty parameters)
- Detailed system info: {"detailed": true}

Use this tool to monitor the overall health of running-man and its managed processes.`,
	}, s.getHealthStatusHandler)

	s.log("Registered MCP tool: get_health_status", false)
}

// getHealthStatusHandler implements the get_health_status MCP tool
func (s *Server) getHealthStatusHandler(ctx context.Context, req *mcp.CallToolRequest, args *GetHealthStatusArgs) (*mcp.CallToolResult, any, error) {
	// Get buffer statistics
	bufferStats := s.buffer.Stats()
	sourceInfos := s.buffer.GetSources()

	// Extract source names
	var sourceNames []string
	for _, sourceInfo := range sourceInfos {
		sourceNames = append(sourceNames, sourceInfo.Name)
	}

	// Get process information if manager is available
	var processCount int
	var runningCount int
	var stoppedCount int
	var processManagerStatus string

	if s.manager != nil {
		processes := s.manager.ListProcesses()
		processCount = len(processes)
		for _, p := range processes {
			if p.Status == "running" {
				runningCount++
			} else {
				stoppedCount++
			}
		}
		processManagerStatus = "available"
	} else {
		processManagerStatus = "not available"
	}

	// Calculate server uptime
	serverUptime := time.Since(s.startTime)

	// Format health report
	var result strings.Builder
	result.WriteString("Running-man Health Status Report\n")
	result.WriteString(strings.Repeat("=", 40) + "\n\n")

	result.WriteString("Server Status:\n")
	result.WriteString(fmt.Sprintf("  Status:      Healthy\n"))
	result.WriteString(fmt.Sprintf("  Uptime:      %s\n", serverUptime.Round(time.Second)))

	// Human-readable uptime
	hours := int(serverUptime.Hours())
	minutes := int(serverUptime.Minutes()) % 60
	seconds := int(serverUptime.Seconds()) % 60
	if hours > 0 {
		result.WriteString(fmt.Sprintf("              (%d hours, %d minutes, %d seconds)\n", hours, minutes, seconds))
	} else if minutes > 0 {
		result.WriteString(fmt.Sprintf("              (%d minutes, %d seconds)\n", minutes, seconds))
	}

	result.WriteString("\nLog Buffer:\n")
	result.WriteString(fmt.Sprintf("  Total Entries: %d / %d\n", bufferStats.TotalEntries, bufferStats.MaxEntries))

	// Calculate buffer usage percentage
	bufferUsagePct := 0.0
	if bufferStats.MaxEntries > 0 {
		bufferUsagePct = float64(bufferStats.TotalEntries) / float64(bufferStats.MaxEntries) * 100
	}
	result.WriteString(fmt.Sprintf("  Usage:         %.1f%%\n", bufferUsagePct))

	result.WriteString(fmt.Sprintf("  Total Size:    %s / %s\n",
		formatBytes(bufferStats.TotalBytes), formatBytes(bufferStats.MaxBytes)))

	if !bufferStats.OldestEntry.IsZero() {
		oldestAge := time.Since(bufferStats.OldestEntry).Round(time.Second)
		result.WriteString(fmt.Sprintf("  Oldest Entry:  %s (%s ago)\n",
			bufferStats.OldestEntry.Format("15:04:05"), oldestAge))
	}

	if !bufferStats.NewestEntry.IsZero() {
		newestAge := time.Since(bufferStats.NewestEntry).Round(time.Second)
		result.WriteString(fmt.Sprintf("  Newest Entry:  %s (%s ago)\n",
			bufferStats.NewestEntry.Format("15:04:05"), newestAge))
	}

	result.WriteString(fmt.Sprintf("  Max Age:       %s\n", bufferStats.MaxAge.Round(time.Second)))
	result.WriteString(fmt.Sprintf("  Sources:       %d unique\n", len(sourceNames)))

	result.WriteString("\nProcess Management:\n")
	result.WriteString(fmt.Sprintf("  Manager:       %s\n", processManagerStatus))
	if s.manager != nil {
		result.WriteString(fmt.Sprintf("  Total Processes: %d\n", processCount))
		result.WriteString(fmt.Sprintf("    Running:      %d\n", runningCount))
		result.WriteString(fmt.Sprintf("    Stopped:      %d\n", stoppedCount))

		if len(sourceNames) > 0 {
			result.WriteString("\n  Managed Sources:\n")
			// Show first 5 sources
			for i, source := range sourceNames {
				if i < 5 {
					result.WriteString(fmt.Sprintf("    - %s\n", source))
				} else {
					result.WriteString(fmt.Sprintf("    ... and %d more\n", len(sourceNames)-5))
					break
				}
			}
		}
	}

	// Add detailed system info if requested
	if args.Detailed {
		result.WriteString("\n" + strings.Repeat("-", 40) + "\n")
		result.WriteString("Detailed System Information:\n")

		// Buffer age distribution (simplified)
		result.WriteString("\n  Buffer Age Distribution:\n")
		result.WriteString(fmt.Sprintf("    Max Retention: %s\n", bufferStats.MaxAge.Round(time.Second)))

		// Process status breakdown
		if s.manager != nil && processCount > 0 {
			result.WriteString("\n  Process Status Breakdown:\n")
			processes := s.manager.ListProcesses()
			for _, p := range processes {
				statusIcon := "✓"
				if p.Status != "running" {
					statusIcon = "✗"
				}
				result.WriteString(fmt.Sprintf("    %s %s: %s", statusIcon, p.Name, p.Status))
				if p.Status == "running" && p.PID > 0 {
					result.WriteString(fmt.Sprintf(" (PID: %d)", p.PID))
				}
				result.WriteString("\n")
			}
		}
	}

	result.WriteString("\n" + strings.Repeat("=", 40) + "\n")
	result.WriteString("Recommendations:\n")

	if bufferUsagePct > 80 {
		result.WriteString("  ⚠  Log buffer is nearly full (" + fmt.Sprintf("%.1f%%", bufferUsagePct) + ")\n")
		result.WriteString("     Consider increasing buffer size or reviewing log volume\n")
	}

	if runningCount == 0 && processCount > 0 {
		result.WriteString("  ⚠  No processes are currently running\n")
		result.WriteString("     Check process status with 'get_process_status'\n")
	}

	if len(sourceNames) == 0 {
		result.WriteString("  ℹ  No log sources detected\n")
		result.WriteString("     Processes may not be configured or logs not yet captured\n")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: result.String()},
		},
	}, nil, nil
}

// formatBytes converts bytes to human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// max returns the larger of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
