package api

import (
	"net/http"

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

// registerSearchLogsTool registers the search_logs MCP tool
// TODO: Implement in task the_running_man-o8k
func (s *Server) registerSearchLogsTool(server *mcp.Server) {
	// Placeholder - tool registration will be implemented in next task
	s.log("MCP tool placeholder: search_logs", false)
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
