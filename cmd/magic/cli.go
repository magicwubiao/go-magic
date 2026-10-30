package main

import (
	"errors"
	"fmt"
	"os"
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
	_, count := cliconnbridge.BuildAndSync(cfg.Cli.Connectors, reg, "")
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

func init() {
	rootCmd.AddCommand(cliCmd)
	cliCmd.AddCommand(cliListCmd)
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

	fmt.Println()
	fmt.Println("Tools:")
	for _, name := range names {
		c := cfg.Cli.Connectors[name]
		if !c.AutoConnect() {
			fmt.Printf("  %s: disabled\n", name)
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
