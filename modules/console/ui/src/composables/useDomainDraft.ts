import { computed, ref, watch, type Ref } from 'vue'
import { clone, domainPerm, type Dataset, type Domain, type Issue, type Report } from '@/lib/config'
import { reportOf, useConfigStore, type DomainPayload } from '@/stores/deceptionConfig'
import { useAuthStore } from '@/stores/auth'

/**
 * 一个可写域的编辑草稿：本地副本 + 脏标记 + 防抖预检 + 保存（带 409 处理）。
 *
 * 预检把草稿合并进**服务端当前数据集**后整体校验 —— 跨域冲突（如诱饵落在禁止欺骗路径下）也能在保存前看见。
 */
export function useDomainDraft<T>(domain: Domain, pick: (d: Dataset) => T, toPayload: (v: T) => DomainPayload) {
  const store = useConfigStore()
  const auth = useAuthStore()
  const snapshot = () => (store.dataset ? clone(pick(store.dataset)) : (undefined as unknown as T))

  const draft = ref(snapshot()) as Ref<T>
  const base = ref(JSON.stringify(draft.value))
  const dirty = computed(() => JSON.stringify(draft.value) !== base.value)
  const report = ref<Report | null>(store.view?.report ?? null)
  const validating = ref(false)
  const saving = ref(false)
  const saveError = ref('')
  const conflict = ref(false)
  const savedAt = ref(0)
  const canWrite = computed(() => auth.can(domainPerm[domain]))

  function reset(): void {
    draft.value = snapshot()
    base.value = JSON.stringify(draft.value)
    report.value = store.view?.report ?? null
    saveError.value = ''
    conflict.value = false
  }

  // 数据集被他人（或本页其他 Tab）更新：没有本地修改就跟上；有修改则保留草稿，等保存时由 409 提示。
  watch(
    () => store.dataset?.version,
    () => {
      if (!dirty.value) reset()
    },
  )

  let timer: ReturnType<typeof setTimeout> | undefined
  let seq = 0
  watch(
    draft,
    () => {
      if (!dirty.value) {
        report.value = store.view?.report ?? null
        return
      }
      clearTimeout(timer)
      timer = setTimeout(() => {
        const mine = ++seq
        validating.value = true
        void store.validate(toPayload(draft.value)).then((r) => {
          if (mine !== seq) return // 只采用最后一次输入的结论
          validating.value = false
          if (r) report.value = r
        })
      }, 400)
    },
    { deep: true },
  )

  function save(): Promise<boolean> {
    saving.value = true
    saveError.value = ''
    return store.save(domain, toPayload(draft.value)).then((err) => {
      saving.value = false
      if (!err) {
        reset()
        const at = Date.now()
        savedAt.value = at
        setTimeout(() => {
          if (savedAt.value === at) savedAt.value = 0 // 「已保存」提示 4 秒后自动收起
        }, 4000)
        return true
      }
      if (err.status === 409) conflict.value = true
      const r = reportOf(err)
      if (r) report.value = r
      saveError.value = err.message
      return false
    })
  }

  /** 409 后：拉最新数据集作为新的基线，但**保留**本地草稿（再保存时用新版本号）。 */
  function keepMineAndRefresh(): Promise<void> {
    const mine = clone(draft.value)
    return store.load().then(() => {
      base.value = JSON.stringify(snapshot())
      draft.value = mine
      conflict.value = false
      saveError.value = ''
    })
  }

  const errors = computed<Issue[]>(() => report.value?.errors ?? [])
  const warnings = computed<Issue[]>(() => report.value?.warnings ?? [])

  /** 某个字段前缀（如 `honeypots[2]`）上的错误。 */
  function issuesAt(prefix: string): Issue[] {
    return errors.value.filter((i) => i.field === prefix || i.field.startsWith(prefix + '.') || i.field.startsWith(prefix + '['))
  }

  return { draft, dirty, report, errors, warnings, validating, saving, saveError, conflict, savedAt, canWrite, reset, save, keepMineAndRefresh, issuesAt }
}

export type DomainDraft<T> = ReturnType<typeof useDomainDraft<T>>
