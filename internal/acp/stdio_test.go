package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// 本文件用"测试二进制自己当 agent 子进程"的方式验证真实 stdio 链路。
//
// 为什么不只用管道假 agent：initialize/close/子进程退出/大段 stderr 这些
// 行为只有真进程才暴露得出来。旧版的两处致命伤（Send 只写不读、stderr 只在
// Close 才排空）用管道测是测不出来的。

const helperEnv = "GO_WANT_ACP_HELPER"

// TestHelperACPAgent 不是真测试：它被 StartStdio 拉起来当 agent 用。
func TestHelperACPAgent(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("helper process only")
	}

	// 先灌 256KB stderr。旧实现只在 Close 时才 drain，管道写满（约 64KB）
	// 这里就会永久阻塞，客户端连 initialize 都收不到。
	blob := strings.Repeat("x", 256<<10)
	_, _ = os.Stderr.WriteString(blob)

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64<<10), MaxMessageBytes)

	write := func(v interface{}) {
		data, _ := json.Marshal(v)
		_, _ = out.Write(append(data, '\n'))
		_ = out.Flush()
	}

	for sc.Scan() {
		var env rpcEnvelope
		if err := json.Unmarshal(sc.Bytes(), &env); err != nil {
			continue
		}

		switch env.Method {
		case "initialize":
			write(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      env.ID,
				"result": map[string]interface{}{
					"protocolVersion": ProtocolVersion,
					"agentInfo":       map[string]string{"name": "helper", "version": "0.1"},
				},
			})
		case "session/new":
			write(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      env.ID,
				"result":  map[string]string{"sessionId": "sess-helper"},
			})
		case "session/prompt":
			write(map[string]interface{}{
				"jsonrpc": "2.0",
				"method":  "session/update",
				"params": map[string]interface{}{
					"sessionId": "sess-helper",
					"update": map[string]interface{}{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]string{"type": "text", "text": "pong from helper"},
					},
				},
			})
			write(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      env.ID,
				"result":  map[string]string{"stopReason": StopReasonEndTurn},
			})
		case "session/cancel":
			// 通知，不响应
		}
	}
	os.Exit(0)
}

func TestStdioRoundTrip(t *testing.T) {
	if os.Getenv(helperEnv) == "1" {
		t.Skip("helper process")
	}

	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := Connect(ctx, ClientOptions{
		Name:    "helper",
		Cwd:     dir,
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperACPAgent"},
		Env:     []string{helperEnv + "=1"},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		// 受限沙箱（CI 容器、部分安全策略）会直接拒绝 fork/exec。
		// 这属于环境限制，不是实现问题：跳过而不是假装通过。
		if strings.Contains(err.Error(), "Access is denied") || strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow spawning child processes: %v", err)
		}
		t.Fatalf("connect: %v", err)
	}

	if client.SessionID() != "sess-helper" {
		t.Errorf("session = %q", client.SessionID())
	}
	if client.Info().AgentInfo == nil || client.Info().AgentInfo.Name != "helper" {
		t.Errorf("agentInfo = %+v", client.Info().AgentInfo)
	}

	out, err := client.Prompt(ctx, []ContentBlock{TextBlock("ping")}, nil)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if out.Text != "pong from helper" {
		t.Errorf("text = %q", out.Text)
	}

	start := time.Now()
	if err := client.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if elapsed := time.Since(start); elapsed > defaultCloseGrace {
		t.Errorf("close took %s (agent should exit on stdin EOF, not need a kill)", elapsed)
	}
}
