// Package clicatalog 是内置的「CLI 应用目录」—— 一组精选的命令行工具，
// 供 Web UI 的「CLI 应用」卡片一键安装成插件。
//
// 为什么要它：手写连接器要求用户知道可执行文件名、子命令、参数模板，
// 而目录把这些知识固化下来。用户点一个卡片、填一个 token，就能在对话里用。
//
// **收录标准（重要，别再往里加"常用工具"）**：目录只放
// `execute_command` 拿不到的东西——需要代管/隔离凭据的跨服务查询
// （gh、aws、aliyun、tccli、wrangler、vercel、supabase），外加一个高频的
// 本地容器视角（docker）。git、kubectl、helm、jq、yq、rg、ffmpeg、yt-dlp
// 这类"本机只读工具"曾经在目录里，已整体移除：模型用内置 shell 就能跑，
// 收进目录的代价是每装一条往工具列表塞 2-5 个定义，直接吃掉上下文。
// 判断口诀：**凭据要不要留在进程环境里？不要 → 走 shell，不进目录。**
//
// 原则（与 pkg/connectors 保持一致）：
//   - 只收录有稳定 CLI 的官方工具，命令与参数按各自文档写；
//   - **不内置任何凭据**：需要认证的条目用 Requires 描述要填什么，安装时由用户填；
//   - 文案用英文（中文由前端 locale 提供）；
//   - 目录只是模板，安装时展开成 cliconn.Connector，不引入新的运行时概念。
//
// raw 不变量：**每个条目都开启 Connector.Raw**。声明式工具只登记高频
// 只读操作（免审批），CLI 的其余子命令面（写操作、长尾、新版本加的
// 参数）一律走 raw 逃生工具——每次调用都要人工审批。目录条目因此
// 不需要在"覆盖"和"信任"之间二选一。
package clicatalog

import (
	"sort"
	"strings"

	"github.com/magicwubiao/go-magic/internal/cliconn"
)

// Requirement 描述安装时必须由用户填写的一项环境变量。
//
// 只支持环境变量：CLI 的凭据几乎都从 env 或配置文件读；参数位留给命令本身。
type Requirement struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Secret      bool   `json:"secret"`
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
	HelpURL     string `json:"help_url,omitempty"`
}

// InstallHints 按平台给安装命令（key: windows / darwin / linux）。
//
// 只做展示与复制：服务端不会去执行这些命令——包管理器安装会弹权限确认、
// 耗时不可控，失败的表达方式也各不相同，交给用户在自己的终端里跑更可靠。
type InstallHints map[string][]string

// CommandsFor 返回指定平台的安装命令（大小写与别名都兼容）。
func (h InstallHints) CommandsFor(goos string) []string {
	if cmds, ok := h[strings.ToLower(goos)]; ok {
		return cmds
	}
	switch strings.ToLower(goos) {
	case "windows":
		return h["windows"]
	case "darwin":
		return h["darwin"]
	default:
		return h["linux"]
	}
}

// sameOnAllPlatforms 给三个平台同一组命令，省掉重复的三份 map。
func sameOnAllPlatforms(cmds ...string) InstallHints {
	return InstallHints{"windows": cmds, "darwin": cmds, "linux": cmds}
}

// Entry 是目录中的一个 CLI 工具条目。
type Entry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	// DefaultName 是建议的连接器名，也是工具前缀（cli_<name>_<tool>）。
	DefaultName string `json:"default_name"`
	// Connector 是安装时写入配置的模板（Env 为空，由用户填写后注入）。
	Connector cliconn.Connector `json:"connector"`
	// Requires 描述必须由用户填写的环境变量。
	Requires []Requirement `json:"requires,omitempty"`
	// Detect 是版本探测参数（如 ["--version"]），空表示不探测。
	// 只用于让用户确认"装的是哪一份、版本够不够"，服务端不解析版本号。
	Detect []string `json:"detect,omitempty"`
	// Login 是登录/授权命令（仅展示）。
	//
	// 这类命令几乎都是交互式的（等浏览器回调、问 region、要 MFA），
	// 服务端无法代跑，也不该代跑——凭据必须经过用户自己的终端。
	Login InstallHints `json:"login,omitempty"`
	// LoginExec 是**可由服务端代跑**的登录参数（不含可执行文件名）。
	//
	// 只有确认能在无 TTY 下跑通的流程才登记：命令打印 device code / 授权 URL
	// 后阻塞等待，本身不提问（或只等一次回车，由前端转发的 stdin 满足）。
	// 空 = 不代跑，只展示 Login 里的命令。
	//
	// 为什么单独一个字段而不是解析 Login：Login 是给人看的 shell 字符串
	// （可能带 sudo、管道、环境变量前缀），拆成 argv 既不可靠也不安全。
	LoginExec []string `json:"login_exec,omitempty"`
	// Check 是"就绪"复检参数：退出码 0 表示已登录 / 服务可达。
	// 与 Detect 分开，是因为有些工具装了但没配（docker 没有 daemon、
	// kubectl 没有 context、aws 没有凭证），只看 PATH 会误报可用。
	Check []string `json:"check,omitempty"`
	// Install 是各平台的安装命令（仅展示）。
	Install InstallHints `json:"install"`
	DocsURL string       `json:"docs_url,omitempty"`
	Tags    []string     `json:"tags,omitempty"`
}

// LoginRunnable 报告该条目是否支持"点一下由服务端代跑登录"。
func (e Entry) LoginRunnable() bool { return len(e.LoginExec) > 0 }

// Category 是目录分类（label 交给前端 locale）。
type Category struct {
	ID string `json:"id"`
	// Label 是分类的展示文案（英文），物化成插件时随 CategoryLabel 下发。
	Label string `json:"-"`
	Sort  int    `json:"-"`
}

var categories = []Category{
	{ID: "dev", Label: "Development", Sort: 1},
	{ID: "cloud", Label: "Cloud", Sort: 2},
}

// Categories 返回全部分类。
func Categories() []Category { return categories }

var entries = []Entry{
	dockerEntry,
	awsEntry,
	aliyunEntry,
	tccliEntry,
	wranglerEntry,
	vercelEntry,
	supabaseEntry,
	ghEntry,
}

// All 返回按分类与名字排好序的目录条目。
func All() []Entry {
	out := make([]Entry, len(entries))
	copy(out, entries)
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := categorySort(out[i].Category), categorySort(out[j].Category)
		if ci != cj {
			return ci < cj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Get 按 ID 查找条目。
func Get(id string) (Entry, bool) {
	for _, e := range entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

func categorySort(id string) int {
	for _, c := range categories {
		if c.ID == id {
			return c.Sort
		}
	}
	return 99
}

var dockerEntry = Entry{
	ID:          "docker",
	Name:        "Docker",
	Description: "List containers and images, read container logs.",
	Category:    "dev",
	DefaultName: "docker",
	Connector: cliconn.Connector{
		Command:     "docker",
		Description: "Docker engine CLI",
		// compose / network / volume / stats 等长尾走 raw（恒审批）。
		Raw: true,
		Tools: []cliconn.ToolSpec{
			{
				Name:        "ps",
				Description: "List running containers.",
				Args:        []string{"ps"},
				ReadOnly:    true,
			},
			{
				Name:        "images",
				Description: "List local images.",
				Args:        []string{"images"},
				ReadOnly:    true,
			},
			{
				Name:        "inspect",
				Description: "Show detailed information about a container or image as JSON (env, mounts, ports, network).",
				Args:        []string{"inspect", "{{target}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "target", Description: "container or image name / id", Required: true},
				},
			},
			{
				Name:        "logs",
				Description: "Show recent logs of a container.",
				Args:        []string{"logs", "--tail", "{{tail}}", "{{container}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "container", Description: "container name or id", Required: true},
					{Name: "tail", Type: "integer", Default: 200},
				},
			},
		},
	},
	Install: InstallHints{
		"windows": {"winget install --id Docker.DockerDesktop -e"},
		"darwin":  {"brew install --cask docker"},
		"linux":   {"sudo apt install docker.io"},
	},
	Detect: []string{"--version"},
	// docker 装了但 daemon 没起是很常见的状态，用 info 探一次比看 PATH 准。
	Check:   []string{"info"},
	DocsURL: "https://docs.docker.com/reference/cli/docker/",
	Tags:    []string{"container"},
}

var awsEntry = Entry{
	ID:          "aws",
	Name:        "AWS CLI",
	Description: "Inspect S3 buckets, EC2 instances and the caller identity.",
	Category:    "cloud",
	DefaultName: "awscli",
	Connector: cliconn.Connector{
		Command:     "aws",
		Description: "AWS CLI (uses the credentials already configured with aws configure)",
		// AWS API 面巨大：s3 cp / logs tail / cloudwatch get-metric-data 等
		// 全走 raw（恒审批）。
		Raw: true,
		// describe-instances 的 JSON 可能很大。
		MaxOutputBytes: 64 << 10,
		Tools: []cliconn.ToolSpec{
			{
				Name:        "whoami",
				Description: "Show the caller identity and account id.",
				Args:        []string{"sts", "get-caller-identity", "--output", "json"},
				ReadOnly:    true,
			},
			{
				Name:        "s3_ls",
				Description: "List S3 buckets, or objects under a prefix.",
				Args:        []string{"s3", "ls", "{{path}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{{
					Name:        "path",
					Description: "s3://bucket/prefix; omit to list buckets",
				}},
			},
			{
				Name:        "ec2_instances",
				Description: "Describe EC2 instances (id, state, type).",
				Args:        []string{"ec2", "describe-instances", "--max-items", "{{limit}}", "--output", "json"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "limit", Type: "integer", Default: 20},
				},
			},
		},
	},
	// 凭据走 aws configure / SSO，不在环境变量里存 —— 目录不要求填任何 token，
	// 但装完后第一次调用如果没登录，错误会由 aws 自己返回，模型能看懂。
	Install: InstallHints{
		"windows": {"winget install --id Amazon.AWSCLI -e"},
		"darwin":  {"brew install awscli"},
		"linux":   {"sudo snap install aws-cli --classic"},
	},
	Detect: []string{"--version"},
	Login:  sameOnAllPlatforms("aws configure"),
	// 这一条会走网络：它同时验证"凭证存在"和"凭证可用"，失败原因
	// （没配 / 过期 / 无网络）都归到同一条提示里。
	Check:   []string{"sts", "get-caller-identity"},
	DocsURL: "https://docs.aws.amazon.com/cli/",
	Tags:    []string{"s3", "ec2"},
}

// ===== 云平台 CLI：本机已有凭据（aws configure / aliyun configure / tccli configure…），
// 连接器只负责隔离调用，不代存 token =====

var aliyunEntry = Entry{
	ID:          "aliyun",
	Name:        "Alibaba Cloud CLI",
	Description: "Inspect ECS instances, OSS buckets and the caller identity on Alibaba Cloud.",
	Category:    "cloud",
	DefaultName: "aliyun",
	Connector: cliconn.Connector{
		Command:     "aliyun",
		Description: "Alibaba Cloud CLI (uses credentials from aliyun configure)",
		Raw:         true,
		Tools: []cliconn.ToolSpec{
			{
				Name:        "whoami",
				Description: "Show the caller identity of the configured credentials.",
				Args:        []string{"sts", "GetCallerIdentity"},
				ReadOnly:    true,
			},
			{
				Name:        "ecs_instances",
				Description: "List ECS instances in a region (id, status, type).",
				Args:        []string{"ecs", "DescribeInstances", "--RegionId", "{{region}}", "--PageSize", "{{limit}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "region", Description: "region id, e.g. cn-hangzhou", Required: true},
					{Name: "limit", Type: "integer", Default: 20},
				},
			},
			{
				Name:        "oss_ls",
				Description: "List OSS buckets, or objects under an oss:// path.",
				Args:        []string{"oss", "ls", "{{path}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "path", Description: "omit to list buckets; oss://bucket/prefix to list objects"},
				},
			},
		},
	},
	// 凭据走 aliyun configure / 环境变量由用户自己管理，目录不代存 token。
	Install: InstallHints{
		"windows": {"winget install --id AlibabaCloud.AliyunCLI -e"},
		"darwin":  {"brew install aliyun-cli"},
		"linux":   {"sudo snap install aliyun-cli --classic"},
	},
	Detect:  []string{"version"},
	Login:   sameOnAllPlatforms("aliyun configure"),
	Check:   []string{"configure", "list"},
	DocsURL: "https://help.aliyun.com/zh/cli/",
	Tags:    []string{"alibaba", "ecs", "oss"},
}

var tccliEntry = Entry{
	ID:          "tccli",
	Name:        "Tencent Cloud CLI",
	Description: "Inspect CVM instances and zones on Tencent Cloud.",
	Category:    "cloud",
	DefaultName: "tccli",
	Connector: cliconn.Connector{
		Command:     "tccli",
		Description: "Tencent Cloud CLI (uses credentials from tccli configure)",
		Raw:         true,
		Tools: []cliconn.ToolSpec{
			{
				Name:        "cvm_instances",
				Description: "List CVM instances in a region.",
				Args:        []string{"cvm", "describe-instances", "--region", "{{region}}", "--limit", "{{limit}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "region", Description: "region, e.g. ap-guangzhou", Required: true},
					{Name: "limit", Type: "integer", Default: 20},
				},
			},
			{
				Name:        "cvm_zones",
				Description: "List availability zones of a region.",
				Args:        []string{"cvm", "describe-zones", "--region", "{{region}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "region", Description: "region, e.g. ap-guangzhou", Required: true},
				},
			},
		},
	},
	Install: InstallHints{
		"windows": {"pip install tccli"},
		"darwin":  {"pip3 install tccli"},
		"linux":   {"pip install tccli"},
	},
	Detect:  []string{"--version"},
	Login:   sameOnAllPlatforms("tccli configure"),
	Check:   []string{"configure", "list"},
	DocsURL: "https://cloud.tencent.com/document/product/440",
	Tags:    []string{"tencent", "cvm"},
}

var wranglerEntry = Entry{
	ID:          "wrangler",
	Name:        "Cloudflare Wrangler",
	Description: "Inspect Cloudflare Workers deployments and KV namespaces.",
	Category:    "cloud",
	DefaultName: "wrangler",
	Connector: cliconn.Connector{
		Command:     "wrangler",
		Description: "Cloudflare Workers CLI (uses credentials from wrangler login)",
		Raw:         true, // deploy / kv put 等写操作走 raw（恒审批）
		Tools: []cliconn.ToolSpec{
			{
				Name:        "whoami",
				Description: "Show the logged-in Cloudflare account.",
				Args:        []string{"whoami"},
				ReadOnly:    true,
			},
			{
				Name:        "deployments",
				Description: "List deployments of the Worker in the current directory (or --name).",
				Args:        []string{"deployments", "list", "{{?name}}--name", "{{name}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "name", Description: "worker name; omit to use ./wrangler.toml"},
				},
			},
			{
				Name:        "kv_namespaces",
				Description: "List KV namespaces of the account.",
				Args:        []string{"kv", "namespace", "list"},
				ReadOnly:    true,
			},
		},
	},
	Install: InstallHints{
		"windows": {"npm install -g wrangler"},
		"darwin":  {"npm install -g wrangler", "brew install cloudflare-wrangler"},
		"linux":   {"npm install -g wrangler"},
	},
	Detect: []string{"--version"},
	Login:  sameOnAllPlatforms("wrangler login"),
	// wrangler login 会打印授权 URL 后阻塞等回调，不提问，可代跑。
	LoginExec: []string{"login"},
	Check:     []string{"whoami"},
	DocsURL:   "https://developers.cloudflare.com/workers/wrangler/commands/",
	Tags:      []string{"cloudflare", "workers", "kv"},
}

var vercelEntry = Entry{
	ID:          "vercel",
	Name:        "Vercel CLI",
	Description: "List projects and recent deployments on Vercel.",
	Category:    "cloud",
	DefaultName: "vercel",
	Connector: cliconn.Connector{
		Command:     "vercel",
		Description: "Vercel CLI (uses credentials from vercel login)",
		Raw:         true, // deploy / env 等写操作走 raw（恒审批）
		Tools: []cliconn.ToolSpec{
			{
				Name:        "whoami",
				Description: "Show the logged-in account.",
				Args:        []string{"whoami"},
				ReadOnly:    true,
			},
			{
				Name:        "projects",
				Description: "List projects of the current team.",
				Args:        []string{"projects", "ls"},
				ReadOnly:    true,
			},
			{
				Name:        "deployments",
				Description: "List recent deployments, optionally filtered by project.",
				Args:        []string{"ls", "{{project}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "project", Description: "project name; omit to list all"},
				},
			},
		},
	},
	Install: InstallHints{
		"windows": {"npm install -g vercel"},
		"darwin":  {"npm install -g vercel", "brew install vercel-cli"},
		"linux":   {"npm install -g vercel"},
	},
	Detect: []string{"--version"},
	Login:  sameOnAllPlatforms("vercel login"),
	// vercel login 走浏览器授权；可能问一次"用哪个账号登录"，前端可以转发输入。
	LoginExec: []string{"login"},
	Check:     []string{"whoami"},
	DocsURL:   "https://vercel.com/docs/cli",
	Tags:      []string{"deploy", "hosting"},
}

var supabaseEntry = Entry{
	ID:          "supabase",
	Name:        "Supabase CLI",
	Description: "Inspect Supabase projects, branches and database migrations.",
	Category:    "cloud",
	DefaultName: "supabase",
	Connector: cliconn.Connector{
		Command:     "supabase",
		Description: "Supabase CLI (uses the access token from supabase login or SUPABASE_ACCESS_TOKEN)",
		Raw:         true, // db push / link 等写操作走 raw（恒审批）
		Tools: []cliconn.ToolSpec{
			{
				Name:        "projects",
				Description: "List projects of the logged-in account.",
				Args:        []string{"projects", "list", "--output", "json"},
				ReadOnly:    true,
			},
			{
				Name:        "orgs",
				Description: "List organizations of the logged-in account.",
				Args:        []string{"orgs", "list"},
				ReadOnly:    true,
			},
			{
				Name:        "db_diff",
				Description: "Diff the local schema changes against a linked database.",
				Args:        []string{"db", "diff", "{{?linked}}--linked"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "linked", Type: "boolean", Description: "diff against the linked remote database"},
				},
			},
		},
	},
	Requires: []Requirement{{
		Key:         "SUPABASE_ACCESS_TOKEN",
		Label:       "Personal access token (optional if supabase login already ran)",
		Secret:      true,
		Required:    false,
		Placeholder: "sbp_...",
		HelpURL:     "https://supabase.com/dashboard/account/tokens",
	}},
	Install: InstallHints{
		"windows": {"winget install --id Supabase.CLI -e", "npm install -g supabase"},
		"darwin":  {"brew install supabase/tap/supabase"},
		"linux":   {"npm install -g supabase"},
	},
	Detect:  []string{"--version"},
	Login:   sameOnAllPlatforms("supabase login"),
	Check:   []string{"projects", "list"},
	DocsURL: "https://supabase.com/docs/guides/local-development/cli/getting-started",
	Tags:    []string{"postgres", "bass"},
}

// ghEntry：GitHub 官方 CLI。
//
// 声明式工具只放高频只读子命令（pr/issue/repo/run 的 list 与 view），
// 同时打开 Raw：gh 的子命令面太大（单 gh api 就能覆盖整个 REST/GraphQL API），
// 手写 ToolSpec 永远追不上，raw 逃生工具（恒审批）才是它长尾的正确入口。
//
// 认证三选一，目录不代存 token：
//  1. 机器上已经 gh auth login 过（凭据在 gh 自己的配置里，最常见）；
//  2. 填 GH_TOKEN（走 Requires，引导用户用 ${VAR} 引用）；
//  3. 在自己的终端里跑 Login 展示的命令。
//
// 不设 LoginExec：gh auth login 的交互（包括最后的 "Press Enter to open
// the browser"）走 survey，检测到 stdin 不是 TTY 会直接报错——管道 stdin
// 的代跑链路喂不进它。--with-token 倒是吃 stdin，但它要等 EOF，而前端
// 的输入转发只写不关，会一直挂起。等哪天 stdin 生命周期可控了再补。
var ghEntry = Entry{
	ID:          "gh",
	Name:        "GitHub CLI",
	Description: "List pull requests, issues and workflow runs; inspect repositories via the GitHub API.",
	Category:    "dev",
	DefaultName: "gh",
	Connector: cliconn.Connector{
		Command:     "gh",
		Description: "GitHub CLI (uses the credentials from gh auth login, or GH_TOKEN)",
		Raw:         true,
		Tools: []cliconn.ToolSpec{
			{
				Name:        "pr_list",
				Description: "List pull requests of a repository (number, title, author, state).",
				Args:        []string{"pr", "list", "--limit", "{{limit}}", "--state", "{{state}}", "{{?repo}}--repo", "{{repo}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "repo", Description: "OWNER/REPO; omit when running inside a repository checkout"},
					{Name: "state", Default: "open", Description: "open / closed / merged / all"},
					{Name: "limit", Type: "integer", Default: 20},
				},
			},
			{
				Name:        "issue_list",
				Description: "List issues of a repository (number, title, author, state).",
				Args:        []string{"issue", "list", "--limit", "{{limit}}", "--state", "{{state}}", "{{?repo}}--repo", "{{repo}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "repo", Description: "OWNER/REPO; omit when running inside a repository checkout"},
					{Name: "state", Default: "open", Description: "open / closed / all"},
					{Name: "limit", Type: "integer", Default: 20},
				},
			},
			{
				Name:        "run_list",
				Description: "List recent workflow (Actions) runs of a repository.",
				Args:        []string{"run", "list", "--limit", "{{limit}}", "{{?repo}}--repo", "{{repo}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "repo", Description: "OWNER/REPO; omit when running inside a repository checkout"},
					{Name: "limit", Type: "integer", Default: 10},
				},
			},
			{
				Name:        "repo_view",
				Description: "Show the description, default branch and README of a repository.",
				Args:        []string{"repo", "view", "{{repo}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "repo", Description: "OWNER/REPO (or full URL)", Required: true},
				},
			},
		},
	},
	Requires: []Requirement{{
		Key:         "GH_TOKEN",
		Label:       "GitHub token (optional if gh auth login already ran)",
		Secret:      true,
		Required:    false,
		Placeholder: "${GH_TOKEN}",
		HelpURL:     "https://github.com/settings/tokens",
	}},
	Install: InstallHints{
		"windows": {"winget install --id GitHub.cli -e"},
		"darwin":  {"brew install gh"},
		"linux":   {"sudo apt install gh", "sudo dnf install gh"},
	},
	Detect: []string{"--version"},
	Login:  sameOnAllPlatforms("gh auth login"),
	// 退出码非 0 表示这台机器既没有 gh 凭据也没有可用 token，与"没装"区分开。
	Check:   []string{"auth", "status"},
	DocsURL: "https://cli.github.com/manual/",
	Tags:    []string{"github", "pr", "issue", "actions"},
}
