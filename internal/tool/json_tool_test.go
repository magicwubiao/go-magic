package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func testToolsJSON() string {
	return `{"tools":[{"name":"alpha","ver":1},{"name":"beta","ver":2}],"count":2}`
}

func TestJSONQueryArrayIndex(t *testing.T) {
	tool := NewJSONTool()
	ctx := context.Background()

	cases := []struct {
		name string
		path string
		data string
		want string // 期望结果 JSON 序列化后包含的文本
	}{
		{"root key returns array", "$.tools", testToolsJSON(), `"name":"alpha"`},
		{"key[0] index", "$.tools[0]", testToolsJSON(), `"name":"alpha"`},
		{"key[0].field", "$.tools[0].name", testToolsJSON(), "alpha"},
		{"key[1].field", "$.tools[1].name", testToolsJSON(), "beta"},
		{"$[0] on root array", "$[0]", `["alpha","beta"]`, "alpha"},
		{"$[1].key on root array", "$[1].name", `[{"name":"alpha"},{"name":"beta"}]`, "beta"},
		{"chained index on nested array", "$.matrix[1][0]", `{"matrix":[[1,2],[3,4]]}`, "3"},
		{"wildcard all elements", "$.tools[*].name", testToolsJSON(), `"beta"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := tool.Execute(ctx, map[string]any{
				"operation": "query",
				"data":      tc.data,
				"path":      tc.path,
			})
			if err != nil {
				t.Fatalf("query %s: %v", tc.path, err)
			}
			b, _ := json.Marshal(res)
			if !strings.Contains(string(b), tc.want) {
				t.Fatalf("query %s = %s, want containing %s", tc.path, b, tc.want)
			}
		})
	}
}

func TestJSONQueryErrors(t *testing.T) {
	tool := NewJSONTool()
	ctx := context.Background()
	data := testToolsJSON()

	// 越界索引必须报错而不是静默返回空
	if _, err := tool.Execute(ctx, map[string]any{
		"operation": "query", "data": data, "path": "$.tools[5]",
	}); err == nil {
		t.Fatal("out-of-range index must error")
	}
	// 不存在的 key 必须报错
	if _, err := tool.Execute(ctx, map[string]any{
		"operation": "query", "data": data, "path": "$.tools[0].nope",
	}); err == nil {
		t.Fatal("missing key must error")
	}
	// 非法索引格式报错
	if _, err := tool.Execute(ctx, map[string]any{
		"operation": "query", "data": data, "path": "$.tools[abc]",
	}); err == nil {
		t.Fatal("invalid index must error")
	}
}

func TestYAMLToListJSON(t *testing.T) {
	tool := NewYAMLTool()
	ctx := context.Background()

	yamlData := `name: demo
tools:
  - id: t1
    cmd: ls
  - id: t2
    cmd: pwd
  - id: t3
    cmd: echo hi
  - id: t4
    cmd: whoami
enabled: true
`

	res, err := tool.Execute(ctx, map[string]any{
		"operation": "to_json",
		"data":      yamlData,
	})
	if err != nil {
		t.Fatalf("to_json: %v", err)
	}

	s, ok := res.(string)
	if !ok {
		t.Fatalf("to_json must return string, got %T", res)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, s)
	}

	arr, ok := parsed["tools"].([]any)
	if !ok {
		t.Fatalf("tools must be a JSON array, got %T (output: %s)", parsed["tools"], s)
	}
	if len(arr) != 4 {
		t.Fatalf("tools must have 4 items, got %d (output: %s)", len(arr), s)
	}
	first := arr[0].(map[string]any)
	if first["id"] != "t1" || first["cmd"] != "ls" {
		t.Fatalf("first tool item wrong: %v", first)
	}
	if parsed["enabled"] != true {
		t.Fatalf("enabled must be boolean true, got %v (%T)", parsed["enabled"], parsed["enabled"])
	}
}

func TestYAMLRoundTrip(t *testing.T) {
	tool := NewYAMLTool()
	ctx := context.Background()

	// JSON → YAML → JSON 往返保持列表结构
	jsonData := `{"servers":[{"host":"a.example","port":80},{"host":"b.example","port":443}],"tags":["x","y"]}`

	y, err := tool.Execute(ctx, map[string]any{"operation": "from_json", "data": jsonData})
	if err != nil {
		t.Fatalf("from_json: %v", err)
	}
	back, err := tool.Execute(ctx, map[string]any{"operation": "to_json", "data": y.(string)})
	if err != nil {
		t.Fatalf("to_json: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(back.(string)), &result); err != nil {
		t.Fatalf("round-trip invalid JSON: %v\n%s", err, back)
	}
	if servers, ok := result["servers"].([]any); !ok || len(servers) != 2 {
		t.Fatalf("servers list lost in round-trip: %s", back)
	}
	if tags, ok := result["tags"].([]any); !ok || len(tags) != 2 {
		t.Fatalf("tags list lost in round-trip: %s", back)
	}
}
