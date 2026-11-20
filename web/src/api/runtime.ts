// 本机运行时（Node.js / Python / Git Bash）探测与一键安装。
//
// 后端只接受固定 id（node / python / gitbash），安装命令硬编码在
// internal/server/runtime.go；安装是长任务，走"提交 + 轮询"，
// 轮询复用 CLI 安装的任务接口（同一个任务表）。
import { request } from './client'
import type { CLIInstallJob, CLIInstallStartResult } from './agentplugins'

export interface RuntimeProbe {
  id: 'node' | 'python' | 'gitbash'
  name: string
  binary: string
  found: boolean
  path?: string
  version?: string
  /** 国内可达的手动下载页,自动安装失败/太慢时兜底用 */
  mirror?: string
}

export interface RuntimeListResult {
  ok: boolean
  platform: string
  runtimes: RuntimeProbe[]
}

/** 探测全部内置运行时的本机状态（found / path / version）。 */
export async function listRuntimes(): Promise<RuntimeListResult> {
  return request<RuntimeListResult>('/runtimes')
}

/** 提交一次运行时安装任务（长任务），轮询用 getCLIInstallJob。 */
export async function startRuntimeInstall(id: string): Promise<CLIInstallStartResult> {
  return request<CLIInstallStartResult>('/runtimes/install', {
    method: 'POST',
    body: JSON.stringify({ id }),
  })
}

export type { CLIInstallJob, CLIInstallStartResult }
