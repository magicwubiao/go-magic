package agentplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/pkg/connectors"
)

// 局域网设备的地址前缀是用户自己填的（Home Assistant 这类）：加载期看不出
// 真实主机，只能放行占位，连接前用展开后的地址复核。两条路径必须共用同一套
// 规则，否则 ${CRED:X} 就成了绕过 https 的后门。
func TestLocalOnlyHost(t *testing.T) {
	local := []string{
		"", "localhost", "LOCALHOST", "api.localhost",
		"127.0.0.1", "127.1.2.3", "::1",
		"10.0.0.5", "172.16.4.2", "172.31.255.1", "192.168.1.10",
		"169.254.10.20", "fe80::1", "fc00::1", "fd12:3456::1",
		"homeassistant.local", "hass.local.", "nas.lan", "box.home", "srv.internal",
		"nas", "hass",
	}
	for _, h := range local {
		if !localOnlyHost(h) {
			t.Errorf("localOnlyHost(%q) = false, want true", h)
		}
	}

	remote := []string{
		"example.com", "mcp.example.com", "api.x.com",
		"8.8.8.8", "1.1.1.1", "172.32.0.1", "172.15.0.1",
		"2001:4860:4860::8888",
	}
	for _, h := range remote {
		if localOnlyHost(h) {
			t.Errorf("localOnlyHost(%q) = true, want false", h)
		}
	}
}

func TestValidateHTTPSpecAllowsPrivateHTTP(t *testing.T) {
	ok := []string{
		"http://localhost:8123/api/mcp",
		"http://127.0.0.1:8123/api/mcp",
		"http://192.168.1.10:8123/api/mcp",
		"http://homeassistant.local:8123/api/mcp",
		"http://nas:5244/mcp",
		"https://mcp.example.com/mcp",
	}
	for _, u := range ok {
		spec := MCPServerSpec{Type: TransportStreamableHTTP, URL: u}
		if _, err := validateHTTPSpec(spec); err != nil {
			t.Errorf("validateHTTPSpec(%q) = %v, want ok", u, err)
		}
	}

	bad := []string{
		"http://mcp.example.com/mcp",
		"http://8.8.8.8/mcp",
		"ftp://example.com/mcp",
		"https://user:pw@example.com/mcp",
		"https://example.com/mcp#frag",
	}
	for _, u := range bad {
		spec := MCPServerSpec{Type: TransportStreamableHTTP, URL: u}
		if _, err := validateHTTPSpec(spec); err == nil {
			t.Errorf("validateHTTPSpec(%q) accepted, want error", u)
		}
	}
}

// URL 里的凭据占位：加载期放行（主机未知），展开后必须照样过校验。
func TestCredentialURLDeferredValidation(t *testing.T) {
	spec := MCPServerSpec{Type: TransportStreamableHTTP, URL: "${CRED:HASS_URL}/api/mcp"}
	if _, err := validateHTTPSpec(spec); err != nil {
		t.Fatalf("placeholder url rejected at load time: %v", err)
	}

	// 展开成局域网地址：允许。
	applied, missing := applyCredentials(spec, map[string]string{"HASS_URL": "http://192.168.1.10:8123"})
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
	if applied.URL != "http://192.168.1.10:8123/api/mcp" {
		t.Fatalf("url = %q", applied.URL)
	}
	if _, err := validateHTTPSpec(applied); err != nil {
		t.Fatalf("expanded private url rejected: %v", err)
	}

	// 展开成公网明文：必须在连接前被拒（否则等于关掉 TLS 要求）。
	pub, _ := applyCredentials(spec, map[string]string{"HASS_URL": "http://evil.example.com"})
	if _, err := validateHTTPSpec(pub); err == nil {
		t.Fatal("expanded public http url accepted, want error")
	}

	// 没填：报缺失，不要揣着字面量去连。
	if _, missing := applyCredentials(spec, nil); len(missing) != 1 || missing[0] != "HASS_URL" {
		t.Fatalf("missing = %v, want [HASS_URL]", missing)
	}
	// 原 spec 不能被改写（重连以 mcp.json 为基准）。
	if spec.URL != "${CRED:HASS_URL}/api/mcp" {
		t.Fatalf("orig spec mutated: %q", spec.URL)
	}
}

// Home Assistant 目录条目物化后：地址带 HASS_URL 占位，请求头是 Bearer 令牌，
// 两个 key 都要出现在凭据表单里。
func TestCatalogPackHomeAssistant(t *testing.T) {
	pack := func() *CatalogPack {
		for _, p := range mcpCatalogPacks() {
			if p.ID == "homeassistant" {
				return &p
			}
		}
		return nil
	}()
	if pack == nil {
		t.Fatal("homeassistant pack not generated")
	}

	conn, ok := connectors.Get("homeassistant")
	if !ok {
		t.Fatal("homeassistant entry missing from catalog")
	}
	spec := mcpOutToSpec(buildMCPSpec(conn, nil))
	if spec.Type != TransportStreamableHTTP {
		t.Fatalf("transport = %q", spec.Type)
	}
	if spec.URL != "${CRED:HASS_URL}/api/mcp" {
		t.Fatalf("url = %q", spec.URL)
	}
	if got := spec.Headers["Authorization"]; got != "Bearer ${CRED:HASS_TOKEN}" {
		t.Fatalf("Authorization = %q", got)
	}
	if len(spec.Env) != 0 {
		t.Fatalf("env = %v, want empty", spec.Env)
	}

	keys := SpecCredentialKeys(spec)
	want := []string{"HASS_TOKEN", "HASS_URL"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	if strings.Join(pack.RequiredEnv, ",") != strings.Join(want, ",") {
		t.Fatalf("pack.RequiredEnv = %v, want %v", pack.RequiredEnv, want)
	}

	// 元数据要能一路透到前端：地址不隐藏、token 隐藏、两者都有获取入口。
	byKey := map[string]CredentialField{}
	for _, f := range pack.CredentialFields {
		byKey[f.Key] = f
	}
	if byKey["HASS_URL"].Secret || byKey["HASS_URL"].Placeholder == "" || byKey["HASS_URL"].HelpURL == "" {
		t.Fatalf("HASS_URL field = %+v", byKey["HASS_URL"])
	}
	if !byKey["HASS_TOKEN"].Secret || byKey["HASS_TOKEN"].HelpURL == "" {
		t.Fatalf("HASS_TOKEN field = %+v", byKey["HASS_TOKEN"])
	}
}

// 真装一遍：过去这条会因为 "non-loopback url must use https" 直接装不上，
// 现在 mcp.json 里存的是占位地址，加载期放行、连接前复核。
func TestInstallHomeAssistantPackEndToEnd(t *testing.T) {
	isolatedHome(t)

	dir, name, required, err := InstallCatalogPack(CatalogKindMCP, "homeassistant", nil)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if name != "mcp-homeassistant" {
		t.Fatalf("name = %q", name)
	}
	if strings.Join(required, ",") != "HASS_TOKEN,HASS_URL" {
		t.Fatalf("required = %v", required)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		`"url": "${CRED:HASS_URL}/api/mcp"`,
		`"Authorization": "Bearer ${CRED:HASS_TOKEN}"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("mcp.json missing %s:/n%s", want, body)
		}
	}

	p := Load(dir, filepath.Join(t.TempDir(), "data"))
	if err := verifyLoadedPack(p); err != nil {
		t.Fatalf("loaded pack invalid: %v", err)
	}
	keys := SpecCredentialKeys(p.MCPServers[0].Spec)
	if strings.Join(keys, ",") != "HASS_TOKEN,HASS_URL" {
		t.Fatalf("credential keys = %v", keys)
	}
}

// 展开后的地址要在连接前复核：否则 ${CRED:HASS_URL} 可以填成任意明文公网
// 地址，加载期的 https 规则就形同虚设。
func TestConnectEntryRejectsExpandedPublicHTTPURL(t *testing.T) {
	r := NewRuntime(&Plugin{})
	r.SetCredentials(map[string]string{
		"HASS_URL":   "http://evil.example.com",
		"HASS_TOKEN": "t",
	})
	entry := &MCPEntryStatus{Name: "ha", Spec: MCPServerSpec{
		Type:    TransportStreamableHTTP,
		URL:     "${CRED:HASS_URL}/api/mcp",
		Headers: map[string]string{"Authorization": "Bearer ${CRED:HASS_TOKEN}"},
	}}

	_, err := r.connectEntry(entry)
	if err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("err = %v, want https rejection", err)
	}
	if kind := mcp.KindOf(err); kind != mcp.ErrKindAuth {
		t.Fatalf("kind = %q, want auth", kind)
	}
}
