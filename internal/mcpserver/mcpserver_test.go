package mcpserver

import (
	"context"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AddTool panics when it cannot infer a schema from a tool's input or
// output type, so registering every tool proves the domain types are
// schema-safe.
func TestToolSchemasInfer(t *testing.T) {
	addTools(mcp.NewServer(&mcp.Implementation{Name: "uncrm", Version: "test"}, nil), nil)
}

// An object schema with no properties that forbids additional ones accepts
// only {}. Inference produces that for a struct with only unexported fields
// (crm.Email, crm.Domain), which marshals to a string, so every call that
// returns one would fail output validation.
func TestToolSchemasHaveNoEmptyObjects(t *testing.T) {
	ctx := context.Background()
	srv := mcp.NewServer(&mcp.Implementation{Name: "uncrm", Version: "test"}, nil)
	addTools(srv, nil)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		for _, bad := range emptyObjects(tool.Name+".input", tool.InputSchema) {
			t.Errorf("%s accepts only {}", bad)
		}
		for _, bad := range emptyObjects(tool.Name+".output", tool.OutputSchema) {
			t.Errorf("%s accepts only {}", bad)
		}
	}
}

func emptyObjects(path string, v any) []string {
	var out []string
	switch v := v.(type) {
	case map[string]any:
		props, _ := v["properties"].(map[string]any)
		if v["type"] == "object" && len(props) == 0 && v["additionalProperties"] == false {
			out = append(out, path)
		}
		for k, child := range v {
			out = append(out, emptyObjects(path+"."+k, child)...)
		}
	case []any:
		for i, child := range v {
			out = append(out, emptyObjects(fmt.Sprintf("%s[%d]", path, i), child)...)
		}
	}
	return out
}
