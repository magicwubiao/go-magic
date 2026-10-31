package agentplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/connectors"
)

// 邮箱类条目（Gmail / QQ Mail）共用 imap-smtp-email-mcp：主机与端口是模板写死
// 的固定值，用户只填账号和应用专用密码/授权码。这里锁住三件事：
//   - 固定 env 真的进了 mcp.json（否则子进程不知道该连哪台服务器）；
//   - 待填项只有 EMAIL_USER / EMAIL_PASS，且密码类字段标了 secret + help_url；
//   - 固定值不会把用户的凭据键一起"填死"（Requires 必须能覆盖同名固定值）。

func mailSpec(t *testing.T, id string) (MCPServerSpec, *CatalogPack) {
	t.Helper()
	conn, ok := connectors.Get(id)
	if !ok {
		t.Fatalf("%s entry missing from catalog", id)
	}
	var pack *CatalogPack
	for _, p := range mcpCatalogPacks() {
		if p.ID == id {
			p := p
			pack = &p
			break
		}
	}
	if pack == nil {
		t.Fatalf("%s pack not generated", id)
	}
	return mcpOutToSpec(buildMCPSpec(conn, nil)), pack
}

func TestMailboxConnectors(t *testing.T) {
	cases := []struct {
		id                     string
		wantIMAPHost, wantHost string
		wantHelp               string
	}{
		{"gmail", "imap.gmail.com", "smtp.gmail.com", "https://myaccount.google.com/apppasswords"},
		{"qqmail", "imap.qq.com", "smtp.qq.com", "https://help.mail.qq.com/detail/0/1087"},
	}

	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			spec, pack := mailSpec(t, c.id)

			if spec.Type != TransportStdio || spec.Command != "npx" {
				t.Fatalf("transport = %s/%s", spec.Type, spec.Command)
			}
			if !strings.Contains(strings.Join(spec.Args, " "), "imap-smtp-email-mcp") {
				t.Fatalf("args = %v", spec.Args)
			}
			for k, want := range map[string]string{
				"IMAP_HOST": c.wantIMAPHost,
				"IMAP_PORT": "993",
				"SMTP_HOST": c.wantHost,
				"SMTP_PORT": "465",
			} {
				if got := spec.Env[k]; got != want {
					t.Errorf("env[%s] = %q, want %q", k, got, want)
				}
			}
			// 待填的两个键必须留空，等用户填；否则会被当成"已配置"直接连。
			for _, k := range []string{"EMAIL_USER", "EMAIL_PASS"} {
				if v, ok := spec.Env[k]; !ok || v != "" {
					t.Errorf("env[%s] = %q (present=%v), want empty placeholder", k, v, ok)
				}
			}

			keys := SpecCredentialKeys(spec)
			if strings.Join(keys, ",") != "EMAIL_PASS,EMAIL_USER" {
				t.Fatalf("keys = %v", keys)
			}

			byKey := map[string]CredentialField{}
			for _, f := range pack.CredentialFields {
				byKey[f.Key] = f
			}
			if byKey["EMAIL_USER"].Secret {
				t.Error("EMAIL_USER should not be masked")
			}
			if !byKey["EMAIL_PASS"].Secret {
				t.Error("EMAIL_PASS must be masked")
			}
			if got := byKey["EMAIL_PASS"].HelpURL; got != c.wantHelp {
				t.Errorf("EMAIL_PASS help_url = %q, want %q", got, c.wantHelp)
			}
		})
	}
}

// 固定 env 里的 {{KEY}} 也要换成凭据引用：这样"发件人地址默认等于登录账号"
// 这类条目不用重复要一次输入。
func TestCatalogFixedEnvExpandsCredRef(t *testing.T) {
	conn := connectors.Connector{
		ID:        "demo",
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "demo"},
		Env:       map[string]string{"MAIL_FROM": "{{EMAIL_USER}}", "FIXED": "keep"},
		Requires: []connectors.Requirement{
			{Key: "EMAIL_USER", Where: connectors.WhereEnv},
		},
	}
	spec := mcpOutToSpec(buildMCPSpec(conn, nil))
	if got := spec.Env["MAIL_FROM"]; got != "${CRED:EMAIL_USER}" {
		t.Fatalf("MAIL_FROM = %q", got)
	}
	if got := spec.Env["FIXED"]; got != "keep" {
		t.Fatalf("FIXED = %q", got)
	}
}

// 真装一遍：装完 mcp.json 里要有固定主机，且插件判定"缺凭据"而不是"配置错误"。
func TestInstallGmailPackEndToEnd(t *testing.T) {
	isolatedHome(t)

	dir, name, required, err := InstallCatalogPack(CatalogKindMCP, "gmail", nil)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if name != "mcp-gmail" {
		t.Fatalf("name = %q", name)
	}
	if strings.Join(required, ",") != "EMAIL_PASS,EMAIL_USER" {
		t.Fatalf("required = %v", required)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		`"IMAP_HOST": "imap.gmail.com"`,
		`"SMTP_HOST": "smtp.gmail.com"`,
		`"EMAIL_USER": ""`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("mcp.json missing %s:\n%s", want, body)
		}
	}

	p := Load(dir, filepath.Join(t.TempDir(), "data"))
	if err := verifyLoadedPack(p); err != nil {
		t.Fatalf("loaded pack invalid: %v", err)
	}
}
