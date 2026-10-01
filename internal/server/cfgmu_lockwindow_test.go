package server

// 守卫：cfgMu 锁窗口内的自我死锁。
//
// **为什么需要一条 AST 守卫而不是靠运行时测试**：sync.RWMutex 不可重入，
// 在 lockCfgForWrite / lockCfgWriteGuard 的窗口内再调 cfgSnapshot()/cfg*()
// 会**当场把整个进程挂死**（不是报错、不是竞态，是永久卡住直到重启），
// 而且只有走到那条分支才会触发 —— 靠测试碰运气覆盖不到所有分支。
//
// 2026-10-01 事故：模型供应商页点"保存"整个后端卡死。前端 configStore.saveProvider
// 第一步 GET /api/providers/{name} 探存在性，而 handleProvidersSubRoutes 入口就
// lockCfgWriteGuard()（s.mu + cfgMu），GET/POST/DELETE 三个分支里却仍调
// s.cfgSnapshot() ⇒ 自我死锁。PUT 分支早已改成直接读 s.cfg，另外三个分支漏了。
//
// 判定口径：
//   - 锁窗口内**允许**：直接读 s.cfg（lockCfgForWrite 已 COW 出写点私有副本）、
//     调 persistConfig（它已改为直接读 s.cfg）。
//   - 锁窗口内**禁止**：cfgSnapshot / cfgWorkingDir / cfgCurrentModel /
//     cfgProfile / cfgProvider / setCfg / refreshConvertConfig（都要再拿 cfgMu），
//     以及 clearAgents / syncConfigFromDisk（要拿 agentsMu / s.mu，必须先 Release）。
//   - 解锁只有两种形态：非 defer 的 `Release()` / `unlockCfgForWrite()`；
//     `defer g.Release()` 在函数返回时才生效，不能在那一行就清锁。
//   - 分析是**分支感知的 may-hold**（任一路径仍持锁 ⇒ 后续视为持锁），宁多报不漏报。
//     确有正当例外时在该行写 `//cfgmu-audit:ignore` 并附理由。
//
// 历史：这个检查先以 .workbuddy/tmp 下的手工脚本形式存在，连续两版假阴性
// （分别漏掉 defer Release 与 PUT 分支 Release 之后的真死锁），故固化为测试。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var cfgLockForbidden = map[string]string{
	"cfgSnapshot":          "再拿 cfgMu（不可重入 → 自我死锁）",
	"cfgWorkingDir":        "再拿 cfgMu（不可重入 → 自我死锁）",
	"cfgCurrentModel":      "再拿 cfgMu（不可重入 → 自我死锁）",
	"cfgProfile":           "再拿 cfgMu（不可重入 → 自我死锁）",
	"cfgProvider":          "再拿 cfgMu（不可重入 → 自我死锁）",
	"setCfg":               "再拿 cfgMu（不可重入 → 自我死锁）",
	"refreshConvertConfig": "内部读配置快照（再拿 cfgMu）",
	"clearAgents":          "要拿 agentsMu，cfgMu 是叶子锁 ⇒ 必须先 Release",
	"syncConfigFromDisk":   "要拿 s.mu / agentsMu ⇒ 必须先 Release",
}

var cfgLockers = map[string]bool{"lockCfgForWrite": true, "lockCfgWriteGuard": true}
var cfgUnlockers = map[string]bool{"unlockCfgForWrite": true, "Release": true}

type cfgLockViolation struct {
	fn       string
	line     int
	call     string
	lockLine int
	why      string
}

func TestNoCfgHelperInsideCfgLockWindow(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var violations []cfgLockViolation
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(src), "\n")
		af, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range af.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			sc := &cfgLockScanner{fset: fset, file: path, lines: lines, fn: fn.Name.Name}
			sc.scanStmts(fn.Body.List, false, 0)
			violations = append(violations, sc.hits...)
		}
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].fn != violations[j].fn {
			return violations[i].fn < violations[j].fn
		}
		return violations[i].line < violations[j].line
	})
	for _, v := range violations {
		t.Errorf("%s:%d: %s(): %s() 在 cfgMu 锁窗口内（锁起于 line %d）— %s",
			v.fn, v.line, v.fn, v.call, v.lockLine, v.why)
	}
	if len(violations) > 0 {
		t.Log("修法：锁窗口内直接读 s.cfg（写点已由 lockCfgForWrite COW 出私有副本）；" +
			"需要拿别的锁（agentsMu/s.mu）或读配置快照的调用，必须先 g.Release() 再调。")
	}
}

type cfgLockScanner struct {
	fset  *token.FileSet
	file  string
	lines []string
	fn    string
	hits  []cfgLockViolation
	seen  map[string]bool
}

func (sc *cfgLockScanner) ignored(line int) bool {
	if line <= 0 || line > len(sc.lines) {
		return false
	}
	return strings.Contains(sc.lines[line-1], "cfgmu-audit:ignore")
}

func (sc *cfgLockScanner) report(call string, line, lockLine int, why string) {
	if sc.ignored(line) {
		return
	}
	if sc.seen == nil {
		sc.seen = map[string]bool{}
	}
	key := fmt.Sprintf("%d:%s", line, call)
	if sc.seen[key] {
		return
	}
	sc.seen[key] = true
	sc.hits = append(sc.hits, cfgLockViolation{fn: sc.fn, line: line, call: call, lockLine: lockLine, why: why})
}

// scanStmts 顺序扫描语句列表，返回"扫描结束后是否仍可能持 cfgMu"。
func (sc *cfgLockScanner) scanStmts(stmts []ast.Stmt, held bool, lockLine int) bool {
	for _, st := range stmts {
		held, lockLine = sc.scanStmt(st, held, lockLine)
	}
	return held
}

func (sc *cfgLockScanner) scanStmt(st ast.Stmt, held bool, lockLine int) (bool, int) {
	switch x := st.(type) {
	case *ast.DeferStmt:
		// defer 的 Release 在函数返回时才生效：只报其中的违禁调用，不改状态
		sc.inspectExpr(x.Call, held, lockLine)
		return held, lockLine

	case *ast.IfStmt:
		h := held
		if x.Init != nil {
			h, lockLine = sc.scanStmt(x.Init, h, lockLine)
		}
		sc.inspectExpr(x.Cond, h, lockLine)
		thenHeld := sc.scanStmts(x.Body.List, h, lockLine)
		elseHeld := h
		if x.Else != nil {
			elseHeld, _ = sc.scanStmt(x.Else, h, lockLine)
		}
		return thenHeld || elseHeld, lockLine

	case *ast.BlockStmt:
		return sc.scanStmts(x.List, held, lockLine), lockLine

	case *ast.ForStmt:
		h := held
		if x.Init != nil {
			h, lockLine = sc.scanStmt(x.Init, h, lockLine)
		}
		sc.inspectExpr(x.Cond, h, lockLine)
		bodyHeld := sc.scanStmts(x.Body.List, h, lockLine)
		if x.Post != nil {
			bodyHeld, _ = sc.scanStmt(x.Post, bodyHeld, lockLine)
		}
		return held || bodyHeld, lockLine

	case *ast.RangeStmt:
		return held || sc.scanStmts(x.Body.List, held, lockLine), lockLine

	case *ast.SwitchStmt:
		h := held
		if x.Init != nil {
			h, lockLine = sc.scanStmt(x.Init, h, lockLine)
		}
		sc.inspectExpr(x.Tag, h, lockLine)
		return sc.mergeCases(x.Body.List, h, lockLine), lockLine

	case *ast.TypeSwitchStmt:
		h := held
		if x.Init != nil {
			h, lockLine = sc.scanStmt(x.Init, h, lockLine)
		}
		if x.Assign != nil {
			h, lockLine = sc.scanStmt(x.Assign, h, lockLine)
		}
		return sc.mergeCases(x.Body.List, h, lockLine), lockLine

	case *ast.SelectStmt:
		merged := false
		for _, c := range x.Body.List {
			if cc, ok := c.(*ast.CommClause); ok && sc.scanStmts(cc.Body, held, lockLine) {
				merged = true
			}
		}
		return held || merged, lockLine

	case *ast.LabeledStmt:
		return sc.scanStmt(x.Stmt, held, lockLine)
	}

	return sc.scanSimple(st, held, lockLine)
}

// mergeCases 合并 switch 各分支的持锁状态。注意：没有 default 时"原状态保持"
// 只意味着进入 switch 时的那份状态（h）—— 若进入时并未持锁，绝不能无条件
// 判为持锁（旧版脚本正是因此把后续整段语句误判成锁窗口）。
func (sc *cfgLockScanner) mergeCases(cases []ast.Stmt, h bool, lockLine int) bool {
	merged, hasDefault := false, false
	for _, c := range cases {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			continue
		}
		if cc.List == nil {
			hasDefault = true
		}
		if sc.scanStmts(cc.Body, h, lockLine) {
			merged = true
		}
	}
	if !hasDefault && h {
		merged = true
	}
	return merged
}

func (sc *cfgLockScanner) scanSimple(st ast.Stmt, held bool, lockLine int) (bool, int) {
	ast.Inspect(st, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		line := sc.fset.Position(call.Pos()).Line
		if held {
			if why := cfgLockForbidden[name]; why != "" {
				sc.report(name, line, lockLine, why)
			}
		}
		if cfgLockers[name] && !held {
			held = true
			lockLine = line
		}
		if cfgUnlockers[name] {
			held = false
		}
		return true
	})
	return held, lockLine
}

// inspectExpr 只扫表达式里的调用（条件 / defer 实参），不改变锁状态。
func (sc *cfgLockScanner) inspectExpr(e ast.Expr, held bool, lockLine int) {
	if e == nil || !held {
		return
	}
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if why := cfgLockForbidden[sel.Sel.Name]; why != "" {
				sc.report(sel.Sel.Name, sc.fset.Position(call.Pos()).Line, lockLine, why)
			}
		}
		return true
	})
}
