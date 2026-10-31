// Package clicatalog 是内置的「CLI 应用目录」—— 一组精选的常用命令行工具，
// 供 Web UI 的「从目录添加」一键安装。
//
// 为什么要它：手写连接器要求用户知道可执行文件名、子命令、参数模板，
// 而目录把这些知识固化下来。用户点一个卡片、填一个 token，就能在对话里用。
//
// 原则（与 pkg/connectors 保持一致）：
//   - 只收录有稳定 CLI 的官方工具，命令与参数按各自文档写；
//   - **不内置任何凭据**：需要认证的条目用 Requires 描述要填什么，安装时由用户填；
//   - 文案用英文（中文由前端 locale 提供）；
//   - 目录只是模板，安装时展开成 cliconn.Connector，不引入新的运行时概念。
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
	{ID: "data", Label: "Data", Sort: 2},
	{ID: "cloud", Label: "Cloud", Sort: 3},
	{ID: "media", Label: "Media", Sort: 4},
}

// Categories 返回全部分类。
func Categories() []Category { return categories }

var entries = []Entry{
	dockerEntry,
	kubectlEntry,
	helmEntry,
	jqEntry,
	yqEntry,
	rgEntry,
	awsEntry,
	aliyunEntry,
	tccliEntry,
	wranglerEntry,
	vercelEntry,
	ytdlpEntry,
	ffmpegEntry,
	supabaseEntry,
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

var kubectlEntry = Entry{
	ID:          "kubectl",
	Name:        "kubectl",
	Description: "Query pods and read logs from a Kubernetes cluster.",
	Category:    "dev",
	DefaultName: "kubectl",
	Connector: cliconn.Connector{
		Command:     "kubectl",
		Description: "Kubernetes CLI",
		Tools: []cliconn.ToolSpec{
			{
				Name:        "get_pods",
				Description: "List pods in a namespace (or all namespaces).",
				Args:        []string{"get", "pods", "-n", "{{namespace}}", "{{?all_namespaces}}--all-namespaces"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "namespace", Default: "default"},
					{Name: "all_namespaces", Type: "boolean", Description: "query every namespace"},
				},
			},
			{
				Name:        "describe",
				Description: "Describe one resource: events, status, container states.",
				Args:        []string{"describe", "{{kind}}", "{{name}}", "-n", "{{namespace}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "kind", Description: "pod / deployment / service", Required: true},
					{Name: "name", Required: true},
					{Name: "namespace", Default: "default"},
				},
			},
			{
				Name:        "logs",
				Description: "Read recent logs of a pod.",
				Args:        []string{"logs", "{{pod}}", "-n", "{{namespace}}", "--tail", "{{tail}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "pod", Required: true},
					{Name: "namespace", Default: "default"},
					{Name: "tail", Type: "integer", Default: 200},
				},
			},
		},
	},
	Install: InstallHints{
		"windows": {"winget install --id Kubernetes.kubectl -e"},
		"darwin":  {"brew install kubectl"},
		"linux":   {"sudo snap install kubectl --classic"},
	},
	Detect: []string{"version", "--client"},
	// 有 context 才说明这台机器真的指向了某个集群（本地判断，不连网）。
	Check:   []string{"config", "current-context"},
	DocsURL: "https://kubernetes.io/docs/reference/kubectl/",
	Tags:    []string{"k8s", "cluster"},
}

var jqEntry = Entry{
	ID:          "jq",
	Name:        "jq",
	Description: "Filter and reshape JSON without writing code.",
	Category:    "data",
	DefaultName: "jq",
	Connector: cliconn.Connector{
		Command:     "jq",
		Description: "jq JSON processor",
		Tools: []cliconn.ToolSpec{
			{
				Name:        "query",
				Description: "Apply a jq filter to a JSON document passed on stdin.",
				Args:        []string{"{{filter}}"},
				StdinParam:  "input",
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "filter", Description: "jq filter, e.g. .items[] | .name", Required: true},
					{Name: "input", Description: "JSON document to process", Required: true},
				},
			},
			{
				Name:        "keys",
				Description: "List the top-level keys of a JSON object.",
				Args:        []string{"keys"},
				StdinParam:  "input",
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "input", Description: "JSON document", Required: true},
				},
			},
		},
	},
	Install: InstallHints{
		"windows": {"winget install --id jqlang.jq -e"},
		"darwin":  {"brew install jq"},
		"linux":   {"sudo apt install jq", "sudo dnf install jq"},
	},
	Detect:  []string{"--version"},
	DocsURL: "https://jqlang.github.io/jq/manual/",
	Tags:    []string{"json"},
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

// ===== 以下为扩充批次：海内外常用工具 =====

var helmEntry = Entry{
	ID:          "helm",
	Name:        "Helm",
	Description: "List releases and read their values and history on a Kubernetes cluster.",
	Category:    "dev",
	DefaultName: "helm",
	Connector: cliconn.Connector{
		Command:     "helm",
		Description: "Helm package manager for Kubernetes",
		Tools: []cliconn.ToolSpec{
			{
				Name:        "list",
				Description: "List releases in a namespace (or all namespaces).",
				Args:        []string{"list", "-n", "{{namespace}}", "{{?all}}--all-namespaces"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "namespace", Default: "default"},
					{Name: "all", Type: "boolean", Description: "list every namespace"},
				},
			},
			{
				Name:        "values",
				Description: "Show the (user-supplied) values of a release.",
				Args:        []string{"get", "values", "{{release}}", "-n", "{{namespace}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "release", Required: true},
					{Name: "namespace", Default: "default"},
				},
			},
			{
				Name:        "history",
				Description: "Show revision history of a release.",
				Args:        []string{"history", "{{release}}", "-n", "{{namespace}}", "--max", "{{limit}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "release", Required: true},
					{Name: "namespace", Default: "default"},
					{Name: "limit", Type: "integer", Default: 10},
				},
			},
		},
	},
	Install: InstallHints{
		"windows": {"winget install --id Helm.Helm -e"},
		"darwin":  {"brew install helm"},
		"linux":   {"sudo snap install helm --classic"},
	},
	Detect:  []string{"version", "--short"},
	Check:   []string{"list", "-A"},
	DocsURL: "https://helm.sh/docs/",
	Tags:    []string{"k8s", "charts"},
}

var yqEntry = Entry{
	ID:          "yq",
	Name:        "yq",
	Description: "Filter and reshape YAML (and JSON) documents without writing code.",
	Category:    "data",
	DefaultName: "yq",
	Connector: cliconn.Connector{
		Command:     "yq",
		Description: "yq YAML/JSON processor (mikefarah)",
		Tools: []cliconn.ToolSpec{
			{
				Name:        "query",
				Description: "Apply a yq expression to a YAML/JSON document passed on stdin.",
				Args:        []string{"{{expression}}"},
				StdinParam:  "input",
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "expression", Description: "yq expression, e.g. .items[].name", Required: true},
					{Name: "input", Description: "YAML or JSON document", Required: true},
				},
			},
			{
				Name:        "to_json",
				Description: "Convert a YAML document on stdin to JSON.",
				Args:        []string{"-o=json"},
				StdinParam:  "input",
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "input", Description: "YAML document", Required: true},
				},
			},
		},
	},
	Install: InstallHints{
		"windows": {"winget install --id MikeFarah.yq -e"},
		"darwin":  {"brew install yq"},
		"linux":   {"sudo snap install yq", "sudo apt install yq"},
	},
	Detect:  []string{"--version"},
	DocsURL: "https://mikefarah.gitbook.io/yq/",
	Tags:    []string{"yaml", "json"},
}

var rgEntry = Entry{
	ID:          "rg",
	Name:        "ripgrep",
	Description: "Fast text search across files, respecting .gitignore.",
	Category:    "data",
	DefaultName: "rg",
	Connector: cliconn.Connector{
		Command:     "rg",
		Description: "ripgrep recursive search",
		Tools: []cliconn.ToolSpec{
			{
				Name:        "search",
				Description: "Search a pattern under a directory; returns matching lines with numbers.",
				Args:        []string{"-n", "--max-count", "{{max}}", "-e", "{{pattern}}", "{{path}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "pattern", Description: "regex to search", Required: true},
					{Name: "path", Description: "file or directory; omit for current directory"},
					{Name: "max", Type: "integer", Description: "max matches per file", Default: 20},
				},
			},
			{
				Name:        "count",
				Description: "Count matches per file under a directory.",
				Args:        []string{"--count-matches", "-e", "{{pattern}}", "{{path}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "pattern", Description: "regex to search", Required: true},
					{Name: "path", Description: "file or directory; omit for current directory"},
				},
			},
		},
	},
	Install: InstallHints{
		"windows": {"winget install --id BurntSushi.ripgrep.MSVC -e"},
		"darwin":  {"brew install ripgrep"},
		"linux":   {"sudo apt install ripgrep", "sudo dnf install ripgrep"},
	},
	Detect:  []string{"--version"},
	DocsURL: "https://github.com/BurntSushi/ripgrep",
	Tags:    []string{"search", "grep"},
}

var aliyunEntry = Entry{
	ID:          "aliyun",
	Name:        "Alibaba Cloud CLI",
	Description: "Inspect ECS instances, OSS buckets and the caller identity on Alibaba Cloud.",
	Category:    "cloud",
	DefaultName: "aliyun",
	Connector: cliconn.Connector{
		Command:     "aliyun",
		Description: "Alibaba Cloud CLI (uses credentials from aliyun configure)",
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

var ytdlpEntry = Entry{
	ID:          "yt-dlp",
	Name:        "yt-dlp",
	Description: "Probe a video URL for formats and metadata, or download media.",
	Category:    "media",
	DefaultName: "yt-dlp",
	Connector: cliconn.Connector{
		Command:     "yt-dlp",
		Description: "yt-dlp video downloader",
		Tools: []cliconn.ToolSpec{
			{
				Name:        "info",
				Description: "Print title, duration and available formats of a video as JSON.",
				Args:        []string{"-J", "--no-playlist", "{{url}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "url", Description: "video page URL", Required: true},
				},
			},
			{
				Name:           "download",
				Description:    "Download a video to a directory. Requires approval because it writes files.",
				Args:           []string{"--no-playlist", "-o", "{{output}}", "{{url}}"},
				TimeoutSeconds: 300,
				Params: []cliconn.ParamSpec{
					{Name: "url", Description: "video page URL", Required: true},
					{Name: "output", Description: "output path template, e.g. ~/Downloads/%(title)s.%(ext)s", Default: "%(title)s.%(ext)s"},
				},
			},
		},
	},
	Install: InstallHints{
		"windows": {"winget install --id yt-dlp.yt-dlp -e"},
		"darwin":  {"brew install yt-dlp"},
		"linux":   {"sudo apt install yt-dlp", "sudo pip install yt-dlp"},
	},
	Detect:  []string{"--version"},
	DocsURL: "https://github.com/yt-dlp/yt-dlp",
	Tags:    []string{"video", "download"},
}

var ffmpegEntry = Entry{
	ID:          "ffmpeg",
	Name:        "FFmpeg",
	Description: "Convert media files and extract audio tracks locally.",
	Category:    "media",
	DefaultName: "ffmpeg",
	Connector: cliconn.Connector{
		Command:     "ffmpeg",
		Description: "FFmpeg media converter",
		Tools: []cliconn.ToolSpec{
			{
				Name:           "convert",
				Description:    "Transcode a media file to another container/codec. Requires approval because it writes files.",
				Args:           []string{"-y", "-i", "{{input}}", "{{output}}"},
				TimeoutSeconds: 300,
				Params: []cliconn.ParamSpec{
					{Name: "input", Description: "source media file", Required: true},
					{Name: "output", Description: "target file, e.g. out.mp4", Required: true},
				},
			},
			{
				Name:           "extract_audio",
				Description:    "Extract the audio track of a media file into an MP3. Requires approval because it writes files.",
				Args:           []string{"-y", "-i", "{{input}}", "-vn", "{{output}}"},
				TimeoutSeconds: 300,
				Params: []cliconn.ParamSpec{
					{Name: "input", Description: "source media file", Required: true},
					{Name: "output", Description: "target audio file, e.g. out.mp3", Required: true},
				},
			},
		},
	},
	Install: InstallHints{
		"windows": {"winget install --id Gyan.FFmpeg -e"},
		"darwin":  {"brew install ffmpeg"},
		"linux":   {"sudo apt install ffmpeg", "sudo dnf install ffmpeg"},
	},
	Detect:  []string{"-version"},
	DocsURL: "https://ffmpeg.org/ffmpeg.html",
	Tags:    []string{"audio", "video", "convert"},
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
