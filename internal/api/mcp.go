package api

import (
	"encoding/json"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// createMCPHandler creates an HTTP handler for the MCP endpoint
// This provides AI agents with access to log querying and process management
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

	// Wrap MCP server in HTTP handler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// MCP protocol typically uses POST with JSON-RPC
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "MCP endpoint requires POST")
			return
		}

		// TODO: Implement proper MCP protocol handling
		// For now, return a placeholder response
		s.log("MCP request received (handler skeleton only)", false)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"result": map[string]interface{}{
				"server": map[string]string{
					"name":    "the-running-man",
					"version": "0.1.0",
				},
				"status": "skeleton - tools not yet implemented",
			},
		})
	})
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
