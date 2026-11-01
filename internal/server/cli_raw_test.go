package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

// raw 逃生工具：注册、列表可见、试跑可达。

func TestCLIConnectorRawRegisteredAndTestable(t *testing.T) {
	s, _ := newCLITestServer(t)

	connJSON := fmt.Sprintf(`{"command": %q, "args": ["-test.run=TestCLIHelperProcess"], "env": ["CLI_HELPER=1"], "raw": true, "tools": []}`, os.Args[0])

	// raw=true 允许零声明工具
	rec := doCLIRequest(t, s, http.MethodPut, "/api/cli/connectors/helper", connJSON)
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !s.toolReg.HasTool("cli_helper_raw") {
		t.Fatal("raw tool was not registered")
	}

	// 列表视图的 tool_names 包含 raw
	rec = doCLIRequest(t, s, http.MethodGet, "/api/cli/connectors/helper", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
	var view cliConnectorView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	found := false
	for _, n := range view.ToolNames {
		if n == "cli_helper_raw" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tool_names missing raw: %v", view.ToolNames)
	}

	// /api/cli/test 支持 tool=raw
	body := fmt.Sprintf(`{
		"name": "helper",
		"connector": %s,
		"tool": "raw",
		"params": {"args": ["anything", "goes here"]}
	}`, connJSON)
	rec = doCLIRequest(t, s, http.MethodPost, "/api/cli/test", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("test status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK     bool                   `json:"ok"`
		Result map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if !resp.OK {
		t.Fatalf("raw test run failed: %s", rec.Body.String())
	}
	if out, _ := resp.Result["stdout"].(string); !strings.Contains(out, "hello from helper") {
		t.Fatalf("stdout = %q", out)
	}
}

// raw 未开启时，tool=raw 的试跑必须被拒绝。
func TestCLITestRawRequiresSwitch(t *testing.T) {
	s, _ := newCLITestServer(t)

	body := fmt.Sprintf(`{
		"connector": {"command": %q, "args": ["-test.run=TestCLIHelperProcess"], "env": ["CLI_HELPER=1"],
			"tools": [{"name": "hi", "description": "d", "args": ["x"]}]},
		"tool": "raw",
		"params": {"args": []}
	}`, os.Args[0])

	rec := doCLIRequest(t, s, http.MethodPost, "/api/cli/test", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// 声明工具占用保留名 raw：保存时 400。
func TestCLIConnectorRawNameReserved(t *testing.T) {
	s, _ := newCLITestServer(t)

	body := `{"command": "gh", "raw": true,
		"tools": [{"name": "raw", "description": "d", "args": ["x"]}]}`
	rec := doCLIRequest(t, s, http.MethodPut, "/api/cli/connectors/gh", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
