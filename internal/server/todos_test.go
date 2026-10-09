package server

import (
	"encoding/json"
	"testing"
)

// TestBuildTodoUpdateArgs 覆盖两处线上缺陷的根因：
//   - P1：任何不带 title 的局部更新都会 500（"title cannot be empty for update"）
//   - P2：带 title 的更新会静默清空 description / priority
//
// 修法 = 只把请求体里**真正出现过**的字段写进 args（工具是"键存在即生效"语义）。
func TestBuildTodoUpdateArgs(t *testing.T) {
	ptr := func(s string) *string { return &s }

	decode := func(t *testing.T, body string) todoUpdateRequest {
		t.Helper()
		var req todoUpdateRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		return req
	}

	t.Run("only status must not carry a title key", func(t *testing.T) {
		req := decode(t, `{"status":"completed"}`)
		args := buildTodoUpdateArgs("todo_1", "sess", req)
		if _, has := args["title"]; has {
			t.Fatalf("title must be absent (else the tool aborts with 'title cannot be empty'): %#v", args)
		}
		if _, has := args["description"]; has {
			t.Fatalf("description must be absent: %#v", args)
		}
		if _, has := args["priority"]; has {
			t.Fatalf("priority must be absent: %#v", args)
		}
		if args["status"] != "completed" {
			t.Fatalf("status lost: %#v", args)
		}
		if args["id"] != "todo_1" || args["session_id"] != "sess" || args["action"] != "update" {
			t.Fatalf("identity fields wrong: %#v", args)
		}
	})

	t.Run("only title must not wipe description and priority", func(t *testing.T) {
		req := decode(t, `{"title":"renamed"}`)
		args := buildTodoUpdateArgs("todo_2", "sess", req)
		if args["title"] != "renamed" {
			t.Fatalf("title lost: %#v", args)
		}
		if _, has := args["description"]; has {
			t.Fatalf("description must stay untouched: %#v", args)
		}
		if _, has := args["priority"]; has {
			t.Fatalf("priority must stay untouched: %#v", args)
		}
	})

	t.Run("explicit empty string still means clear", func(t *testing.T) {
		req := todoUpdateRequest{Description: ptr(""), Priority: ptr("")}
		args := buildTodoUpdateArgs("todo_3", "sess", req)
		if v, has := args["description"]; !has || v != "" {
			t.Fatalf("explicit empty description must be forwarded: %#v", args)
		}
		if v, has := args["priority"]; !has || v != "" {
			t.Fatalf("explicit empty priority must be forwarded: %#v", args)
		}
	})

	t.Run("empty status is dropped, not treated as a value", func(t *testing.T) {
		req := decode(t, `{"status":""}`)
		args := buildTodoUpdateArgs("todo_4", "sess", req)
		if _, has := args["status"]; has {
			t.Fatalf("empty status should not produce a key: %#v", args)
		}
	})
}
