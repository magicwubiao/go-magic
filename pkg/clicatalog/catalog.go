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
	// Install 是各平台的安装命令（仅展示）。
	Install InstallHints `json:"install"`
	DocsURL string       `json:"docs_url,omitempty"`
	Tags    []string     `json:"tags,omitempty"`
}

// Category 是目录分类（label 交给前端 locale）。
type Category struct {
	ID   string `json:"id"`
	Sort int    `json:"-"`
}

var categories = []Category{
	{ID: "dev", Sort: 1},
	{ID: "data", Sort: 2},
	{ID: "cloud", Sort: 3},
}

// Categories 返回全部分类。
func Categories() []Category { return categories }

var entries = []Entry{
	ghEntry,
	gitEntry,
	dockerEntry,
	kubectlEntry,
	jqEntry,
	awsEntry,
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

// tokenHelp 是各平台通用的 PAT 申请地址（GitHub 专用）。
const ghTokenHelp = "https://github.com/settings/tokens"

var ghEntry = Entry{
	ID:          "gh",
	Name:        "GitHub CLI",
	Description: "List and inspect pull requests and issues of a repository.",
	Category:    "dev",
	DefaultName: "gh",
	Connector: cliconn.Connector{
		Command:     "gh",
		Description: "GitHub CLI (gh)",
		Tools: []cliconn.ToolSpec{
			{
				Name:        "pr_list",
				Description: "List pull requests of a repository.",
				Args:        []string{"pr", "list", "--repo", "{{repo}}", "--limit", "{{limit}}", "--state", "{{state}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "repo", Description: "owner/name", Required: true},
					{Name: "limit", Type: "integer", Description: "max rows", Default: 20},
					{Name: "state", Enum: []string{"open", "closed", "merged", "all"}, Default: "open"},
				},
			},
			{
				Name:        "pr_view",
				Description: "Show one pull request: title, description, checks and diff summary.",
				Args:        []string{"pr", "view", "{{number}}", "--repo", "{{repo}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "number", Type: "integer", Description: "PR number", Required: true},
					{Name: "repo", Description: "owner/name", Required: true},
				},
			},
			{
				Name:        "issue_list",
				Description: "List issues of a repository.",
				Args:        []string{"issue", "list", "--repo", "{{repo}}", "--limit", "{{limit}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "repo", Description: "owner/name", Required: true},
					{Name: "limit", Type: "integer", Default: 20},
				},
			},
		},
	},
	// token 可选：gh 自己会读 keychain / gh auth login 的凭据。
	// 把它设成必填会让"已经登录过 gh"的用户白白多一步，甚至让安装失败。
	Requires: []Requirement{{
		Key:         "GH_TOKEN",
		Label:       "Personal access token (optional if you already ran gh auth login)",
		Secret:      true,
		Required:    false,
		Placeholder: "ghp_...",
		HelpURL:     ghTokenHelp,
	}},
	Install: InstallHints{
		"windows": {"winget install --id GitHub.cli -e"},
		"darwin":  {"brew install gh"},
		"linux":   {"sudo apt install gh", "sudo dnf install gh"},
	},
	DocsURL: "https://cli.github.com/manual/",
	Tags:    []string{"git", "github", "pr"},
}

var gitEntry = Entry{
	ID:          "git",
	Name:        "Git",
	Description: "Inspect a repository: status, recent commits, diffs.",
	Category:    "dev",
	DefaultName: "git",
	Connector: cliconn.Connector{
		Command:     "git",
		Description: "Git version control",
		Tools: []cliconn.ToolSpec{
			{
				Name:        "status",
				Description: "Show short status of the working tree.",
				Args:        []string{"-C", "{{dir}}", "status", "--short", "--branch"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{{
					Name:        "dir",
					Description: "repository path; omit to use the server working directory",
				}},
			},
			{
				Name:        "log",
				Description: "List recent commits, one line each.",
				Args:        []string{"-C", "{{dir}}", "log", "--oneline", "--decorate", "--max-count", "{{limit}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "dir", Description: "repository path; omit to use the server working directory"},
					{Name: "limit", Type: "integer", Default: 20},
				},
			},
			{
				Name:        "diff",
				Description: "Show a diff of the working tree (or staged changes).",
				Args:        []string{"-C", "{{dir}}", "diff", "--stat", "{{?cached}}--cached"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "dir", Description: "repository path; omit to use the server working directory"},
					{Name: "cached", Type: "boolean", Description: "show staged changes instead"},
				},
			},
			{
				Name:        "show",
				Description: "Show one commit with its diff summary.",
				Args:        []string{"-C", "{{dir}}", "show", "--stat", "{{ref}}"},
				ReadOnly:    true,
				Params: []cliconn.ParamSpec{
					{Name: "dir", Description: "repository path; omit to use the server working directory"},
					{Name: "ref", Description: "commit/branch/tag", Default: "HEAD"},
				},
			},
		},
	},
	Install: InstallHints{
		"windows": {"winget install --id Git.Git -e"},
		"darwin":  {"brew install git"},
		"linux":   {"sudo apt install git", "sudo dnf install git"},
	},
	DocsURL: "https://git-scm.com/docs",
	Tags:    []string{"vcs", "commit"},
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
	DocsURL: "https://docs.aws.amazon.com/cli/",
	Tags:    []string{"s3", "ec2"},
}
