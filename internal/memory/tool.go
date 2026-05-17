package memory

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// ToolResult represents the result of a memory operation
type ToolResult struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// MemoryTool provides CLI commands for memory operations
type MemoryTool struct {
	store *Store
}

// NewMemoryTool creates a new memory tool
func NewMemoryTool(store *Store) *MemoryTool {
	return &MemoryTool{store: store}
}

// Commands returns the memory CLI commands
func (m *MemoryTool) Commands() []*cobra.Command {
	return []*cobra.Command{
		m.storeCommand(),
		m.recallCommand(),
		m.searchCommand(),
		m.listCommand(),
		m.deleteCommand(),
		m.summarizeCommand(),
		m.agentMemoryCommand(),
		m.userMemoryCommand(),
	}
}

func (m *MemoryTool) storeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Store a new memory",
		Long:  `Store a new memory with optional type, scope, and categories`,
		RunE: func(cmd *cobra.Command, args []string) error {
			content, _ := cmd.Flags().GetString("content")
			mType, _ := cmd.Flags().GetString("type")
			scope, _ := cmd.Flags().GetString("scope")
			cats, _ := cmd.Flags().GetStringSlice("category")
			importance, _ := cmd.Flags().GetFloat64("importance")

			mem := &Memory{
				Type:       MemoryType(mType),
				Content:    content,
				Scope:      scope,
				Categories: cats,
				Importance: importance,
			}

			if err := m.store.Store(mem); err != nil {
				return fmt.Errorf("failed to store memory: %w", err)
			}

			fmt.Printf("Memory stored successfully: %s\n", mem.ID)
			return nil
		},
	}

	cmd.Flags().StringP("content", "c", "", "Memory content")
	cmd.Flags().StringP("type", "t", "agent", "Memory type (agent, user, session, project, knowledge, preference)")
	cmd.Flags().StringP("scope", "s", "", "Memory scope (e.g., /infrastructure/database)")
	cmd.Flags().StringSliceP("category", "C", []string{}, "Categories/tags")
	cmd.Flags().Float64P("importance", "i", 0.5, "Importance level (0.0-1.0)")
	cmd.MarkFlagRequired("content")

	return cmd
}

func (m *MemoryTool) recallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recall [query]",
		Short: "Recall relevant memories",
		Long:  `Search and recall memories relevant to the query`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			limit, _ := cmd.Flags().GetInt("limit")
			mType, _ := cmd.Flags().GetString("type")

			var memories []*Memory
			var err error

			if mType != "" {
				memories, err = m.store.Recall(query, limit, MemoryType(mType))
			} else {
				memories, err = m.store.Recall(query, limit)
			}

			if err != nil {
				return fmt.Errorf("failed to recall memories: %w", err)
			}

			if len(memories) == 0 {
				fmt.Println("No matching memories found.")
				return nil
			}

			fmt.Printf("Found %d matching memories:\n\n", len(memories))
			for i, mem := range memories {
				fmt.Printf("## [%d] %s (%s)\n", i+1, mem.ID, mem.Type)
				fmt.Printf("   Scope: %s\n", mem.Scope)
				fmt.Printf("   Categories: %s\n", strings.Join(mem.Categories, ", "))
				fmt.Printf("   Importance: %.2f\n", mem.Importance)
				fmt.Printf("   Content:\n%s\n\n", mem.Content)
			}

			return nil
		},
	}

	cmd.Flags().IntP("limit", "l", 10, "Maximum results to return")
	cmd.Flags().StringP("type", "t", "", "Filter by memory type")

	return cmd
}

func (m *MemoryTool) searchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Full-text search memories",
		Long:  `Perform FTS5 full-text search across all memories`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			limit, _ := cmd.Flags().GetInt("limit")

			memories, err := m.store.Search(query, limit)
			if err != nil {
				return fmt.Errorf("failed to search: %w", err)
			}

			if len(memories) == 0 {
				fmt.Println("No results found.")
				return nil
			}

			fmt.Printf("Search results for '%s':\n\n", query)
			for i, mem := range memories {
				fmt.Printf("[%d] %s - %s\n", i+1, mem.Type, mem.Scope)
				fmt.Printf("    %s\n\n", truncate(mem.Content, 200))
			}

			return nil
		},
	}

	cmd.Flags().IntP("limit", "l", 20, "Maximum results")

	return cmd
}

func (m *MemoryTool) listCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all memories",
		Long:  `List memories with optional type filter`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mType, _ := cmd.Flags().GetString("type")
			limit, _ := cmd.Flags().GetInt("limit")
			offset, _ := cmd.Flags().GetInt("offset")

			var memories []*Memory
			var err error

			if mType != "" {
				memories, err = m.store.List(MemoryType(mType), limit, offset)
			} else {
				memories, err = m.store.List("", limit, offset)
			}

			if err != nil {
				return fmt.Errorf("failed to list memories: %w", err)
			}

			if len(memories) == 0 {
				fmt.Println("No memories found.")
				return nil
			}

			// Get stats
			stats, _ := m.store.Stats()
			fmt.Printf("Total memories: %d\n\n", stats.TotalMemories)

			for _, mem := range memories {
				fmt.Printf("[%s] %s | %s | importance: %.2f | accessed: %d\n",
					mem.ID[:8], mem.Type, mem.Scope, mem.Importance, mem.AccessCount)
			}

			return nil
		},
	}

	cmd.Flags().StringP("type", "t", "", "Filter by type")
	cmd.Flags().IntP("limit", "l", 50, "Limit results")
	cmd.Flags().IntP("offset", "o", 0, "Offset for pagination")

	return cmd
}

func (m *MemoryTool) deleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete a memory",
		Long:  `Delete a memory by ID`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := m.store.Delete(id); err != nil {
				return fmt.Errorf("failed to delete: %w", err)
			}
			fmt.Printf("Memory %s deleted.\n", id)
			return nil
		},
	}

	return cmd
}

func (m *MemoryTool) summarizeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "summarize [query]",
		Short: "Summarize relevant memories",
		Long:  `Get an LLM-powered summary of relevant memories`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			memories, err := m.store.Recall(query, 10)
			if err != nil {
				return fmt.Errorf("failed to recall: %w", err)
			}

			summary, err := m.store.Summarize(memories)
			if err != nil {
				return fmt.Errorf("failed to summarize: %w", err)
			}

			fmt.Println("=== Memory Summary ===")
			fmt.Println(summary)
			return nil
		},
	}

	return cmd
}

func (m *MemoryTool) agentMemoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent-memory",
		Short: "Manage agent memory file",
		Long:  `Read or write to the Cortex-style agent memory file`,
	}

	readCmd := &cobra.Command{
		Use:   "read",
		Short: "Read agent memory",
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := m.store.ReadAgentMemory()
			if err != nil {
				return err
			}
			fmt.Println(content)
			return nil
		},
	}
	cmd.AddCommand(readCmd)

	writeCmd := &cobra.Command{
		Use:   "write [content]",
		Short: "Write agent memory",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content := strings.Join(args, " ")
			return m.store.WriteAgentMemory(content)
		},
	}
	cmd.AddCommand(writeCmd)

	appendCmd := &cobra.Command{
		Use:   "append [content]",
		Short: "Append to agent memory",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content := strings.Join(args, " ")
			return m.store.AppendAgentMemory(content)
		},
	}
	cmd.AddCommand(appendCmd)

	return cmd
}

func (m *MemoryTool) userMemoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user-memory",
		Short: "Manage user profile file",
		Long:  `Read or write to the Cortex-style user profile file`,
	}

	readCmd := &cobra.Command{
		Use:   "read",
		Short: "Read user memory",
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := m.store.ReadUserMemory()
			if err != nil {
				return err
			}
			fmt.Println(content)
			return nil
		},
	}
	cmd.AddCommand(readCmd)

	writeCmd := &cobra.Command{
		Use:   "write [content]",
		Short: "Write user memory",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content := strings.Join(args, " ")
			return m.store.WriteUserMemory(content)
		},
	}
	cmd.AddCommand(writeCmd)

	appendCmd := &cobra.Command{
		Use:   "append [content]",
		Short: "Append to user memory",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content := strings.Join(args, " ")
			return m.store.AppendUserMemory(content)
		},
	}
	cmd.AddCommand(appendCmd)

	return cmd
}

// truncate shortens a string to maxLen characters
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// ToolFunction represents a callable memory tool (for API use)
type ToolFunction struct {
	store *Store
}

// NewToolFunction creates a new memory tool function
func NewToolFunction(store *Store) *ToolFunction {
	return &ToolFunction{store: store}
}

// ToolDefinition returns the OpenAI-style tool definition
func (t *ToolFunction) ToolDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "memory",
			"description": "Manage persistent memories - store, recall, search, and summarize information across sessions",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"store", "recall", "search", "list", "delete", "summarize", "agent_memory", "user_memory"},
						"description": "The memory operation to perform",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The content or query for the operation",
					},
					"memory_type": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"agent", "user", "session", "project", "knowledge", "preference"},
						"description": "Type of memory",
					},
					"scope": map[string]interface{}{
						"type":        "string",
						"description": "Scope/path for the memory (e.g., /infrastructure/database)",
					},
					"categories": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Categories or tags for the memory",
					},
					"importance": map[string]interface{}{
						"type":        "number",
						"description": "Importance level (0.0-1.0)",
					},
					"memory_id": map[string]interface{}{
						"type":        "string",
						"description": "Memory ID for delete operation",
					},
				},
				"required": []string{"operation"},
			},
		},
	}
}

// Execute runs a memory operation
func (t *ToolFunction) Execute(params map[string]interface{}) (*ToolResult, error) {
	op, _ := params["operation"].(string)

	switch op {
	case "store":
		return t.executeStore(params)
	case "recall":
		return t.executeRecall(params)
	case "search":
		return t.executeSearch(params)
	case "list":
		return t.executeList(params)
	case "delete":
		return t.executeDelete(params)
	case "summarize":
		return t.executeSummarize(params)
	case "agent_memory":
		return t.executeAgentMemory(params)
	case "user_memory":
		return t.executeUserMemory(params)
	default:
		return &ToolResult{Success: false, Error: "Unknown operation: " + op}, nil
	}
}

func (t *ToolFunction) executeStore(params map[string]interface{}) (*ToolResult, error) {
	content, _ := params["content"].(string)
	mType, _ := params["memory_type"].(string)
	scope, _ := params["scope"].(string)
	importance, _ := params["importance"].(float64)

	cats := []string{}
	if c, ok := params["categories"].([]interface{}); ok {
		for _, v := range c {
			if s, ok := v.(string); ok {
				cats = append(cats, s)
			}
		}
	}

	if mType == "" {
		mType = "agent"
	}

	mem := &Memory{
		Type:       MemoryType(mType),
		Content:    content,
		Scope:      scope,
		Categories: cats,
		Importance: importance,
	}

	if err := t.store.Store(mem); err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Data: map[string]string{"id": mem.ID}}, nil
}

func (t *ToolFunction) executeRecall(params map[string]interface{}) (*ToolResult, error) {
	query, _ := params["content"].(string)
	mType, _ := params["memory_type"].(string)

	var memories []*Memory
	var err error

	if mType != "" {
		memories, err = t.store.Recall(query, 10, MemoryType(mType))
	} else {
		memories, err = t.store.Recall(query, 10)
	}

	if err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	data, _ := json.Marshal(memories)
	return &ToolResult{Success: true, Data: json.RawMessage(data)}, nil
}

func (t *ToolFunction) executeSearch(params map[string]interface{}) (*ToolResult, error) {
	query, _ := params["content"].(string)

	memories, err := t.store.Search(query, 20)
	if err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	data, _ := json.Marshal(memories)
	return &ToolResult{Success: true, Data: json.RawMessage(data)}, nil
}

func (t *ToolFunction) executeList(params map[string]interface{}) (*ToolResult, error) {
	mType, _ := params["memory_type"].(string)

	memories, err := t.store.List(MemoryType(mType), 50, 0)
	if err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	data, _ := json.Marshal(memories)
	return &ToolResult{Success: true, Data: json.RawMessage(data)}, nil
}

func (t *ToolFunction) executeDelete(params map[string]interface{}) (*ToolResult, error) {
	id, _ := params["memory_id"].(string)

	if err := t.store.Delete(id); err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Data: map[string]string{"deleted": id}}, nil
}

func (t *ToolFunction) executeSummarize(params map[string]interface{}) (*ToolResult, error) {
	query, _ := params["content"].(string)

	memories, err := t.store.Recall(query, 10)
	if err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	summary, err := t.store.Summarize(memories)
	if err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Data: map[string]string{"summary": summary}}, nil
}

func (t *ToolFunction) executeAgentMemory(params map[string]interface{}) (*ToolResult, error) {
	subOp, _ := params["content"].(string)

	var content string
	var err error

	switch subOp {
	case "read":
		content, err = t.store.ReadAgentMemory()
	default:
		return &ToolResult{Success: false, Error: "Use 'read' as content for agent_memory read"}, nil
	}

	if err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Data: map[string]string{"content": content}}, nil
}

func (t *ToolFunction) executeUserMemory(params map[string]interface{}) (*ToolResult, error) {
	subOp, _ := params["content"].(string)

	var content string
	var err error

	switch subOp {
	case "read":
		content, err = t.store.ReadUserMemory()
	default:
		return &ToolResult{Success: false, Error: "Use 'read' as content for user_memory read"}, nil
	}

	if err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	return &ToolResult{Success: true, Data: map[string]string{"content": content}}, nil
}
