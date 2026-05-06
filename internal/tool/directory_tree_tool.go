package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DirectoryTreeTool 目录树展示工具
type DirectoryTreeTool struct {
	BaseTool
}

// NewDirectoryTreeTool 创建目录树工具
func NewDirectoryTreeTool() *DirectoryTreeTool {
	return &DirectoryTreeTool{
		BaseTool: *NewBaseTool(
			"directory_tree",
			"Display the directory structure as a tree view. Shows files and folders with optional depth limit.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Root directory path to display",
						"default":     ".",
					},
					"max_depth": map[string]interface{}{
						"type":        "number",
						"description": "Maximum depth to traverse (default: 3)",
						"default":     3,
					},
					"include_hidden": map[string]interface{}{
						"type":        "boolean",
						"description": "Include hidden files and directories",
						"default":     false,
					},
					"exclude": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Patterns to exclude (e.g., '*.log', 'node_modules')",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format: tree, json, list",
						"enum":        []string{"tree", "json", "list"},
						"default":     "tree",
					},
				},
				"required": []string{},
			},
		),
	}
}

// Execute 执行目录树展示
func (t *DirectoryTreeTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	path := "."
	if p, ok := params["path"].(string); ok && p != "" {
		path = p
	}

	maxDepth := 3
	if md, ok := params["max_depth"].(float64); ok {
		maxDepth = int(md)
	}

	includeHidden := false
	if h, ok := params["include_hidden"].(bool); ok {
		includeHidden = h
	}

	format := "tree"
	if f, ok := params["format"].(string); ok && f != "" {
		format = f
	}

	var excludePatterns []string
	if excl, ok := params["exclude"].([]interface{}); ok {
		for _, e := range excl {
			if s, ok := e.(string); ok {
				excludePatterns = append(excludePatterns, s)
			}
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory")
	}

	excludeRegexes := make([]*regexp.Regexp, 0)
	for _, pattern := range excludePatterns {
		// Convert glob pattern to regex
		regex := globToRegex(pattern)
		if r, err := regexp.Compile(regex); err == nil {
			excludeRegexes = append(excludeRegexes, r)
		}
	}

	tree := t.buildTree(absPath, 0, maxDepth, includeHidden, excludeRegexes)

	switch format {
	case "json":
		jsonData, err := json.MarshalIndent(tree, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to marshal JSON: %w", err)
		}
		return string(jsonData), nil
	case "list":
		return t.treeToList(tree, ""), nil
	default:
		return tree, nil
	}
}

type TreeNode struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"` // "file" or "directory"
	Path     string      `json:"path"`
	Children []*TreeNode `json:"children,omitempty"`
	Size     int64       `json:"size,omitempty"`
	Mode     string      `json:"mode,omitempty"`
}

func (t *DirectoryTreeTool) buildTree(dirPath string, depth int, maxDepth int, includeHidden bool, exclude []*regexp.Regexp) *TreeNode {
	node := &TreeNode{
		Name: filepath.Base(dirPath),
		Type: "directory",
		Path: dirPath,
	}

	if depth >= maxDepth {
		return node
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		node.Name = node.Name + " (error: " + err.Error() + ")"
		return node
	}

	// Sort entries: directories first, then files, alphabetically
	sort.Slice(entries, func(i, j int) bool {
		iIsDir := entries[i].IsDir()
		jIsDir := entries[j].IsDir()

		if iIsDir != jIsDir {
			return iIsDir
		}
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files if not included
		if !includeHidden && strings.HasPrefix(name, ".") {
			continue
		}

		// Check exclude patterns
		skipped := false
		for _, re := range exclude {
			if re.MatchString(name) {
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}

		entryPath := filepath.Join(dirPath, name)
		child := &TreeNode{
			Name: name,
			Path: entryPath,
		}

		if entry.IsDir() {
			child.Type = "directory"
			childNode := t.buildTree(entryPath, depth+1, maxDepth, includeHidden, exclude)
			child.Children = []*TreeNode{childNode}
		} else {
			child.Type = "file"
			if info, err := entry.Info(); err == nil {
				child.Size = info.Size()
				child.Mode = info.Mode().String()
			}
		}

		node.Children = append(node.Children, child)
	}

	return node
}

func (t *DirectoryTreeTool) treeToList(node *TreeNode, prefix string) string {
	var lines []string

	connector := "├── "
	if prefix == "" {
		connector = ""
	}

	lines = append(lines, prefix+connector+node.Name)

	if len(node.Children) > 0 {
		for i, child := range node.Children {
			isLast := i == len(node.Children)-1
			childPrefix := prefix + "│   "
			if isLast {
				childPrefix = prefix + "    "
			}

			if child.Type == "directory" && len(child.Children) > 0 {
				lines = append(lines, t.treeToList(child, childPrefix))
			} else {
				suffix := ""
				if child.Size > 0 {
					suffix = fmt.Sprintf(" (%s)", formatSize(child.Size))
				}
				if isLast {
					lines = append(lines, childPrefix+"└── "+child.Name+suffix)
				} else {
					lines = append(lines, childPrefix+"├── "+child.Name+suffix)
				}
			}
		}
	}

	return strings.Join(lines, "\n")
}

func globToRegex(pattern string) string {
	result := regexp.QuoteMeta(pattern)
	result = strings.ReplaceAll(result, `\*`, `[^/]*`)
	result = strings.ReplaceAll(result, `\?`, `.`)
	return `^` + result + `$`
}

func formatSize(bytes int64) string {
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

// ValidateParams 实现 ParamValidator 接口
func (t *DirectoryTreeTool) ValidateParams(params map[string]interface{}) error {
	return ValidateParams(t.Schema(), params)
}
