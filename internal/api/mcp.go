package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/iangeorge/the_running_man/internal/parser"
	"github.com/iangeorge/the_running_man/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// createMCPHandler creates an HTTP handler for the Model Context Protocol (MCP) endpoint.
// MCP provides a standardized interface for AI agents to query logs, check process status,
// and retrieve operational data. This enables integration with Claude, GPT, and other AI systems.
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

	// Register the 4 MCP tools
	// TODO: Implement these in separate tasks
	s.registerSearchLogsTool(server)
	s.registerGetRecentErrorsTool(server)
	s.registerGetProcessStatusTool(server)
	s.registerGetStartupLogsTool(server)

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
	Source   string `json:"source,omitempty" jsonschema:"description=Filter by source (supports glob patterns like backend*)"`
	Since    string `json:"since,omitempty" jsonschema:"description=Time window (e.g. 5m, 1h, 30s)"`
	Level    string `json:"level,omitempty" jsonschema:"description=Filter by log level (error/warn/info/debug)"`
	Contains string `json:"contains,omitempty" jsonschema:"description=Search for text in log messages"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=Maximum number of log entries to return;default=50;minimum=1;maximum=1000"`
}

// registerSearchLogsTool registers the search_logs MCP tool
func (s *Server) registerSearchLogsTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_logs",
		Description: "Search and filter log entries from the running-man buffer. Supports filtering by source, time window, log level, and text content.",
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

// registerGetRecentErrorsTool registers the get_recent_errors MCP tool
// TODO: Implement in task the_running_man-k39
func (s *Server) registerGetRecentErrorsTool(server *mcp.Server) {
	// Placeholder - tool registration will be implemented in next task
	s.log("MCP tool placeholder: get_recent_errors", false)
}

// registerGetProcessStatusTool registers the get_process_status MCP tool
// TODO: Implement in task the_running_man-bpd
func (s *Server) registerGetProcessStatusTool(server *mcp.Server) {
	// Placeholder - tool registration will be implemented in next task
	s.log("MCP tool placeholder: get_process_status", false)
}

// registerGetStartupLogsTool registers the get_startup_logs MCP tool
// TODO: Implement in task the_running_man-zhn
func (s *Server) registerGetStartupLogsTool(server *mcp.Server) {
	// Placeholder - tool registration will be implemented in next task
	s.log("MCP tool placeholder: get_startup_logs", false)
}
