<template>
  <n-modal
    :show="show"
    preset="card"
    style="width: 680px; max-width: 92vw;"
    :title="t('agentplugins.cliInstallTitle', { name: displayName })"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <n-text depth="3" style="font-size: 12px; display: block;">
      {{ t('agentplugins.cliInstallHint') }}
    </n-text>

    <!-- 将要执行的命令：来源是内置目录，先把"到底跑什么"摊开给用户看 -->
    <div v-if="job && job.commands.length" class="ci-cmds">
      <code v-for="cmd in job.commands" :key="cmd" class="ci-cmd">{{ cmd }}</code>
    </div>

    <div class="ci-status">
      <n-spin v-if="phase === 'running'" :size="14" />
      <n-text v-if="phase === 'running'" depth="3" style="font-size: 12px;">
        {{ t('agentplugins.cliInstallRunning') }}
      </n-text>
      <n-tag v-else-if="phase === 'done' && job?.binary_found" size="small" type="success" :bordered="false">
        {{ t('agentplugins.cliInstallDone') }}
      </n-tag>
      <n-tag v-else-if="phase === 'done'" size="small" type="warning" :bordered="false">
        {{ t('agentplugins.cliInstallNeedRestart') }}
      </n-tag>
      <n-tag v-else-if="phase === 'failed'" size="small" type="error" :bordered="false">
        {{ t('agentplugins.cliInstallFailed') }}
      </n-tag>
    </div>

    <div v-if="job && job.output" class="ci-out-wrap">
      <div class="ci-out-label">{{ t('agentplugins.cliInstallOutput') }}</div>
      <pre class="ci-out">{{ job.output }}</pre>
      <n-text v-if="job.truncated" depth="3" style="font-size: 12px;">
        {{ t('agentplugins.cliInstallTruncated') }}
      </n-text>
    </div>

    <n-text v-if="phase === 'failed' && job?.error" depth="3" class="ci-err">
      {{ job.error }}
    </n-text>

    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="emit('update:show', false)">{{ t('common.close') }}</n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { NButton, NModal, NSpace, NSpin, NTag, NText, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  getCLIInstallJob,
  startCLIInstall,
  type CLIInstallJob,
} from '@/api/agentplugins'

/**
 * CLI 一键安装弹窗：提交任务 + 轮询状态 + 实时输出。
 *
 * 为什么单独抽组件：插件详情里的"命令未安装"与目录卡的"缺命令"是同一个动作，
 * 两处各写一份轮询迟早不一致（一边不清理定时器、一边忘了刷新列表）。
 *
 * 传参二选一：plugin（已安装插件，后端反查目录条目）或 catalogId（还没装）。
 */
const props = defineProps<{
  show: boolean
  /** 已安装插件名：后端从 plugin.json 的 extensions 反查目录条目 */
  plugin?: string
  /** 目录条目 id：安装前从目录卡直接装命令 */
  catalogId?: string
  /** 弹窗标题里显示的名字 */
  displayName?: string
}>()

const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  /** done: 安装任务结束（binaryFound 表示命令是否已经可用） */
  (e: 'done', payload: { binaryFound: boolean }): void
}>()

const { t } = useI18n()
const message = useMessage()

const job = ref<CLIInstallJob | null>(null)
const phase = computed<'running' | 'done' | 'failed'>(() => job.value?.state ?? 'running')
const displayName = computed(() => props.displayName || props.catalogId || props.plugin || '')

let timer: number | undefined

function stopTimer() {
  if (timer !== undefined) {
    window.clearInterval(timer)
    timer = undefined
  }
}

/** 轮询间隔 1.5s：包管理器的输出本来就是"几秒一段"，再快只是浪费请求。 */
function startPolling(id: string) {
  stopTimer()
  timer = window.setInterval(() => void poll(id), 1500)
}

async function poll(id: string) {
  try {
    const res = await getCLIInstallJob(id)
    job.value = res.job
    if (res.job.state !== 'running') {
      stopTimer()
      finish(res.job)
    }
  } catch {
    // 任务丢了（进程重启 / 超过保留期）：停掉轮询，让用户重新点一次。
    stopTimer()
    job.value = job.value ? { ...job.value, state: 'failed', error: '' } : null
  }
}

function finish(done: CLIInstallJob) {
  emit('done', { binaryFound: !!done.binary_found })
  if (done.state === 'failed') {
    message.error(t('agentplugins.cliInstallFailed'))
    return
  }
  if (done.binary_found) {
    message.success(t('agentplugins.cliInstallDone'))
  } else {
    message.warning(t('agentplugins.cliInstallNeedRestart'))
  }
}

async function start() {
  job.value = null
  stopTimer()
  try {
    const res = await startCLIInstall(props.plugin ? { plugin: props.plugin } : { id: props.catalogId })
    job.value = res.job
    if (res.reused) message.info(t('agentplugins.cliInstallReused'))
    if (res.job.state === 'running') {
      startPolling(res.job.id)
    } else {
      finish(res.job)
    }
  } catch (e: any) {
    message.error(e?.message || t('agentplugins.cliInstallFailed'))
    emit('update:show', false)
  }
}

watch(
  () => props.show,
  (visible) => {
    if (visible) void start()
    else stopTimer()
  },
)

onBeforeUnmount(stopTimer)
</script>

<style scoped>
.ci-cmds {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 10px;
}
.ci-cmd {
  font-size: 12px;
  padding: 6px 10px;
  border-radius: 6px;
  background: #f5f6f8;
  color: #333;
  word-break: break-all;
}
.ci-status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
}
.ci-out-wrap {
  margin-top: 12px;
}
.ci-out-label {
  font-size: 12px;
  color: #909399;
  margin-bottom: 6px;
}
.ci-out {
  max-height: 320px;
  overflow: auto;
  margin: 0 0 6px;
  padding: 10px;
  border-radius: 8px;
  background: #f8f9fb;
  border: 1px solid #ebedf0;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
}
.ci-err {
  display: block;
  margin-top: 8px;
  font-size: 12px;
  word-break: break-all;
}

@media (prefers-color-scheme: dark) {
  .ci-cmd {
    background: #26262b;
    color: #d8d8d8;
  }
  .ci-out {
    background: #18181c;
    border-color: #2c2c30;
    color: #d8d8d8;
  }
}
</style>
