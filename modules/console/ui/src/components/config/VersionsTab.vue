<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { History, RotateCcw } from 'lucide-vue-next'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import Badge from '@/components/ui/badge/Badge.vue'
import { Button } from '@/components/ui/button'
import Input from '@/components/ui/input/Input.vue'
import Sheet from '@/components/ui/sheet/Sheet.vue'
import { api } from '@/lib/api'
import type { Dataset, VersionMeta } from '@/lib/config'
import { fmtDateTime } from '@/lib/format'
import { useAuthStore } from '@/stores/auth'
import { useConfigStore } from '@/stores/deceptionConfig'

const store = useConfigStore()
const auth = useAuthStore()
const versions = ref<VersionMeta[]>([])
const selected = ref(0)
const left = ref<Dataset | null>(null)
const right = computed<Dataset | null>(() => store.dataset)
const confirmOpen = ref(false)
const confirmText = ref('')
const busy = ref(false)
const error = ref('')

function loadVersions(): void {
  void api
    .get<{ versions: VersionMeta[] }>('/api/v1/config/versions')
    .then((r) => {
      versions.value = r.versions
      if (!selected.value && r.versions.length > 1) selected.value = r.versions[1].version
    })
    .catch((err: unknown) => console.error('加载版本时间线失败', err))
}

function fetchVersion(v: number): Promise<Dataset | null> {
  return api
    .get<{ dataset: Dataset }>(`/api/v1/config/versions/${v}`)
    .then((r) => r.dataset)
    .catch((err: unknown) => {
      console.error('加载历史版本失败', err)
      return null
    })
}

watch(() => store.version, loadVersions, { immediate: true })
watch(selected, (v) => {
  if (!v) return
  void fetchVersion(v).then((d) => (left.value = d))
})

const strip = (d: Dataset | null) =>
  d ? JSON.stringify({ honeypots: d.honeypots, decoys: d.decoys, whitelist: d.whitelist, blacklist: d.blacklist, injects: d.injects, injects_provided: d.injects_provided, bindings: d.bindings }, null, 2).split('\n') : []

/** 逐行对比（LCS），产出并排视图的行。数据集规模有上限，行数在千级，O(n·m) 足够。 */
const diff = computed(() => {
  const a = strip(left.value)
  const b = strip(right.value)
  const n = a.length
  const m = b.length
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1])
  const rows: { l: string; r: string; kind: 'same' | 'del' | 'add' }[] = []
  let i = 0
  let j = 0
  while (i < n || j < m) {
    if (i < n && j < m && a[i] === b[j]) rows.push({ l: a[i++], r: b[j++], kind: 'same' })
    else if (j < m && (i >= n || dp[i][j + 1] >= dp[i + 1][j])) rows.push({ l: '', r: b[j++], kind: 'add' })
    else rows.push({ l: a[i++], r: '', kind: 'del' })
  }
  return rows
})
const changed = computed(() => diff.value.filter((r) => r.kind !== 'same').length)

function rollback(): void {
  busy.value = true
  error.value = ''
  void store.rollback(selected.value).then((err) => {
    busy.value = false
    if (err) {
      error.value = err.message
      return
    }
    confirmOpen.value = false
    confirmText.value = ''
  })
}
</script>

<template>
  <div class="grid grid-cols-1 gap-4 xl:grid-cols-[300px_minmax(0,1fr)]">
    <SpotlightCard :interactive="false" class="p-4" body-class="flex flex-col gap-3">
      <h2 class="flex items-center gap-2 text-base font-medium"><History class="h-4 w-4 text-primary" />版本时间线</h2>
      <ol class="relative flex max-h-[70vh] flex-col gap-1 overflow-y-auto border-l border-border/60 pl-4">
        <li v-for="v in versions" :key="v.version" class="relative">
          <span class="absolute -left-[21px] top-3 h-2.5 w-2.5 rounded-full border-2 border-background" :class="v.version === store.version ? 'bg-origin' : 'bg-primary/60'" />
          <button
            type="button"
            class="flex w-full cursor-pointer flex-col gap-0.5 rounded-lg px-3 py-2 text-left transition hover:bg-primary/5"
            :class="selected === v.version ? 'glow-ring bg-primary/10' : ''"
            :disabled="v.version === store.version"
            @click="selected = v.version"
          >
            <span class="flex items-center gap-2 text-sm font-medium">
              v{{ v.version }}<Badge v-if="v.version === store.version" tone="origin">当前</Badge>
            </span>
            <span class="text-[11px] text-muted-foreground">{{ v.updated_by }} · {{ fmtDateTime(v.updated_at) }}</span>
            <span class="text-xs text-foreground/80">{{ v.summary || '—' }}</span>
          </button>
        </li>
      </ol>
      <p class="text-[11px] text-muted-foreground">保留最近 50 个历史版本。回滚会以旧内容发布一个<strong>新版本</strong>（版本号只增不减，边缘对账无需特判）。</p>
    </SpotlightCard>

    <SpotlightCard :interactive="false" class="min-w-0 p-4" body-class="flex min-w-0 flex-col gap-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h2 class="text-base font-medium">
          差异：<span class="font-mono">v{{ selected || '—' }}</span> → <span class="font-mono">v{{ store.version }}（当前）</span>
          <span class="ml-2 text-xs text-muted-foreground">{{ changed }} 行变化</span>
        </h2>
        <Button v-if="auth.can('config:admin')" variant="destructive" size="sm" :disabled="!selected || selected === store.version" @click="confirmOpen = true">
          <RotateCcw class="h-3.5 w-3.5" />回滚到 v{{ selected }}
        </Button>
      </div>
      <div class="grid max-h-[70vh] grid-cols-2 overflow-auto rounded-lg border border-border/60 font-mono text-[11px] leading-5">
        <template v-for="(row, k) in diff" :key="k">
          <pre class="whitespace-pre-wrap break-all border-r border-border/40 px-3" :class="row.kind === 'del' ? 'bg-danger/15 text-danger' : row.kind === 'add' ? 'bg-muted/20' : 'text-muted-foreground'">{{ row.l }}</pre>
          <pre class="whitespace-pre-wrap break-all px-3" :class="row.kind === 'add' ? 'bg-origin/15 text-origin' : row.kind === 'del' ? 'bg-muted/20' : 'text-muted-foreground'">{{ row.r }}</pre>
        </template>
      </div>
    </SpotlightCard>
  </div>

  <Sheet v-model:open="confirmOpen" side="center" :title="`回滚到 v${selected}`" :description="`以 v${selected} 的内容发布一个新版本（v${store.version + 1}）；核心终检通过后热替换生效。`">
    <p class="text-sm text-muted-foreground">请输入版本号 <span class="font-mono text-foreground">{{ selected }}</span> 确认：</p>
    <Input v-model="confirmText" class="mt-2 font-mono" :placeholder="String(selected)" />
    <p v-if="error" role="alert" class="mt-2 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{{ error }}</p>
    <template #footer>
      <Button variant="ghost" @click="confirmOpen = false">取消</Button>
      <Button variant="destructive" :loading="busy" :disabled="confirmText.trim() !== String(selected)" @click="rollback">确认回滚</Button>
    </template>
  </Sheet>
</template>
