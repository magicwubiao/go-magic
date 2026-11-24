package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
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
		"name":   name,
		"value":  value,
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
		"name":    name,
		"value":   value,
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
		"count":     len(envVars),
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
		"count":     len(envVars),
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
			"os":     t.osInfo(),
			"cpu":    t.cpuInfo(),
			"memory": t.memoryInfo(),
			"go":     t.goInfo(),
		}, nil
	}
}

func (t *SystemInfoTool) osInfo() map[string]any {
	// version 曾经直接返回 runtime.Version()（go1.26.0），info_type=os 的语义
	// 就变成了"操作系统版本 = Go 运行时版本"，非常容易误读。现在显式区分：
	// go_version 是运行时版本，os_version 才是操作系统版本。
	return map[string]any{
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"os_version": osVersion(),
		"go_version": runtime.Version(),
	}
}

func (t *SystemInfoTool) cpuInfo() map[string]any {
	return map[string]any{
		"num_cpu":       runtime.NumCPU(),
		"num_goroutine": runtime.NumGoroutine(),
	}
}

func (t *SystemInfoTool) memoryInfo() map[string]any {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]any{
		"alloc":       m.Alloc,
		"total_alloc": m.TotalAlloc,
		"sys":         m.Sys,
		"num_gc":      m.NumGC,
		"go_version":  runtime.Version(),
	}
}

func (t *SystemInfoTool) goInfo() map[string]any {
	return map[string]any{
		"version": runtime.Version(),
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
	}
}

var (
	osVersionOnce sync.Once
	osVersionVal  string
)

// osVersion 返回操作系统版本字符串（拿不到时返回空串，绝不报错）。
// 结果缓存：system 工具会被频繁调用，没必要每次 fork 一个进程。
func osVersion() string {
	osVersionOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "windows":
			cmd = exec.CommandContext(ctx, "cmd", "/c", "ver")
		case "darwin":
			cmd = exec.CommandContext(ctx, "sw_vers", "-productVersion")
		case "linux":
			// /etc/os-release 比 uname 更贴近用户认知（"Ubuntu 24.04"）。
			if data, err := os.ReadFile("/etc/os-release"); err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					if strings.HasPrefix(line, "PRETTY_NAME=") {
						osVersionVal = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
						return
					}
				}
			}
			cmd = exec.CommandContext(ctx, "uname", "-r")
		default:
			return
		}

		out, err := cmd.Output()
		if err != nil {
			return
		}
		osVersionVal = strings.TrimSpace(string(out))
	})
	return osVersionVal
}
