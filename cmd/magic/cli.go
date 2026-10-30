package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/cliconnbridge"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// CLI 连接器的命令行入口。
//
// 与 `magic mcp` 对称：MCP 那边有 add/list/health，这里先只做 list——
// 定义本身是 JSON 结构（tools 里带参数模板），用旗标拼一遍既不好写也容易
// 拼错，真正需要批量写入的场景走 Web 的「应用目录」或 config.json。
// list 的价值在于：排查"模型为什么看不到某个 cli_ 工具"时，能看到
// 连接器到底被解析成了哪几个注册名。

// bridgeConfiguredCLITools 把 config 里配置的 CLI 连接器注册进工具表，
// 返回注册成功的工具数。与 web server 走同一条 cliconnbridge.BuildAndSync，
// 避免两个入口的过滤规则（enabled / profiles）出现分叉。
func bridgeConfiguredCLITools(reg *tool.Registry, cfg *config.Config) int {
	if reg == nil || cfg == nil || cfg.Cli == nil || len(cfg.Cli.Connectors) == 0 {
		return 0
	}
	_, count := cliconnbridge.BuildAndSync(cfg.Cli.Connectors, reg, "", &cfg.Policy)
	return count
}

var cliCmd = &cobra.Command{
	Use:   "cli",
	Short: "Manage CLI connectors (external command-line tools as agent tools)",
}

var cliListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured CLI connectors and the tools they expose",
	Run:   runCLIList,
}

var (
	cliAddName     string
	cliAddTool     string
	cliAddDesc     string
	cliAddReadOnly bool
	cliAddTimeout  int
	cliAddDryRun   bool
	cliAddForce    bool

	cliAddCmd = &cobra.Command{
		Use:   "add <command line>",
		Short: "Add a tool by pasting the command line you would run in a terminal",
		Long: `Add a CLI tool from a command line.

The command line is parsed into a connector + tool draft:
  magic cli add "gh pr list --state open --limit 20"

  --state open      -> --state {{state}}   (string parameter)
  --limit 20        -> --limit {{limit}}   (integer parameter)
  --json            -> {{?json}}--json     (boolean switch, dropped when false)
  <SLUG> / {{slug}} -> positional parameter

Adding a second command of the same program merges it into the same connector:
  magic cli add "gh pr view {{number}} --json title,state" --name gh

Nothing is executed at add time: the command becomes a tool the model can call,
and every call goes through the approval chain unless --read-only is set.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error { return runCLIAdd(strings.Join(args, " ")) },
	}

	cliRemoveCmd = &cobra.Command{
		Use:   "remove <connector>",
		Short: "Remove a CLI connector and all of its tools",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return runCLIRemove(args[0]) },
	}
)

func init() {
	rootCmd.AddCommand(cliCmd)
	cliCmd.AddCommand(cliListCmd, cliAddCmd, cliRemoveCmd)
	cliAddCmd.Flags().StringVarP(&cliAddName, "name", "n", "", "connector name (default: derived from the executable)")
	cliAddCmd.Flags().StringVarP(&cliAddTool, "tool", "t", "", "tool name (default: derived from the subcommand words)")
	cliAddCmd.Flags().StringVarP(&cliAddDesc, "desc", "d", "", "tool description shown to the model")
	cliAddCmd.Flags().BoolVar(&cliAddReadOnly, "read-only", false, "declare the command as read-only so it can skip approval")
	cliAddCmd.Flags().IntVar(&cliAddTimeout, "timeout", 0, "per-call timeout in seconds (0 = connector default)")
	cliAddCmd.Flags().BoolVar(&cliAddDryRun, "dry-run", false, "print the resulting JSON without saving")
	cliAddCmd.Flags().BoolVarP(&cliAddForce, "force", "f", false, "replace an existing tool with the same name")
}

// runCLIAdd 是 `magic cli add` 的执行体：解析命令 → 合并进连接器 → 落盘。
//
// 与 Web 的「快速导入」共用同一个 cliconn.ParseCommand，两边产出的草稿形状一致；
// 差异只在这里多了"同名连接器合并"和命令行覆盖项。
func runCLIAdd(commandLine string) error {
	parsed, err := cliconn.ParseCommand(commandLine)
	if err != nil {
		return fmt.Errorf("cannot parse command: %w", err)
	}
	for _, w := range parsed.Warnings {
		fmt.Fprintf(os.Stderr, "note: %s\n", w)
	}

	// 连接器名默认取可执行文件名（去掉路径与 .exe），再按 cliconn 的字符集净化。
	connectorName := strings.TrimSpace(cliAddName)
	if connectorName == "" {
		base := filepath.Base(parsed.Command)
		base = strings.TrimSuffix(base, ".exe")
		connectorName = sanitizeConnectorName(base)
	}
	if err := cliconn.ValidateConnectorName(connectorName); err != nil {
		return fmt.Errorf("invalid connector name %q: %w (use --name)", connectorName, err)
	}

	spec := parsed.Tool
	if cliAddTool != "" {
		spec.Name = strings.TrimSpace(cliAddTool)
	}
	if cliAddDesc != "" {
		spec.Description = cliAddDesc
	}
	spec.ReadOnly = cliAddReadOnly || spec.ReadOnly
	if cliAddTimeout > 0 {
		spec.TimeoutSeconds = cliAddTimeout
	}

	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfig) {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if cfg.Cli == nil {
		cfg.Cli = &cliconn.Config{}
	}
	if cfg.Cli.Connectors == nil {
		cfg.Cli.Connectors = map[string]cliconn.Connector{}
	}

	conn, exists := cfg.Cli.Connectors[connectorName]
	if !exists {
		conn = cliconn.Connector{Command: parsed.Command}
	} else if !sameCommand(conn.Command, parsed.Command) {
		return fmt.Errorf("connector %q already points at %q; pick another name with --name", connectorName, conn.Command)
	}

	replaced := false
	for i, existing := range conn.Tools {
		if existing.Name == spec.Name {
			if !cliAddForce {
				return fmt.Errorf("tool %q already exists in connector %q (use --force to replace, or --tool to pick another name)", spec.Name, connectorName)
			}
			conn.Tools[i] = spec
			replaced = true
		}
	}
	if !replaced {
		conn.Tools = append(conn.Tools, spec)
	}
	if err := conn.Validate(); err != nil {
		return fmt.Errorf("invalid connector: %w", err)
	}

	if cliAddDryRun {
		out, err := json.MarshalIndent(map[string]cliconn.Connector{connectorName: conn}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}

	cfg.Cli.Connectors[connectorName] = conn
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Added %s to connector %s\n", cliconn.ToolName(connectorName, spec.Name), connectorName)
	if spec.ReadOnly {
		fmt.Println("  read-only: calls skip the approval prompt")
	} else {
		fmt.Println("  approval required: every call asks for confirmation")
	}
	if len(spec.Params) > 0 {
		fmt.Println("  parameters:")
		for _, p := range spec.Params {
			required := ""
			if p.Required {
				required = " (required)"
			}
			typ := p.Type
			if typ == "" {
				typ = "string"
			}
			fmt.Printf("    - %s: %s%s\n", p.Name, typ, required)
		}
	}
	fmt.Println("Run 'magic cli list' to confirm, or restart a running session to pick it up.")
	return nil
}

// runCLIRemove 删除整个连接器（连带它下面的所有工具）。
func runCLIRemove(name string) error {
	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfig) {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if cfg.Cli == nil {
		return fmt.Errorf("no CLI connectors configured")
	}
	conn, ok := cfg.Cli.Connectors[name]
	if !ok {
		return fmt.Errorf("connector %q not found", name)
	}
	delete(cfg.Cli.Connectors, name)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Printf("Removed connector %s (%d tool(s))\n", name, len(conn.Tools))
	return nil
}

// sameCommand 比较两个可执行文件引用是否指向同一个程序。
// 允许 basename 与绝对路径混用（用户这次写 gh、上次写 /usr/bin/gh）。
func sameCommand(a, b string) bool {
	if a == b {
		return true
	}
	return filepath.Base(a) == filepath.Base(b)
}

// sanitizeConnectorName 把可执行文件名净化成合法连接器名。
// cliconn 只接受 [A-Za-z0-9][A-Za-z0-9_.-]*，其他字符统一换成 '-'。
func sanitizeConnectorName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "cli"
	}
	return out
}

func runCLIList(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfig) {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	if cfg.Cli == nil || len(cfg.Cli.Connectors) == 0 {
		fmt.Println("No CLI connectors configured.")
		fmt.Println("Add one from the Web UI (Connectors -> CLI -> App catalog) or edit config.json.")
		return
	}

	names := make([]string, 0, len(cfg.Cli.Connectors))
	for name := range cfg.Cli.Connectors {
		names = append(names, name)
	}
	sort.Strings(names)

	// env 只统计条数，绝不打印值：这里可能是 token，
	// 而 list 的输出经常被贴进 issue。
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCOMMAND\tTOOLS\tENABLED\tTIMEOUT\tENV")
	for _, name := range names {
		c := cfg.Cli.Connectors[name]
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%d\n",
			name,
			connectorCommand(c),
			len(c.Tools),
			boolLabel(c.AutoConnect()),
			strconv.Itoa(c.EffectiveTimeout(cliconn.ToolSpec{}))+"s",
			len(c.Env),
		)
	}
	w.Flush()

	// 被准入策略挡住的连接器单独说清楚：它们看起来"配好了但没生效"，
	// 不解释的话排查方向会跑偏到配置语法上。
	if blocked := blockedCLIConnectors(cfg); len(blocked) > 0 {
		fmt.Println()
		fmt.Println("Blocked by policy:")
		for _, line := range blocked {
			fmt.Printf("  %s\n", line)
		}
	}

	fmt.Println()
	fmt.Println("Tools:")
	for _, name := range names {
		c := cfg.Cli.Connectors[name]
		if !c.AutoConnect() {
			fmt.Printf("  %s: disabled\n", name)
			continue
		}
		// 被策略挡住的连接器不会有工具注册，列出来会让人误以为"只是没生效"。
		if err := cfg.Policy.CheckCLI(name, c.Command); err != nil {
			fmt.Printf("  %s: blocked by policy\n", name)
			continue
		}
		for _, spec := range c.Tools {
			timeout := c.EffectiveTimeout(spec)
			fmt.Printf("  %-40s %s (%ds%s)\n",
				cliconn.ToolName(name, spec.Name),
				spec.Description,
				timeout,
				readOnlyLabel(spec.ReadOnly),
			)
		}
	}
}

// blockedCLIConnectors 返回被 policy 拒绝的连接器（排序后的可读描述）。
func blockedCLIConnectors(cfg *config.Config) []string {
	if cfg == nil || cfg.Cli == nil {
		return nil
	}
	var out []string
	for name, c := range cfg.Cli.Connectors {
		if err := cfg.Policy.CheckCLI(name, c.Command); err != nil {
			out = append(out, err.Error())
		}
	}
	sort.Strings(out)
	return out
}

func connectorCommand(c cliconn.Connector) string {
	if len(c.Args) == 0 {
		return c.Command
	}
	return c.Command + " " + strings.Join(c.Args, " ")
}

func boolLabel(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func readOnlyLabel(readOnly bool) string {
	if readOnly {
		return ", read-only"
	}
	return ""
}
