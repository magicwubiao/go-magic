package config

import (
	"os"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// 进程级 .env 注入器的唯一入口。
//
// 为什么必须是全局单例：`EnvSet` 的价值全在"谁把这个键设进环境"的记账上，
// 如果启动路径用一个 EnvSet、面板的写入路径用另一个，面板删掉一个 key 时
// 就查不到"这是我们设的"，于是要么不敢 Unsetenv（删了不生效），要么瞎
// Unsetenv（把运维 export 的变量干掉）。两条路径必须共享同一个实例。

// processEnv 是当前进程的 .env 注入器。
var processEnv EnvSet

// ProcessEnv 返回进程级注入器，供 server / bot / gateway 侧读写环境变量时记账。
func ProcessEnv() *EnvSet { return &processEnv }

// LoadProfileEnv 加载 <magicHome>/.env 并注入进程环境。
//
// 历史包袱：这个函数过去按"活动分身"选文件（profiles/<p>/.env）。人格预设
// 降级为内容模板后，凭据不再跟着预设走 —— 一套部署只有一份凭据，切人格
// 不该换 key（那会让"换个说话风格"变成"换账号"，语义更混乱）。旧预设目录
// 里的 .env 原样留在磁盘上，但不再被读取。
//
// 这是"面板里填的 key 到底有没有生效"的唯一开关：调用点必须早于任何
// 会把探测结果缓存起来的初始化（`sync.Once`、单例构造、Available() 判定）。
//
// 返回注入的键数。文件不存在、读盘失败都**不返回错误**：凭据文件属于可选的
// 运行期配置，缺了只影响对应能力（视觉/搜索/GitHub），不该让整个进程起不来。
// 真出错时留一条日志，便于排查"为什么 key 没用"。
func LoadProfileEnv() int {
	return LoadEnvFor(GetMagicHome())
}

// LoadEnvFor 按显式的 magicHome 加载 .env。
//
// 单独提供的原因：server 持有的 magicHome 来自它自己的配置（可能由
// `--magic-home`、测试注入或守护进程参数决定），未必等于 GetMagicHome()
// 读到的全局环境变量。两者用错，面板就会写 A 目录、进程去读 B 目录 ——
// 表现为"保存成功但工具永远说 key 没配置"。
func LoadEnvFor(magicHome string) int {
	path := EnvFilePath(magicHome, DefaultProfileName)
	if path == "" {
		log.Warnf("[env] cannot resolve .env path under %q; skipping load", magicHome)
		return 0
	}

	vars, err := LoadEnvFile(path)
	if err != nil {
		log.Warnf("[env] failed to read %s: %v", path, err)
		return 0
	}
	if len(vars) == 0 {
		return 0
	}

	applied := processEnv.Apply(vars)
	skipped := len(vars) - len(applied)
	if len(applied) > 0 {
		log.Infof("[env] loaded %d variable(s) from %s", len(applied), path)
	}
	if skipped > 0 {
		// 明确说清楚"文件里有、但环境里已经有"，避免用户以为 .env 被忽略了。
		log.Infof("[env] %d variable(s) in %s left untouched: already set in the real environment", skipped, path)
	}
	return len(applied)
}

// ActiveEnvPath 返回 .env 的路径（面板展示用）。
func ActiveEnvPath() string {
	return EnvFilePath(GetMagicHome(), DefaultProfileName)
}

// EnvVarSource 说明一个环境变量的来源，用于面板区分
// ".env 提供（可在面板修改）"与"外部环境提供（面板改了也不生效）"。
type EnvVarSource string

const (
	// EnvSourceDotEnv 表示该值来自 .env，且由本进程注入。
	EnvSourceDotEnv EnvVarSource = "dotenv"
	// EnvSourceReal 表示该值来自真实环境（shell / systemd / docker），
	// .env 里的同名键不会覆盖它。
	EnvSourceReal EnvVarSource = "environment"
	// EnvSourceUnset 表示进程环境里没有这个键。
	EnvSourceUnset EnvVarSource = "unset"
)

// EnvSourceOf 判断 key 当前生效值的来源。
func EnvSourceOf(key string) EnvVarSource {
	if processEnv.Has(key) {
		return EnvSourceDotEnv
	}
	if _, ok := os.LookupEnv(key); ok {
		return EnvSourceReal
	}
	return EnvSourceUnset
}

// SetEnvVarInstant 让单个键**立刻**在进程内生效（面板保存后无需重启）。
//
// 只写进程环境，不落盘；落盘由调用方（server 的 .env 写入）负责。分开是
// 有意的：写文件可能失败（权限/磁盘），但"用户以为保存了却没生效"和
// "保存失败"是两种不同的错误，必须分别汇报。
//
// 返回 false 表示该键由真实环境提供，面板的值不会生效。
func SetEnvVarInstant(key, value string) bool {
	if key == "" {
		return false
	}
	return processEnv.SetKey(key, value)
}

// UnsetEnvVarInstant 撤销单个键：仅当它是由 .env 注入时才真正移除。
// 返回是否真的删除了。
func UnsetEnvVarInstant(key string) bool {
	return processEnv.UnsetKey(key)
}

// IsEnvManaged 判断某个键是否由 .env 注入（即面板有权修改/删除）。
func IsEnvManaged(key string) bool { return processEnv.Has(key) }

// ManagedEnvValues 返回由 .env 注入的键值快照。
func ManagedEnvValues() map[string]string { return processEnv.Managed() }
