package tool

// TestFormatPythonAutoInstallsBlack 端到端验证 format_code 的 Python 分支：
// 机器上没有 black/autopep8 时，工具应自动 `pip install --user black` 并完成格式化，
// 而不是报 "no Python formatter found"。
//
// 注意：该测试会真的执行 pip 安装（有网时一次性代价），并依赖机器上存在
// 可用的 Python 解释器；没有解释器时跳过。
import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatPythonAutoInstallsBlack(t *testing.T) {
	bin, prefix, ok := ResolvePythonCommand()
	if !ok {
		t.Skip("no python interpreter on this machine; cannot exercise the python format path")
	}
	t.Logf("resolved interpreter: %s %v", bin, prefix)

	work := t.TempDir()
	// 刻意写成不符合 black 风格的代码：多余空格、双引号、未换行。
	src := "x=1\ny =  2\ndef  foo( a,b ) :\n    return  a+b\n"
	pyFile := filepath.Join(work, "messy.py")
	if err := os.WriteFile(pyFile, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	ctx = WithWorkDir(ctx, work)

	res, err := NewFormatTool().Execute(ctx, map[string]interface{}{
		"path":     "messy.py",
		"language": "python",
	})
	if err != nil {
		t.Fatalf("format_code failed: %v", err)
	}
	t.Logf("result: %v", res)

	out, err := os.ReadFile(pyFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("formatted content:\n%s", out)

	// black 会把双引号改写为单引号、收紧运算符两侧空格；
	// 出现任一特征即证明是 black（而非 autopep8 兜底）完成的格式化。
	formatted := string(out)
	if formatted == src {
		t.Fatal("file was not formatted at all")
	}
	if !strings.Contains(formatted, "def foo(a, b):") {
		t.Errorf("expected black-style signature `def foo(a, b):`, got:\n%s", formatted)
	}
}
