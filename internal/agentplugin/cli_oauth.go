package agentplugin

// 插件内 CLI 连接器的 OAuth token 命名空间。
//
// 与插件 MCP 条目（agentplugin:<plugin>/<server>）和全局 CLI 连接器
// （cliconn:<name>）共用同一个 oauth_tokens.json，靠键前缀隔离：
//
//	cliconn:agentplugin/<plugin>/<connector>
//
// 为什么挂在 cliconn: 下而不是 agentplugin: 下：token 的消费方是
// cliconn.Run（注入 env），与全局连接器是同一类凭据；键前缀决定的是
// "谁在用"，不是"谁存的"。全局连接器名不允许出现 '/'，因此这个前缀
// 不会与用户自建的连接器撞键。

import "strings"

// cliOAuthKeyPrefix 是插件内 CLI 连接器的 token 键前缀。
const cliOAuthKeyPrefix = "cliconn:agentplugin/"

// CLIOAuthKey 拼出插件内 CLI 连接器的 token 存储键。
func CLIOAuthKey(pluginName, connectorName string) string {
	return cliOAuthKeyPrefix + pluginName + "/" + connectorName
}

// ParseCLIOAuthKey 把键拆回 (plugin, connector)。
// 非插件 CLI 键（全局连接器 / MCP 条目）返回 ok=false。
func ParseCLIOAuthKey(key string) (pluginName, connectorName string, ok bool) {
	if !strings.HasPrefix(key, cliOAuthKeyPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, cliOAuthKeyPrefix)
	idx := strings.LastIndex(rest, "/")
	if idx <= 0 || idx == len(rest)-1 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}
