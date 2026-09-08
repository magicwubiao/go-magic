package mcpbridge

import (
	"context"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/internal/tool"
)

// registryStubTool is a minimal tool.Tool used to plant pre-registered
// entries (including stale mcp_* ones) into a tool.Registry in tests.
type registryStubTool struct {
	name        string
	description string
	schema      map[string]interface{}
}

func (t *registryStubTool) Name() string        { return t.name }
func (t *registryStubTool) Description() string { return t.description }
func (t *registryStubTool) Schema() map[string]interface{} {
	if t.schema == nil {
		return map[string]interface{}{"type": "object"}
	}
	return t.schema
}
func (t *registryStubTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return "ok", nil
}

// newServerTool returns an mcp.Tool as discovered from a standalone MCP server.
func newServerTool(name string) mcp.Tool {
	return mcp.Tool{
		Name:        name,
		Description: "server tool " + name,
		InputSchema: map[string]interface{}{"type": "object"},
	}
}

func TestNamespaceHelpers(t *testing.T) {
	if mcp.NamespacePrefix != "mcp_" {
		t.Fatalf("NamespacePrefix = %q, want mcp_", mcp.NamespacePrefix)
	}
	if got := mcp.ToolPrefix("filesystem"); got != "mcp_filesystem_" {
		t.Fatalf("ToolPrefix(filesystem) = %q, want mcp_filesystem_", got)
	}
	if got := mcp.ToolName("filesystem", "read_file"); got != "mcp_filesystem_read_file" {
		t.Fatalf("ToolName = %q, want mcp_filesystem_read_file", got)
	}
}

func TestMCPToolAdapter(t *testing.T) {
	mgr := mcp.NewManager()
	mt := mcp.NewMCPTool("files", newServerTool("read"), mgr)

	if got := mt.Name(); got != "mcp_files_read" {
		t.Fatalf("Name() = %q, want mcp_files_read", got)
	}
	if got := mt.Description(); !strings.Contains(got, "server tool read") {
		t.Fatalf("Description() = %q should mention the server tool", got)
	}
	if got := mt.Schema(); got == nil || got["type"] != "object" {
		t.Fatalf("Schema() = %v, want object schema", got)
	}

	// Adapter routes execution through the manager; calling a server that is
	// not connected must surface the manager's error (not a nil deref).
	_, err := mt.Execute(context.Background(), map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Execute() error = %v, want manager 'not found' error", err)
	}
}

func TestRemoveServerTools(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := mcp.NewManager()
	reg.Register(mcp.NewMCPTool("files", newServerTool("read"), mgr))
	reg.Register(mcp.NewMCPTool("files", newServerTool("write"), mgr))
	reg.Register(mcp.NewMCPTool("db", newServerTool("query"), mgr))
	reg.Register(&registryStubTool{name: "web_search", description: "builtin"})

	RemoveServerTools(reg, "files")

	if reg.HasTool("mcp_files_read") || reg.HasTool("mcp_files_write") {
		t.Fatalf("RemoveServerTools(files) left files tools behind: %v", reg.List())
	}
	if !reg.HasTool("mcp_db_query") {
		t.Fatalf("RemoveServerTools(files) removed unrelated server tools: %v", reg.List())
	}
	if !reg.HasTool("web_search") {
		t.Fatalf("RemoveServerTools removed a non-MCP builtin tool")
	}

	// Removing an unknown server is a no-op.
	RemoveServerTools(reg, "ghost")
	if !reg.HasTool("mcp_db_query") {
		t.Fatalf("RemoveServerTools(ghost) removed mcp_db_query")
	}
}

func TestSyncToRegistryCleansStaleAndKeepsHealthy(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := mcp.NewManager()

	// A stale mcp_* entry whose server no longer exists must be purged.
	reg.Register(&registryStubTool{name: "mcp_ghost_tool", description: "stale"})
	reg.Register(&registryStubTool{name: "read_file", description: "builtin"})

	got := SyncToRegistry(mgr, reg)

	if got != 0 {
		t.Fatalf("SyncToRegistry with no connected servers = %d, want 0", got)
	}
	if reg.HasTool("mcp_ghost_tool") {
		t.Fatalf("SyncToRegistry left stale mcp_ghost_tool behind: %v", reg.List())
	}
	if !reg.HasTool("read_file") {
		t.Fatalf("SyncToRegistry removed non-MCP builtin tool")
	}
}

func TestSyncServerToRegistryUnknownServer(t *testing.T) {
	reg := tool.NewRegistry()
	mgr := mcp.NewManager()

	reg.Register(&registryStubTool{name: "mcp_db_query", description: "stale"})
	reg.Register(&registryStubTool{name: "read_file", description: "builtin"})

	got := SyncServerToRegistry(mgr, reg, "db")

	if got != 0 {
		t.Fatalf("SyncServerToRegistry(unknown) = %d, want 0", got)
	}
	if reg.HasTool("mcp_db_query") {
		t.Fatalf("SyncServerToRegistry(db) left stale mcp_db_query behind")
	}
	if !reg.HasTool("read_file") {
		t.Fatalf("SyncServerToRegistry removed non-MCP builtin tool")
	}
}

func TestNilRegistryAndManagerAreSafe(t *testing.T) {
	if got := SyncToRegistry(nil, nil); got != 0 {
		t.Fatalf("SyncToRegistry(nil, nil) = %d, want 0", got)
	}
	if got := SyncServerToRegistry(nil, nil, "x"); got != 0 {
		t.Fatalf("SyncServerToRegistry(nil, nil) = %d, want 0", got)
	}
	// Must not panic.
	RemoveServerTools(nil, "x")

	reg := tool.NewRegistry()
	if got := SyncServerToRegistry(nil, reg, "x"); got != 0 {
		t.Fatalf("SyncServerToRegistry(nil manager) = %d, want 0", got)
	}
}

func TestConnectAndSyncNilManager(t *testing.T) {
	err := ConnectAndSync(nil, tool.NewRegistry(), nil)
	if err == nil {
		t.Fatalf("ConnectAndSync with nil manager should return an error")
	}
	if !strings.Contains(err.Error(), "mcp manager is nil") {
		t.Fatalf("ConnectAndSync error = %v, want 'mcp manager is nil'", err)
	}
}
