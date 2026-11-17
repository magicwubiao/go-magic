package server

import (
	"runtime"
	"strings"
	"testing"
)

// TestRuntimeSpecByID 固定清单必须能按 id 找到，且未知 id 一定被拒绝——
// 这是"请求体只传 id、不传命令"这条安全边界的前提。
func TestRuntimeSpecByID(t *testing.T) {
	for _, id := range []string{"node", "python", "gitbash"} {
		spec, ok := runtimeSpecByID(id)
		if !ok {
			t.Fatalf("runtime %q not found in built-in specs", id)
		}
		if spec.ID != id {
			t.Fatalf("spec.ID = %q, want %q", spec.ID, id)
		}
		if spec.Binary == "" {
			t.Fatalf("runtime %q has empty binary", id)
		}
	}
	if _, ok := runtimeSpecByID("../../evil"); ok {
		t.Fatal("unknown runtime id must be rejected")
	}
	if _, ok := runtimeSpecByID(""); ok {
		t.Fatal("empty runtime id must be rejected")
	}
}

// TestRuntimeInstallCommands 平台命令存在、不含 sudo、winget 已带无人值守开关。
func TestRuntimeInstallCommands(t *testing.T) {
	for _, spec := range runtimeSpecs {
		cmds := runtimeInstallCommands(spec)
		if len(cmds) == 0 {
			t.Fatalf("runtime %q has no install command for %s", spec.ID, runtime.GOOS)
		}
		for _, c := range cmds {
			if strings.Contains(c, "sudo") {
				t.Fatalf("runtime %q command contains sudo: %s", spec.ID, c)
			}
			if runtime.GOOS == "windows" && strings.HasPrefix(c, "winget ") &&
				!strings.Contains(c, "--accept-package-agreements") {
				t.Fatalf("runtime %q winget command missing unattended flags: %s", spec.ID, c)
			}
		}
	}
}

// TestProbeRuntimeShape 探测结果的基本形态：未知 id 不该出现在列表里。
func TestProbeRuntimeShape(t *testing.T) {
	spec, ok := runtimeSpecByID("gitbash")
	if !ok {
		t.Fatal("gitbash spec missing")
	}
	res := probeRuntime(spec)
	if res.ID != "gitbash" || res.Name == "" {
		t.Fatalf("unexpected probe result: %+v", res)
	}
	// Found 时必须有版本；未 Found 时不得带路径。
	if res.Found && res.Version == "" {
		t.Fatalf("found runtime %q without version", res.ID)
	}
	if !res.Found && res.Path != "" {
		t.Fatalf("missing runtime %q reported a path: %s", res.ID, res.Path)
	}
}
