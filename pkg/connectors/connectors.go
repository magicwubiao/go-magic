// Package connectors 是内置的「连接器目录」—— 一组经过挑选的 MCP server
// 模板，供 Web UI 的"从目录添加"使用。
//
// 为什么要它：现在的添加流程要求用户自己知道包名、命令参数、环境变量名，
// 一个字段填错就是一个连不上的连接器。目录把这些知识固化下来，用户只需要
// 选一个条目、填自己的 token。
//
// 原则：
//   - 只收录**官方或社区公认**的 server，命令/参数按各自仓库 README 写；
//   - 不内置任何凭据，需要认证的条目用 Requires 描述要填什么；
//   - 描述文案用英文（与 pkg/catalog 一致，中文交给前端 locale）；
//   - 目录只是模板，安装时展开成 mcp.ServerConfig，不引入新的运行时概念。
package connectors

import (
	"sort"
	"strings"
)

// Requirement 描述安装时必须由用户填写的一项配置。
//
// Where 只有四种取值：env（stdio 子进程的环境变量）、header（远程连接的
// HTTP 头）、arg（stdio 命令行参数，Args 里的 {{KEY}} 占位符会被替换）与
// url（只出现在 Connector.URL 的 {{KEY}} 里，如局域网设备的 base URL）。
// 这四条正好对应 ServerConfig 的四个凭据入口。
type Requirement struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Where string `json:"where"`
	// Header 仅 Where=header 时使用：写入哪个 HTTP 头（空 = 与 Key 同名）。
	// 绝大多数服务要的是 "Authorization: Bearer <token>"，头名与凭据名并不
	// 相同，所以两者分开描述。
	Header string `json:"header,omitempty"`
	// Value 是位置模板：Where=header 时是头值（可含 {{KEY}}，默认 ${CRED:<Key>}）。
	// Where=arg 由 Args 里的 {{KEY}} 决定，本字段不用。
	Value       string `json:"value,omitempty"`
	Secret      bool   `json:"secret"`
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
	HelpURL     string `json:"help_url,omitempty"`
}

const (
	WhereEnv    = "env"
	WhereHeader = "header"
	WhereArg    = "arg"
	// WhereURL 表示这一项只出现在 Connector.URL 的 {{KEY}} 模板里，不需要落
	// 到 env/headers。保留 Requirement 是为了带上 Label、Placeholder 与
	// HelpURL——用户填的是"你的实例地址"，不是一串没有上下文的值。
	WhereURL = "url"
)

// OAuthRequirement 声明该远程连接器支持 OAuth 2.1 授权。
//
// 有值的条目在物化成插件时会写进 mcp.json 的 "oauth" 字段：用户点一下
// "登录"、在浏览器完成授权即可，不需要自己去后台建 API key。字段全部可空
// （留空 = 自动发现授权服务器 + 动态注册客户端 + PKCE）。
type OAuthRequirement struct {
	Scopes       []string `json:"scopes,omitempty"`
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`
	Resource     string   `json:"resource,omitempty"`
	RedirectPort int      `json:"redirect_port,omitempty"`
}

// Connector 是目录中的一个条目。
type Connector struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Transport   string   `json:"transport"` // stdio / http / sse
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args,omitempty"`
	// Env 是条目的**固定**环境变量（IMAP/SMTP 主机与端口这类用户不该操心的值）。
	// 值里可以写 {{KEY}} 占位，物化时换成凭据引用；Requires 里同名的项优先，
	// 也就是"固定值"永远盖不住"用户填的值"这条方向反过来不成立。
	Env      map[string]string `json:"env,omitempty"`
	URL      string            `json:"url,omitempty"`
	Requires []Requirement     `json:"requires,omitempty"`
	DocsURL  string            `json:"docs_url,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	// OAuth 非空表示该条目支持（或要求）OAuth 授权，安装后无需填写凭据。
	OAuth *OAuthRequirement `json:"oauth,omitempty"`
	// DefaultName 是建议的服务器名（用户在表单里可改）。
	DefaultName string `json:"default_name"`
}

// Category 是目录的分类。
type Category struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Categories 返回固定顺序的分类表。顺序按使用频率排，不是字母序——
// 大多数人来这里是为了开发工具或搜索，放在前面更省一次滚动。
func Categories() []Category {
	return []Category{
		{ID: "office", Label: "Office & Collaboration"},
		{ID: "dev", Label: "Development"},
		{ID: "search", Label: "Search & Web"},
		{ID: "data", Label: "Data & Databases"},
		{ID: "productivity", Label: "Productivity"},
		{ID: "design", Label: "Design & Creative"},
		{ID: "automation", Label: "Browser & Automation"},
		{ID: "home", Label: "Home & Devices"},
		{ID: "local", Label: "Local & Files"},
	}
}

// catalog 是内置条目表。
//
// 命令以 npx 拉起为主（免安装、跨平台），这类条目对 Node 有依赖；需要
// Python 的条目用 uvx。参数里的路径/仓库地址由用户在表单里补。
var catalog = []Connector{
	// --- Office & Collaboration（海内外办公协同） ---
	// 全部条目按各家官方文档核实（2026-10）：飞书/钉钉/语雀为官方 MCP，
	// Asana 为官方 remote，Microsoft 365 / Atlassian / Airtable 为社区事实标准。
	{
		// 邮箱类条目共用一个实现（imap-smtp-email-mcp）：它按 IMAP/SMTP 标准协议
		// 收信发信，同一套环境变量既能连 Gmail 也能连 QQ/163/126，所以两个条目
		// 只在主机、端口和"去哪儿拿密码"上不同。相比各家 OAuth 方案，应用专用
		// 密码不需要建 Google Cloud 项目，是当前成本最低的可跑通路径。
		ID:          "gmail",
		Name:        "Gmail",
		Description: "Read, search and send Gmail through IMAP/SMTP with an app password (Google account needs 2-Step Verification; no Cloud project).",
		Category:    "office",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "imap-smtp-email-mcp"},
		Env: map[string]string{
			"IMAP_HOST": "imap.gmail.com",
			"IMAP_PORT": "993",
			"SMTP_HOST": "smtp.gmail.com",
			"SMTP_PORT": "465",
		},
		DefaultName: "gmail",
		Requires: []Requirement{
			{
				Key:         "EMAIL_USER",
				Label:       "Gmail address",
				Where:       WhereEnv,
				Required:    true,
				Placeholder: "you@gmail.com",
				HelpURL:     "https://mail.google.com",
			},
			{
				Key:         "EMAIL_PASS",
				Label:       "App password (16 characters)",
				Where:       WhereEnv,
				Secret:      true,
				Required:    true,
				Placeholder: "abcd efgh ijkl mnop",
				HelpURL:     "https://myaccount.google.com/apppasswords",
			},
		},
		DocsURL: "https://www.npmjs.com/package/imap-smtp-email-mcp",
		Tags:    []string{"gmail", "email", "imap", "smtp", "google", "mailbox"},
	},
	{
		ID:          "qqmail",
		Name:        "QQ Mail",
		Description: "Read, search and send QQ Mail through IMAP/SMTP with a 16-character authorization code.",
		Category:    "office",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "imap-smtp-email-mcp"},
		Env: map[string]string{
			"IMAP_HOST": "imap.qq.com",
			"IMAP_PORT": "993",
			"SMTP_HOST": "smtp.qq.com",
			"SMTP_PORT": "465",
		},
		DefaultName: "qqmail",
		Requires: []Requirement{
			{
				Key:         "EMAIL_USER",
				Label:       "QQ Mail address",
				Where:       WhereEnv,
				Required:    true,
				Placeholder: "123456789@qq.com",
				HelpURL:     "https://mail.qq.com",
			},
			{
				Key:         "EMAIL_PASS",
				Label:       "Authorization code (not your QQ password)",
				Where:       WhereEnv,
				Secret:      true,
				Required:    true,
				Placeholder: "16-character code",
				HelpURL:     "https://help.mail.qq.com/detail/0/1087",
			},
		},
		DocsURL: "https://www.npmjs.com/package/imap-smtp-email-mcp",
		Tags:    []string{"qq", "qqmail", "email", "imap", "smtp", "mailbox"},
	},
	{
		ID:          "lark",
		Name:        "Feishu / Lark",
		Description: "Official Feishu/Lark OpenAPI: messages, docs, wiki, calendar, tasks and Bitable.",
		Category:    "office",
		Transport:   "stdio",
		Command:     "npx",
		// App ID/Secret 以命令行参数传入（官方 README 用法），展开时由
		// arg 类 Requirement 占位替换。
		Args:        []string{"-y", "@larksuiteoapi/lark-mcp", "mcp", "-a", "{{LARK_APP_ID}}", "-s", "{{LARK_APP_SECRET}}"},
		DefaultName: "lark",
		Requires: []Requirement{
			{Key: "LARK_APP_ID", Label: "App ID", Where: WhereArg, Required: true, HelpURL: "https://open.feishu.cn/app"},
			{Key: "LARK_APP_SECRET", Label: "App Secret", Where: WhereArg, Secret: true, Required: true, HelpURL: "https://open.feishu.cn/app"},
		},
		DocsURL: "https://github.com/larksuite/lark-openapi-mcp",
		Tags:    []string{"feishu", "lark", "docs", "calendar", "chat", "bitable"},
	},
	{
		ID:          "dingtalk",
		Name:        "DingTalk",
		Description: "Official DingTalk OpenAPI: contacts, calendar, todos, group messages and work notices.",
		Category:    "office",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "dingtalk-mcp@latest"},
		DefaultName: "dingtalk",
		Requires: []Requirement{
			{Key: "DINGTALK_Client_ID", Label: "AppKey (Client ID)", Where: WhereEnv, Required: true, HelpURL: "https://open.dingtalk.com"},
			{Key: "DINGTALK_Client_Secret", Label: "AppSecret (Client Secret)", Where: WhereEnv, Secret: true, Required: true, HelpURL: "https://open.dingtalk.com"},
		},
		DocsURL: "https://open.dingtalk.com/document/ai-dev/second-level-node-1",
		Tags:    []string{"dingtalk", "calendar", "todo", "chat"},
	},
	{
		ID:          "yuque",
		Name:        "Yuque",
		Description: "Official Yuque knowledge base: search, read and write docs, books and notes.",
		Category:    "office",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "yuque-mcp"},
		DefaultName: "yuque",
		Requires: []Requirement{{
			Key:      "YUQUE_PERSONAL_TOKEN",
			Label:    "Personal access token",
			Where:    WhereEnv,
			Secret:   true,
			Required: true,
			HelpURL:  "https://www.yuque.com/settings/tokens",
		}},
		DocsURL: "https://github.com/yuque/yuque-mcp-server",
		Tags:    []string{"yuque", "docs", "knowledge-base"},
	},
	{
		ID:          "ms365",
		Name:        "Microsoft 365",
		Description: "Outlook mail, calendar, OneDrive, Excel and Teams via Microsoft Graph (device-code login on first use).",
		Category:    "office",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@softeria/ms-365-mcp-server"},
		DefaultName: "ms365",
		DocsURL:     "https://github.com/softeria/ms-365-mcp-server",
		Tags:        []string{"outlook", "onedrive", "excel", "calendar", "teams"},
	},
	{
		ID:          "asana",
		Name:        "Asana",
		Description: "Official Asana Work Graph: tasks, projects, goals and portfolios (browser OAuth on first use).",
		Category:    "office",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "mcp-remote", "https://mcp.asana.com/sse"},
		DefaultName: "asana",
		DocsURL:     "https://developers.asana.com/docs/using-asanas-mcp-server",
		Tags:        []string{"tasks", "project-management"},
	},
	{
		ID:          "confluence",
		Name:        "Confluence",
		Description: "Search and read Confluence spaces and pages (Atlassian Cloud).",
		Category:    "office",
		Transport:   "stdio",
		Command:     "uvx",
		Args:        []string{"mcp-atlassian", "--confluence-url={{CONFLUENCE_URL}}", "--confluence-username={{CONFLUENCE_USERNAME}}", "--confluence-api-token={{CONFLUENCE_API_TOKEN}}"},
		DefaultName: "confluence",
		Requires: []Requirement{
			{Key: "CONFLUENCE_URL", Label: "Site URL", Where: WhereArg, Required: true, Placeholder: "https://your.atlassian.net/wiki"},
			{Key: "CONFLUENCE_USERNAME", Label: "Account email", Where: WhereArg, Required: true},
			{Key: "CONFLUENCE_API_TOKEN", Label: "API token", Where: WhereArg, Secret: true, Required: true, HelpURL: "https://id.atlassian.com/manage-profile/security/api-tokens"},
		},
		DocsURL: "https://github.com/sooperset/mcp-atlassian",
		Tags:    []string{"confluence", "atlassian", "docs", "wiki"},
	},
	{
		ID:          "jira",
		Name:        "Jira",
		Description: "Search and update Jira issues, comments and transitions (Atlassian Cloud).",
		Category:    "office",
		Transport:   "stdio",
		Command:     "uvx",
		Args:        []string{"mcp-atlassian", "--jira-url={{JIRA_URL}}", "--jira-username={{JIRA_USERNAME}}", "--jira-api-token={{JIRA_API_TOKEN}}"},
		DefaultName: "jira",
		Requires: []Requirement{
			{Key: "JIRA_URL", Label: "Site URL", Where: WhereArg, Required: true, Placeholder: "https://your.atlassian.net"},
			{Key: "JIRA_USERNAME", Label: "Account email", Where: WhereArg, Required: true},
			{Key: "JIRA_API_TOKEN", Label: "API token", Where: WhereArg, Secret: true, Required: true, HelpURL: "https://id.atlassian.com/manage-profile/security/api-tokens"},
		},
		DocsURL: "https://github.com/sooperset/mcp-atlassian",
		Tags:    []string{"jira", "atlassian", "issues"},
	},
	{
		ID:          "airtable",
		Name:        "Airtable",
		Description: "Schema-aware read and write for Airtable bases, tables and records.",
		Category:    "office",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "airtable-mcp-server"},
		DefaultName: "airtable",
		Requires: []Requirement{{
			Key:         "AIRTABLE_API_KEY",
			Label:       "Personal access token",
			Where:       WhereEnv,
			Secret:      true,
			Required:    true,
			Placeholder: "pat…",
			HelpURL:     "https://airtable.com/create/tokens/new",
		}},
		DocsURL: "https://github.com/domdomegg/airtable-mcp-server",
		Tags:    []string{"airtable", "database", "no-code"},
	},

	// --- Development ---
	{
		ID:          "github",
		Name:        "GitHub",
		Description: "Repositories, issues, pull requests and code search on GitHub.",
		Category:    "dev",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-github"},
		DefaultName: "github",
		Requires: []Requirement{{
			Key:         "GITHUB_PERSONAL_ACCESS_TOKEN",
			Label:       "Personal access token",
			Where:       WhereEnv,
			Secret:      true,
			Required:    true,
			Placeholder: "ghp_…",
			HelpURL:     "https://github.com/settings/tokens",
		}},
		DocsURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/github",
		Tags:    []string{"git", "issues", "pull-requests", "code-search"},
	},
	{
		ID:          "gitlab",
		Name:        "GitLab",
		Description: "Projects, merge requests and pipelines on GitLab.",
		Category:    "dev",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-gitlab"},
		DefaultName: "gitlab",
		Requires: []Requirement{
			{Key: "GITLAB_PERSONAL_ACCESS_TOKEN", Label: "Personal access token", Where: WhereEnv, Secret: true, Required: true},
			{Key: "GITLAB_API_URL", Label: "API URL (self-hosted only)", Where: WhereEnv, Placeholder: "https://gitlab.com/api/v4"},
		},
		DocsURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/gitlab",
		Tags:    []string{"git", "merge-requests", "ci"},
	},
	{
		ID:          "sentry",
		Name:        "Sentry",
		Description: "Errors, issues and releases from Sentry projects.",
		Category:    "dev",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@sentry/mcp-server"},
		DefaultName: "sentry",
		Requires: []Requirement{{
			Key:      "SENTRY_ACCESS_TOKEN",
			Label:    "Access token",
			Where:    WhereEnv,
			Secret:   true,
			Required: true,
			HelpURL:  "https://sentry.io/settings/account/api/auth-tokens/",
		}},
		DocsURL: "https://docs.sentry.io/product/sentry-mcp/",
		Tags:    []string{"errors", "monitoring"},
	},
	{
		ID:          "context7",
		Name:        "Context7",
		Description: "Up-to-date library documentation and code examples.",
		Category:    "dev",
		Transport:   "http",
		URL:         "https://mcp.context7.com/mcp",
		DefaultName: "context7",
		DocsURL:     "https://github.com/upstash/context7",
		Tags:        []string{"docs", "libraries"},
	},
	{
		ID:          "deepwiki",
		Name:        "DeepWiki",
		Description: "Ask questions about any public GitHub repository.",
		Category:    "dev",
		Transport:   "http",
		URL:         "https://mcp.deepwiki.com/mcp",
		DefaultName: "deepwiki",
		DocsURL:     "https://docs.devin.ai/work-with-devin/deepwiki-mcp",
		Tags:        []string{"docs", "repositories"},
	},

	// --- Search & Web ---
	{
		ID:          "brave-search",
		Name:        "Brave Search",
		Description: "Web and local search through the Brave Search API.",
		Category:    "search",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-brave-search"},
		DefaultName: "brave-search",
		Requires: []Requirement{{
			Key:      "BRAVE_API_KEY",
			Label:    "Brave Search API key",
			Where:    WhereEnv,
			Secret:   true,
			Required: true,
			HelpURL:  "https://brave.com/search/api/",
		}},
		DocsURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/brave-search",
		Tags:    []string{"search", "web"},
	},
	{
		ID:          "fetch",
		Name:        "Fetch",
		Description: "Fetch a URL and convert HTML to markdown for the model.",
		Category:    "search",
		Transport:   "stdio",
		Command:     "uvx",
		Args:        []string{"mcp-server-fetch"},
		DefaultName: "fetch",
		DocsURL:     "https://github.com/modelcontextprotocol/servers/tree/main/src/fetch",
		Tags:        []string{"http", "markdown", "web"},
	},
	{
		ID:          "tavily",
		Name:        "Tavily",
		Description: "Search and extract content tuned for LLM agents.",
		Category:    "search",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "tavily-mcp"},
		DefaultName: "tavily",
		Requires: []Requirement{{
			Key:      "TAVILY_API_KEY",
			Label:    "Tavily API key",
			Where:    WhereEnv,
			Secret:   true,
			Required: true,
			HelpURL:  "https://app.tavily.com/",
		}},
		DocsURL: "https://github.com/tavily-ai/tavily-mcp",
		Tags:    []string{"search", "extract"},
	},

	// --- Data & Databases ---
	{
		ID:          "postgres",
		Name:        "PostgreSQL",
		Description: "Read-only SQL queries against a PostgreSQL database.",
		Category:    "data",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-postgres", "postgresql://localhost/mydb"},
		DefaultName: "postgres",
		DocsURL:     "https://github.com/modelcontextprotocol/servers/tree/main/src/postgres",
		Tags:        []string{"sql", "database"},
	},
	{
		ID:          "sqlite",
		Name:        "SQLite",
		Description: "Query and manage a local SQLite database file.",
		Category:    "data",
		Transport:   "stdio",
		Command:     "uvx",
		Args:        []string{"mcp-server-sqlite", "--db-path", "/path/to/database.db"},
		DefaultName: "sqlite",
		DocsURL:     "https://github.com/modelcontextprotocol/servers/tree/main/src/sqlite",
		Tags:        []string{"sql", "database", "local"},
	},

	// --- Productivity ---
	{
		ID:          "notion",
		Name:        "Notion",
		Description: "Search, read and create pages in a Notion workspace.",
		Category:    "productivity",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@notionhq/notion-mcp-server"},
		DefaultName: "notion",
		Requires: []Requirement{{
			Key:      "NOTION_TOKEN",
			Label:    "Integration token",
			Where:    WhereEnv,
			Secret:   true,
			Required: true,
			HelpURL:  "https://www.notion.so/my-integrations",
		}},
		DocsURL: "https://github.com/makenotion/notion-mcp-server",
		Tags:    []string{"notes", "wiki", "docs"},
	},
	{
		ID:          "slack",
		Name:        "Slack",
		Description: "Read channels and post messages in a Slack workspace.",
		Category:    "productivity",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-slack"},
		DefaultName: "slack",
		Requires: []Requirement{
			{Key: "SLACK_BOT_TOKEN", Label: "Bot token", Where: WhereEnv, Secret: true, Required: true, Placeholder: "xoxb-…"},
			{Key: "SLACK_TEAM_ID", Label: "Team ID", Where: WhereEnv, Required: true},
		},
		DocsURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/slack",
		Tags:    []string{"chat", "messaging"},
	},
	// Linear 官方 MCP 端点（2025 起只有 streamable-http 的 /mcp；老的
	// https://mcp.linear.app/sse 已下线，任何请求都回 404）。用 mcp-remote
	// 桥接 /sse 的旧写法因此永远等不到 401、也就永远走不到 OAuth：用户看到
	// 的只是 "Non-200 status code (404)" 的无限重连。
	// 官方支持 DCR（registration_endpoint /register）、PKCE S256 与 refresh_token，
	// 所以直接用远程条目 + 原生 OAuth：点一下弹浏览器授权，不需要 node。
	{
		ID:          "linear",
		Name:        "Linear",
		Description: "Issues, projects and cycles in Linear.",
		Category:    "productivity",
		Transport:   "http",
		URL:         "https://mcp.linear.app/mcp",
		OAuth:       &OAuthRequirement{Scopes: []string{"read", "write"}},
		DefaultName: "linear",
		DocsURL:     "https://linear.app/docs/mcp",
		Tags:        []string{"issues", "project-management"},
	},

	// --- Design & Creative ---
	// 端点与授权方式均按官方现状实测（2026-11）：
	//   - Miro / Framer / Spline 的远程端点都返回 401 +
	//     resource_metadata，且 /register（RFC 7591 动态注册）对公共客户端
	//     实测 201 → 走 go-magic 原生 OAuth，用户点一下授权即可，无需自建应用。
	//   - Canva 曾按同样方式内置，已于 2026-11 移除：它的发现/注册/授权 URL
	//     都能走通，但 www.canva.com 对中国大陆 IP 一律 302 到 canva.cn
	//     （首页、/login、/api/oauth/authorize 三条都跳，与 UA、Accept-Language
	//     无关），而 canva.cn 是独立运营的中国站、账号不与国际站互通，授权页
	//     因此根本打不开，表现为"点了授权直接跳 canva.cn 首页"。只有浏览器走
	//     海外出口那一次才能完成授权，不适合作为开箱即用的内置条目；需要的人
	//     可以按"自定义 MCP"手填 https://mcp.canva.com/mcp。
	//   - Figma 的远程端点 https://mcp.figma.com/mcp 虽然在线，但它的 DCR 要
	//     Figma 白名单审批（POST /v1/oauth/mcp/register 实测 403，官方文档也写明
	//     "想接入新客户端请申请"），第三方客户端无法自助注册。所以 Figma 给两条
	//     不依赖 DCR 的路：官方桌面版 MCP（本地端口，免 token）与社区事实标准
	//     figma-developer-mcp（个人访问令牌）。
	//
	// 只保留"纯远程"条目：内置目录要能用在服务器端部署上（go-magic 经常跑在
	// 远端），而下面两类依赖"同一台机器上装着那个客户端"的条目一旦内置，在
	// 服务端就是必然失败——用户看到的只是"连不上"。它们已移到示例插件
	// （跟着示例走，本地装才用，一个软件一个示例）：
	//   - Figma 桌面版 MCP（127.0.0.1:3845，需 Figma 客户端开着 Dev Mode）
	//     → examples/plugins/09-mcp-figma-desktop
	//   - 即时设计画布桥接（本地进程 WebSocket 连客户端里的 MCP Bridge 插件）
	//     → examples/plugins/10-mcp-jsdesign
	{
		ID:          "figma-api",
		Name:        "Figma (API token)",
		Description: "Read any Figma file or frame by its URL with a personal access token — works without the Figma desktop app.",
		Category:    "design",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "figma-developer-mcp", "--stdio"},
		DefaultName: "figma-api",
		Requires: []Requirement{{
			Key:      "FIGMA_API_KEY",
			Label:    "Personal access token",
			Where:    WhereEnv,
			Secret:   true,
			Required: true,
			HelpURL:  "https://help.figma.com/hc/en-us/articles/8085703771159",
		}},
		DocsURL: "https://github.com/GLips/Figma-Context-MCP",
		Tags:    []string{"design", "ui", "tokens", "api-key"},
	},
	{
		ID:          "miro",
		Name:        "Miro",
		Description: "Create and read boards, sticky notes and diagrams on a Miro whiteboard.",
		Category:    "design",
		Transport:   "http",
		URL:         "https://mcp.miro.com/mcp",
		OAuth:       &OAuthRequirement{Scopes: []string{"boards:read", "boards:write"}},
		DefaultName: "miro",
		DocsURL:     "https://developers.miro.com/docs/miro-mcp",
		Tags:        []string{"whiteboard", "diagram", "collaboration"},
	},
	{
		ID:          "framer",
		Name:        "Framer",
		Description: "Read pages, components and CMS content of a Framer website project.",
		Category:    "design",
		Transport:   "http",
		URL:         "https://mcp.framer.com/mcp",
		OAuth:       &OAuthRequirement{Scopes: []string{"mcp:project"}},
		DefaultName: "framer",
		DocsURL:     "https://www.framer.com/",
		Tags:        []string{"web-design", "site-builder"},
	},
	{
		ID:          "spline",
		Name:        "Spline",
		Description: "Read and edit 3D scenes and objects in a Spline project.",
		Category:    "design",
		Transport:   "http",
		URL:         "https://mcp.spline.design/mcp",
		OAuth:       &OAuthRequirement{Scopes: []string{"spline:read", "spline:write"}},
		DefaultName: "spline",
		DocsURL:     "https://spline.design/mcp",
		Tags:        []string{"3d", "motion", "design"},
	},

	// --- 国内设计工具 ---
	// 同样按"第三方客户端能否自助接入"实测筛选（2026-11）：
	//   - Pixso / 墨刀 / MasterGo 都有**官方**远程 MCP。Pixso 是标准 OAuth：
	//     https://pixso.cn/mcp 返回 401 + resource_metadata，well-known 里有
	//     registration_endpoint，用 go-magic 的注册参数 POST
	//     /api/user/pixso/oauth2/register 实测 201，StartOAuthManual 能产出
	//     带 PKCE 的 authorize URL（与 linear 同一条链路）。墨刀、MasterGo
	//     不做 OAuth 发现，只认个人令牌，走 header；MasterGo 的头名是
	//     x-mg-useraccesstoken（缺 token 时它自己会在响应里提示这个头名）。
	//   - 即时设计（js.design）没有公共远程端点，官方给的是"画布桥接"：
	//     本地 npx 进程用 WebSocket 连设计端里的 MCP Bridge 插件。它依赖本机
	//     装着客户端，已移到 examples/plugins/10-mcp-jsdesign（实测
	//     `npx -y jsdesign-mcp` 的 initialize/tools 都正常，但没开插件时工具
	//     调用会失败）。
	//   - 蓝湖官方没有 MCP，社区 mcp-lanhu 用浏览器 Cookie 读设计稿；实测
	//     initialize 正常，凭据走 LANHU_COOKIE（Cookie 会过期，需要重填）。
	{
		ID:          "pixso",
		Name:        "Pixso",
		Description: "Read design nodes, styles, variables and components in Pixso, and turn frames into code. Official remote MCP with OAuth — no client registration needed.",
		Category:    "design",
		Transport:   "http",
		URL:         "https://pixso.cn/mcp",
		OAuth:       &OAuthRequirement{Scopes: []string{"mcp:connect"}},
		DefaultName: "pixso",
		DocsURL:     "https://pixso.net/help/en/guide/104/1760931757629102",
		Tags:        []string{"design", "ui", "design-to-code", "cn"},
	},
	{
		ID:          "mastergo",
		Name:        "MasterGo",
		Description: "Read DSL and design-to-code data from MasterGo (莫高设计) files. Needs a personal access token; the file must be in a team space with at least team plan.",
		Category:    "design",
		Transport:   "http",
		URL:         "https://mastergo.com/mcp",
		DefaultName: "mastergo",
		Requires: []Requirement{{
			Key:         "MASTERGO_MCP_TOKEN",
			Label:       "Personal access token",
			Where:       WhereHeader,
			Header:      "x-mg-useraccesstoken",
			Value:       "{{MASTERGO_MCP_TOKEN}}",
			Secret:      true,
			Required:    true,
			Placeholder: "MG_MCP_TOKEN",
			HelpURL:     "https://mastergo.com/help/ai-features/magic-mcp",
		}},
		DocsURL: "https://mastergo.com/help/ai-features/magic-mcp",
		Tags:    []string{"design", "ui", "design-to-code", "cn"},
	},
	{
		ID:          "modao",
		Name:        "Modao (墨刀)",
		Description: "Generate HTML prototypes, React apps and PRD documents with Modao AI. Official Streamable HTTP MCP authenticated by a personal token header.",
		Category:    "design",
		Transport:   "http",
		URL:         "https://modao.cc/agent-py/ai/mcp",
		DefaultName: "modao",
		Requires: []Requirement{{
			Key:      "MODAO_TOKEN",
			Label:    "Personal access token",
			Where:    WhereHeader,
			Header:   "modao-token",
			Value:    "{{MODAO_TOKEN}}",
			Secret:   true,
			Required: true,
			HelpURL:  "https://modao.cc/feature/ai-mcp.html",
		}},
		DocsURL: "https://modao.cc/feature/ai-mcp.html",
		Tags:    []string{"prototype", "prd", "html", "cn"},
	},
	{
		ID:          "lanhu",
		Name:        "蓝湖 (Lanhu)",
		Description: "Read Lanhu design specs, layer styles, slices and PRDs for design-to-code. Community implementation: needs a Lanhu browser cookie, which expires and has to be refreshed.",
		Category:    "design",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "mcp-lanhu"},
		DefaultName: "lanhu",
		Requires: []Requirement{{
			Key:         "LANHU_COOKIE",
			Label:       "Lanhu browser cookie",
			Where:       WhereEnv,
			Secret:      true,
			Required:    true,
			Placeholder: "session=...; user_token=...",
			HelpURL:     "https://github.com/MrDgbot/lanhu-mcp",
		}},
		DocsURL: "https://github.com/MrDgbot/lanhu-mcp",
		Tags:    []string{"design", "handoff", "design-to-code", "cn"},
	},

	// --- 这里刻意没有「本地软件桥」条目 ---
	// Blender / Unity / Godot、Figma 桌面版、即时设计这些 MCP 的 server 侧没问题，
	// 但真正干活的是**装在同一台机器上**的宿主软件（引擎 / 设计客户端 / 桥接插件）。
	// go-magic 经常部署在服务器或另一台机器上，内置进目录就等于一条"点了必然连不上"
	// 的条目——用户只会看到超时或空工具列表，无法自己判断是环境问题。
	// 这类条目改为随示例插件分发（一个引擎/客户端一个，按需只装自己用的）：
	//   examples/plugins/06-mcp-blender
	//   examples/plugins/07-mcp-unity
	//   examples/plugins/08-mcp-godot
	//   examples/plugins/09-mcp-figma-desktop
	//   examples/plugins/10-mcp-jsdesign
	// 要加回内置前先想清楚服务端部署场景。Unreal 也在这批里，但理由不同：它要
	// UE 5.8+ 的内置 ModelContextProtocol 插件，且必须同时启用 All Toolsets
	// （只开前者时 server 在跑、tools/list 却是空的）再勾 Auto Start Server，
	// 配置差异太大，只留手填自定义 MCP 的路：Transport=http，
	// URL=http://127.0.0.1:8000/mcp。

	// --- Browser & Automation ---
	{
		ID:          "playwright",
		Name:        "Playwright",
		Description: "Drive a real browser: navigate, click, screenshot, extract.",
		Category:    "automation",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@playwright/mcp@latest"},
		DefaultName: "playwright",
		DocsURL:     "https://github.com/microsoft/playwright-mcp",
		Tags:        []string{"browser", "screenshot", "testing"},
	},
	{
		ID:          "puppeteer",
		Name:        "Puppeteer",
		Description: "Headless Chrome automation and page scraping.",
		Category:    "automation",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-puppeteer"},
		DefaultName: "puppeteer",
		DocsURL:     "https://github.com/modelcontextprotocol/servers/tree/main/src/puppeteer",
		Tags:        []string{"browser", "scraping"},
	},

	// --- Home & Devices ---
	// 内置工具里原本就有一整套 ha_* 工具，但只读环境变量 HASS_URL/HASS_TOKEN，
	// 在 Web UI 里等于用不了。官方 MCP Server integration（2025.2+）覆盖实体、
	// 服务与 Assist 意图，且能被别的客户端复用，做成目录条目后"填地址 + 填令牌"
	// 就能装上。地址前缀是用户自己的实例，所以写成 {{HASS_URL}} 模板：私网地址
	// 允许明文 http（见 agentplugin.localOnlyHost），公网仍强制 https。
	{
		ID:          "homeassistant",
		Name:        "Home Assistant",
		Description: "Official Home Assistant MCP Server: read entity states, call services and run Assist intents on your own instance (needs the Model Context Protocol Server integration).",
		Category:    "home",
		Transport:   "http",
		URL:         "{{HASS_URL}}/api/mcp",
		DefaultName: "homeassistant",
		Requires: []Requirement{
			{
				Key:         "HASS_URL",
				Label:       "Instance URL",
				Where:       WhereURL,
				Required:    true,
				Placeholder: "http://192.168.1.10:8123",
				HelpURL:     "https://www.home-assistant.io/integrations/mcp_server/",
			},
			{
				Key:         "HASS_TOKEN",
				Label:       "Long-lived access token",
				Where:       WhereHeader,
				Header:      "Authorization",
				Value:       "Bearer {{HASS_TOKEN}}",
				Secret:      true,
				Required:    true,
				Placeholder: "eyJhbGciOiJIUzI1NiIsInR5cCI6…",
				HelpURL:     "https://www.home-assistant.io/docs/authentication/",
			},
		},
		DocsURL: "https://www.home-assistant.io/integrations/mcp_server/",
		Tags:    []string{"home-assistant", "hass", "iot", "smart-home"},
	},

	// --- Local & Files ---
	{
		ID:          "filesystem",
		Name:        "Filesystem",
		Description: "Read and write files under directories you allow.",
		Category:    "local",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-filesystem", "/path/to/allowed/dir"},
		DefaultName: "filesystem",
		DocsURL:     "https://github.com/modelcontextprotocol/servers/tree/main/src/filesystem",
		Tags:        []string{"files", "local"},
	},
	{
		ID:          "memory",
		Name:        "Memory",
		Description: "A knowledge graph the model can persist facts into.",
		Category:    "local",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-memory"},
		DefaultName: "memory",
		DocsURL:     "https://github.com/modelcontextprotocol/servers/tree/main/src/memory",
		Tags:        []string{"memory", "knowledge-graph"},
	},
	{
		ID:          "time",
		Name:        "Time",
		Description: "Current time and timezone conversions.",
		Category:    "local",
		Transport:   "stdio",
		Command:     "uvx",
		Args:        []string{"mcp-server-time"},
		DefaultName: "time",
		DocsURL:     "https://github.com/modelcontextprotocol/servers/tree/main/src/time",
		Tags:        []string{"time", "timezone"},
	},
}

// All 返回全部条目（按分类再按名字排序，保证列表稳定）。
func All() []Connector {
	out := make([]Connector, len(catalog))
	copy(out, catalog)
	order := map[string]int{}
	for i, c := range Categories() {
		order[c.ID] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		oi, oj := order[out[i].Category], order[out[j].Category]
		if oi != oj {
			return oi < oj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Search 按关键词与分类过滤。q 为空表示不过滤；匹配 id/name/description/tags。
func Search(q, category string) []Connector {
	q = strings.ToLower(strings.TrimSpace(q))
	category = strings.TrimSpace(category)

	out := make([]Connector, 0, len(catalog))
	for _, c := range All() {
		if category != "" && c.Category != category {
			continue
		}
		if q != "" && !matches(c, q) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func matches(c Connector, q string) bool {
	if strings.Contains(strings.ToLower(c.ID), q) ||
		strings.Contains(strings.ToLower(c.Name), q) ||
		strings.Contains(strings.ToLower(c.Description), q) {
		return true
	}
	for _, t := range c.Tags {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}

// Get 按 id 查找条目。
func Get(id string) (Connector, bool) {
	for _, c := range catalog {
		if c.ID == id {
			return c, true
		}
	}
	return Connector{}, false
}
