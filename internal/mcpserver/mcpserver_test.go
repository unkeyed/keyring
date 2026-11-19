package mcpserver

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AddTool panics when it cannot infer a schema from a tool's input or
// output type, so registering every tool proves the domain types are
// schema-safe.
func TestToolSchemasInfer(t *testing.T) {
	addTools(mcp.NewServer(&mcp.Implementation{Name: "uncrm", Version: "test"}, nil), nil)
}
