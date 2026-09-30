import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { ApiError, api } from '@/lib/api'
import type { DatasetView, Domain, Report, SyncStatus, SystemAlert, TemplatesView, SeedOutcome } from '@/lib/config'

/** 保存某个域时请求体里的那一段（注入规则带 injects_provided）。 */
export type DomainPayload = Record<string, unknown>

/**
 * 欺骗管控数据集的前端状态：当前数据集 + 模板 + 同步状态。
 *
 * 写入一律带 expected_version（整体乐观锁）；409 时保留调用方草稿，由页面提示「刷新并保留我的修改」。
 */
export const useConfigStore = defineStore('deception-config', () => {
  const view = ref<DatasetView | null>(null)
  const templates = ref<TemplatesView | null>(null)
  const sync = ref<SyncStatus | null>(null)
  const alerts = ref<SystemAlert[]>([])
  const loading = ref(false)
  const error = ref<ApiError | null>(null)

  const dataset = computed(() => view.value?.dataset ?? null)
  const version = computed(() => view.value?.dataset.version ?? 0)

  function load(): Promise<void> {
    loading.value = true
    return api
      .get<DatasetView>('/api/v1/config/dataset')
      .then((v) => {
        view.value = v
        sync.value = v.sync
        error.value = null
      })
      .catch((err: unknown) => {
        console.error('加载欺骗管控数据集失败', err)
        error.value = err instanceof ApiError ? err : new ApiError(0, 'network', '网络错误')
      })
      .finally(() => {
        loading.value = false
      })
  }

  function loadTemplates(): Promise<void> {
    if (templates.value) return Promise.resolve()
    return api
      .get<TemplatesView>('/api/v1/config/templates')
      .then((t) => {
        templates.value = t
      })
      .catch((err: unknown) => console.error('加载内置模板失败', err))
  }

  function refreshSync(): Promise<void> {
    return api
      .get<{ sync: SyncStatus; alerts: SystemAlert[]; seed: SeedOutcome; dataset_version: number }>('/api/v1/config/sync')
      .then((r) => {
        sync.value = r.sync
        alerts.value = r.alerts
        // 他人保存了新版本：静默刷新数据集（本地未保存的草稿由各 Tab 自己保留）。
        if (view.value && r.dataset_version !== view.value.dataset.version) void load()
      })
      .catch((err: unknown) => console.error('刷新同步状态失败', err))
  }

  /** 预检（不保存）：返回合并草稿后的整份校验报告。 */
  function validate(draft: DomainPayload): Promise<Report | null> {
    return api
      .post<{ report: Report }>('/api/v1/config/validate', draft)
      .then((r) => r.report)
      .catch((err: unknown) => {
        console.error('预检失败', err)
        return null
      })
  }

  /** 保存一个域。成功返回 null；失败返回 ApiError（validation_failed 的 body 带 report）。 */
  function save(domain: Domain, payload: DomainPayload): Promise<ApiError | null> {
    return api
      .put<DatasetView>(`/api/v1/config/${domain}`, { expected_version: version.value, ...payload })
      .then((v) => {
        view.value = v
        sync.value = v.sync
        return null
      })
      .catch((err: unknown) => {
        console.error(`保存 ${domain} 失败`, err)
        return err instanceof ApiError ? err : new ApiError(0, 'network', '网络错误')
      })
  }

  function rollback(to: number): Promise<ApiError | null> {
    return api
      .post<DatasetView>('/api/v1/config/rollback', { to, expected_version: version.value })
      .then((v) => {
        view.value = v
        sync.value = v.sync
        return null
      })
      .catch((err: unknown) => {
        console.error('回滚失败', err)
        return err instanceof ApiError ? err : new ApiError(0, 'network', '网络错误')
      })
  }

  return { view, templates, sync, alerts, loading, error, dataset, version, load, loadTemplates, refreshSync, validate, save, rollback }
})

/** 从 ApiError 里取出服务端附带的校验报告（validation_failed）。 */
export function reportOf(err: ApiError | null): Report | null {
  const body = err?.body as { report?: Report } | undefined
  return body?.report ?? null
}
