<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { Ban, Plus, ShieldCheck, Trash2 } from 'lucide-vue-next'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import { Button } from '@/components/ui/button'
import Input from '@/components/ui/input/Input.vue'
import SaveBar from './SaveBar.vue'
import TagInput from './TagInput.vue'
import ValidationPanel from './ValidationPanel.vue'
import { useDomainDraft } from '@/composables/useDomainDraft'

const wl = useDomainDraft('whitelist', (ds) => ds.whitelist, (v) => ({ whitelist: v }))
const bl = useDomainDraft('blacklist', (ds) => ds.blacklist, (v) => ({ blacklist: v }))

// 右侧面板展示「最近一次被编辑的那份草稿」的整份预检结果。
const active = ref<'wl' | 'bl'>('wl')
const cur = computed(() => (active.value === 'wl' ? wl : bl))
const flash = ref('')

const cidrOK = (c: string) => /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$|^[0-9a-f:]+\/\d{1,3}$/i.test(c)

function addRule(): void {
  active.value = 'bl'
  bl.draft.value = [...bl.draft.value, { id: `rule-${bl.draft.value.length + 1}`, path_prefix: '/', reason: '' }]
}

function update(i: number, key: 'id' | 'path_prefix' | 'reason', v: string | number | undefined): void {
  active.value = 'bl'
  bl.draft.value = bl.draft.value.map((r, j) => (j === i ? { ...r, [key]: String(v ?? '') } : r))
}

function locate(field: string): void {
  flash.value = field.replace(/\.(id|path_prefix|reason)$/, '')
  void nextTick(() => document.querySelector(`[data-field="${flash.value}"]`)?.scrollIntoView({ behavior: 'smooth', block: 'center' }))
  setTimeout(() => (flash.value = ''), 1600)
}

const groups = [
  { key: 'source_cidrs', title: '来源网段（CIDR）', hint: '内网 / 运维跳板 / 监控出口。例：10.0.0.0/8、192.168.10.0/24', mono: true },
  { key: 'user_agents', title: 'User-Agent（精确匹配）', hint: '健康检查与探针。例：kube-probe/1.29、ELB-HealthChecker/2.0', mono: false },
  { key: 'path_prefixes', title: '路径前缀', hint: '按路径段边界匹配。例：/healthz、/metrics', mono: true },
] as const
</script>

<template>
  <div class="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
    <div class="grid grid-cols-1 gap-4 2xl:grid-cols-2">
      <SpotlightCard :interactive="false" class="p-5" body-class="flex flex-col gap-4" @focusin="active = 'wl'">
        <div>
          <h2 class="flex items-center gap-2 text-base font-medium"><ShieldCheck class="h-4 w-4 text-origin" />白名单 · 免判定直通</h2>
          <p class="text-xs text-muted-foreground">命中任一项即原样放行、连判定都不做 —— 只放内部来源与探针，范围越小越好。</p>
        </div>
        <div v-for="g in groups" :key="g.key" class="flex flex-col gap-1.5" :data-field="`whitelist.${g.key}`" :class="flash.startsWith(`whitelist.${g.key}`) ? 'rounded-lg ring-2 ring-warn/60' : ''">
          <span class="text-sm">{{ g.title }} <span class="text-xs text-muted-foreground">· {{ wl.draft.value[g.key].length }} 条</span></span>
          <TagInput
            v-model="wl.draft.value[g.key]" :mono="g.mono" :disabled="!wl.canWrite.value" :placeholder="g.hint"
            :invalid="g.key === 'source_cidrs' ? (c) => !cidrOK(c) : g.key === 'path_prefixes' ? (p) => !p.startsWith('/') : undefined"
          />
        </div>
        <SaveBar
          :dirty="wl.dirty.value" :errors="wl.errors.value.length" :warnings="wl.warnings.value.length" :saving="wl.saving.value"
          :can-write="wl.canWrite.value" :conflict="wl.conflict.value" :save-error="wl.saveError.value" :saved-at="wl.savedAt.value"
          @save="wl.save" @reset="wl.reset" @keep-mine="wl.keepMineAndRefresh"
        />
      </SpotlightCard>

      <SpotlightCard :interactive="false" class="p-5" body-class="flex flex-col gap-4" @focusin="active = 'bl'">
        <div class="flex flex-wrap items-start justify-between gap-2">
          <div>
            <h2 class="flex items-center gap-2 text-base font-medium"><Ban class="h-4 w-4 text-danger" />黑名单 · 禁止欺骗路径</h2>
            <p class="text-xs text-muted-foreground">命中即绝不改道到幻境（判定照常、可观测，信号 deception_excluded）。用于支付回调等合规路径，原因必填。</p>
          </div>
          <Button v-if="bl.canWrite.value" size="sm" variant="outline" @click="addRule"><Plus class="h-3.5 w-3.5" />新增规则</Button>
        </div>
        <TransitionGroup tag="div" name="rule" class="flex flex-col gap-2">
          <div
            v-for="(r, i) in bl.draft.value"
            :key="i"
            :data-field="`blacklist[${i}]`"
            class="grid grid-cols-[120px_1fr_auto] items-start gap-2 rounded-xl border p-3 transition"
            :class="[bl.issuesAt(`blacklist[${i}]`).length ? 'border-danger/40 bg-danger/5' : 'border-border/60', flash === `blacklist[${i}]` ? 'ring-2 ring-warn/60' : '']"
          >
            <Input :model-value="r.id" class="font-mono text-xs" placeholder="pay-callback" :disabled="!bl.canWrite.value" @update:model-value="(v) => update(i, 'id', v)" />
            <Input :model-value="r.path_prefix" class="font-mono text-xs" placeholder="/pay/callback" :disabled="!bl.canWrite.value" @update:model-value="(v) => update(i, 'path_prefix', v)" />
            <Button v-if="bl.canWrite.value" variant="ghost" size="icon" :aria-label="`删除规则 ${r.id}`" @click="bl.draft.value = bl.draft.value.filter((_, j) => j !== i)">
              <Trash2 class="h-3.5 w-3.5 text-danger" />
            </Button>
            <Input
              :model-value="r.reason" class="col-span-3 text-xs" placeholder="原因（必填）：例如「支付渠道回调，合规要求不做任何欺骗」"
              :disabled="!bl.canWrite.value" @update:model-value="(v) => update(i, 'reason', v)"
            />
            <p v-for="iss in bl.issuesAt(`blacklist[${i}]`)" :key="iss.field + iss.reason" class="col-span-3 text-[11px] text-danger">{{ iss.reason }}</p>
          </div>
        </TransitionGroup>
        <p v-if="!bl.draft.value.length" class="rounded-lg border border-dashed border-border px-3 py-6 text-center text-xs text-muted-foreground">
          暂无禁止欺骗路径。支付回调、SSO 回跳等「绝不能出错」的路径建议登记在这里。
        </p>
        <SaveBar
          :dirty="bl.dirty.value" :errors="bl.errors.value.length" :warnings="bl.warnings.value.length" :saving="bl.saving.value"
          :can-write="bl.canWrite.value" :conflict="bl.conflict.value" :save-error="bl.saveError.value" :saved-at="bl.savedAt.value"
          @save="bl.save" @reset="bl.reset" @keep-mine="bl.keepMineAndRefresh"
        />
      </SpotlightCard>
    </div>
    <ValidationPanel
      :errors="cur.errors.value" :warnings="cur.warnings.value" :validating="cur.validating.value" :dirty="cur.dirty.value"
      :domains="['whitelist', 'blacklist', 'decoys']" @locate="locate"
    />
  </div>
</template>

<style scoped>
.rule-enter-active {
  transition: all 0.25s ease;
}
.rule-enter-from {
  opacity: 0;
  transform: translateY(-6px);
}
</style>
