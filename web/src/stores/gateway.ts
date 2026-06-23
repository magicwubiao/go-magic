import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as gatewayApi from '@/api/gateway'
import type { GatewayStatus } from '@/api/gateway'

export interface GatewayError {
  message: string
  code?: string
}

export const useGatewayStore = defineStore('gateway', () => {
  const status = ref<GatewayStatus | null>(null)
  const loading = ref(false)
  const error = ref<GatewayError | null>(null)

  async function loadStatus(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      status.value = await gatewayApi.getGatewayStatus()
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to load status: ' + errMsg }
      status.value = null
    } finally {
      loading.value = false
    }
  }

  async function restart(): Promise<boolean> {
    try {
      error.value = null
      await gatewayApi.restartGateway()
      await loadStatus()
      return true
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to restart gateway: ' + errMsg }
      return false
    }
  }

  async function start(): Promise<boolean> {
    try {
      error.value = null
      await gatewayApi.startGateway()
      await loadStatus()
      return true
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to start gateway: ' + errMsg }
      return false
    }
  }

  async function stop(): Promise<boolean> {
    try {
      error.value = null
      await gatewayApi.stopGateway()
      await loadStatus()
      return true
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to stop gateway: ' + errMsg }
      return false
    }
  }

  return { status, loading, error, loadStatus, restart, start, stop }
})
