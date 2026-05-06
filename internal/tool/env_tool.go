package tool

import (
	"context"
	"fmt"
	"os"
	"runtime"
)

// ============================================================================
// Env Tool - 环境变量工具
// ============================================================================

type EnvTool struct {
	BaseTool
}

func NewEnvTool() *EnvTool {
	return &EnvTool{
		BaseTool: *NewBaseTool(
			"env",
			"Read and manage environment variables",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"description": "Operation: get, set, list, unset, get_all",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Environment variable name",
					},
					"value": map[string]any{
						"type":        "string",
						"description": "Value to set (for 'set' operation)",
					},
				},
				"required": []any{"operation"},
			},
		),
	}
}

func (t *EnvTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	operation, _ := args["operation"].(string)

	switch operation {
	case "get":
		return t.get(args)
	case "set":
		return t.set(args)
	case "list":
		return t.list(args)
	case "unset":
		return t.unset(args)
	case "get_all":
		return t.getAll()
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
}

func (t *EnvTool) get(args map[string]any) (map[string]any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name is required for get operation")
	}

	value := os.Getenv(name)
	return map[string]any{
		"name":  name,
		"value": value,
		"exists": value != "",
	}, nil
}

func (t *EnvTool) set(args map[string]any) (map[string]any, error) {
	name, _ := args["name"].(string)
	value, _ := args["value"].(string)

	if name == "" {
		return nil, fmt.Errorf("name is required for set operation")
	}

	if err := os.Setenv(name, value); err != nil {
		return nil, fmt.Errorf("failed to set environment variable: %w", err)
	}

	return map[string]any{
		"name":  name,
		"value": value,
		"success": true,
	}, nil
}

func (t *EnvTool) list(args map[string]any) (map[string]any, error) {
	prefix := ""
	if p, ok := args["name"].(string); ok {
		prefix = p
	}

	envVars := make(map[string]string)
	for _, env := range os.Environ() {
		parts := splitEnv(env)
		if len(parts) == 2 {
			if prefix == "" || len(parts[0]) >= len(prefix) && parts[0][:len(prefix)] == prefix {
				envVars[parts[0]] = parts[1]
			}
		}
	}

	return map[string]any{
		"variables": envVars,
		"count": len(envVars),
	}, nil
}

func (t *EnvTool) unset(args map[string]any) (map[string]any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name is required for unset operation")
	}

	if err := os.Unsetenv(name); err != nil {
		return nil, fmt.Errorf("failed to unset environment variable: %w", err)
	}

	return map[string]any{
		"name":    name,
		"success": true,
	}, nil
}

func (t *EnvTool) getAll() (map[string]any, error) {
	envVars := make(map[string]string)
	for _, env := range os.Environ() {
		parts := splitEnv(env)
		if len(parts) == 2 {
			envVars[parts[0]] = parts[1]
		}
	}

	return map[string]any{
		"variables": envVars,
		"count":    len(envVars),
	}, nil
}

func splitEnv(env string) []string {
	for i := 0; i < len(env); i++ {
		if env[i] == '=' {
			return []string{env[:i], env[i+1:]}
		}
	}
	return []string{env}
}

// ============================================================================
// System Info Tool - 系统信息工具
// ============================================================================

type SystemInfoTool struct {
	BaseTool
}

func NewSystemInfoTool() *SystemInfoTool {
	return &SystemInfoTool{
		BaseTool: *NewBaseTool(
			"system",
			"Get system information: OS, CPU, memory, Go version",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"info_type": map[string]any{
						"type":        "string",
						"description": "Type of info: os, cpu, memory, go, all",
						"default":     "all",
					},
				},
			},
		),
	}
}

func (t *SystemInfoTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	infoType := "all"
	if it, ok := args["info_type"].(string); ok {
		infoType = it
	}

	switch infoType {
	case "os":
		return t.osInfo(), nil
	case "cpu":
		return t.cpuInfo(), nil
	case "memory":
		return t.memoryInfo(), nil
	case "go":
		return t.goInfo(), nil
	default:
		return map[string]any{
			"os":      t.osInfo(),
			"cpu":     t.cpuInfo(),
			"memory":  t.memoryInfo(),
			"go":      t.goInfo(),
		}, nil
	}
}

func (t *SystemInfoTool) osInfo() map[string]any {
	return map[string]any{
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
		"version": runtime.Version(),
	}
}

func (t *SystemInfoTool) cpuInfo() map[string]any {
	return map[string]any{
		"num_cpu":    runtime.NumCPU(),
		"num_goroutine": runtime.NumGoroutine(),
	}
}

func (t *SystemInfoTool) memoryInfo() map[string]any {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	return map[string]any{
		"alloc":      m.Alloc,
		"total_alloc": m.TotalAlloc,
		"sys":        m.Sys,
		"num_gc":     m.NumGC,
		"go_version": runtime.Version(),
	}
}

func (t *SystemInfoTool) goInfo() map[string]any {
	return map[string]any{
		"version": runtime.Version(),
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
	}
}
