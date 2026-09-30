<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Check } from 'lucide-vue-next'
import Badge from '@/components/ui/badge/Badge.vue'
import { Button } from '@/components/ui/button'
import Input from '@/components/ui/input/Input.vue'
import NativeSelect from '@/components/ui/select/NativeSelect.vue'
import Sheet from '@/components/ui/sheet/Sheet.vue'
import Switch from '@/components/ui/switch/Switch.vue'
import TagInput from './TagInput.vue'
import { clone, decoyKindLabel, honeypotTypeLabel, type DecoyAsset, type DecoyTemplate, type Honeypot } from '@/lib/config'

const open = defineModel<boolean>('open', { default: false })
const props = defineProps<{ initial: DecoyAsset | null; isNew: boolean; templates: DecoyTemplate[]; honeypots: Honeypot[]; takenIds: string[] }>()
const emit = defineEmits<{ submit: [a: DecoyAsset] }>()

const empty = (): DecoyAsset => ({ id: '', kind: 'developer_api', path: '/', hosts: [], content: '', backend: '', enabled: false })
const form = ref<DecoyAsset>(empty())
const step = ref(1)
watch(
  () => [open.value, props.initial] as const,
  ([o]) => {
    if (!o) return
    form.value = props.initial ? clone(props.initial) : empty()
    step.value = props.isNew && !props.initial?.template_id ? 1 : 2
  },
  { immediate: true },
)

function useTemplate(t: DecoyTemplate): void {
  const hp = props.honeypots.find((h) => h.enabled && t.honeypot_types.includes(h.type)) ?? props.honeypots.find((h) => h.enabled)
  form.value = { ...form.value, id: form.value.id || t.id.replace(/^dt-/, ''), kind: t.kind, path: t.path, content: t.content,
    backend: hp?.name ?? '', template_id: t.id }
  step.value = 2
}

const backendOptions = computed(() => [
  { value: '', label: '（未指定 —— 仅停用时允许）' },
  ...props.honeypots.map((h) => ({ value: h.name, label: `${h.name} · ${honeypotTypeLabel[h.type] ?? h.type}${h.enabled ? '' : '（未启用）'}` })),
])
const kindOptions = computed(() => Object.entries(decoyKindLabel).map(([value, label]) => ({ value, label: `${label}（${value}）` })))
const tpl = computed(() => props.templates.find((t) => t.id === form.value.template_id))

const problems = computed(() => {
  const f = form.value
  const out: string[] = []
  if (!/^[a-z0-9][a-z0-9._-]{0,63}$/.test(f.id)) out.push('ID 只允许小写字母、数字与 . _ -')
  if (props.takenIds.includes(f.id)) out.push('ID 已存在')
  if (!f.path.startsWith('/')) out.push('路径必须以 / 开头')
  if (f.path.length > 1 && (f.path.endsWith('/') || f.path.includes('//') || /\/\.\.?(\/|$)/.test(f.path))) out.push('路径需为归一化形态（无尾斜杠、无 // 与 . / ..）')
  if (f.hosts.some((h) => h.trim() === '*')) out.push('禁止裸 * 主机')
  if (f.enabled && !f.hosts.length) out.push('启用中的诱饵必须声明至少一个主机（归属声明 W7）')
  if (f.enabled && !f.backend) out.push('启用中的诱饵必须指定后端蜜罐')
  const hp = props.honeypots.find((h) => h.name === f.backend)
  if (f.enabled && hp && !hp.enabled) out.push(`后端 ${hp.name} 未启用`)
  return out
})

function submit(): void {
  if (problems.value.length) return
  emit('submit', { ...form.value, hosts: form.value.hosts.map((h) => h.trim().toLowerCase()).filter(Boolean) })
  open.value = false
}

const steps = ['选择形态', '投放与落点', '确认']
</script>

<template>
  <Sheet v-model:open="open" width="max-w-2xl" :title="isNew ? '新建诱饵资产' : `编辑诱饵 · ${initial?.id}`" description="诱饵是投放在真实站点上的「钩子」：命中后请求被改道到指定蜜罐（边缘诱饵路由 + 核心判定面同时生效）。">
    <ol class="mb-5 flex items-center gap-2 text-xs">
      <li v-for="(s, i) in steps" :key="s" class="flex items-center gap-2">
        <button
          type="button"
          class="flex cursor-pointer items-center gap-1.5 rounded-full px-2.5 py-1 transition"
          :class="step === i + 1 ? 'bg-primary/15 text-primary' : step > i + 1 ? 'text-origin' : 'text-muted-foreground'"
          @click="step = i + 1"
        >
          <span class="flex h-4 w-4 items-center justify-center rounded-full border text-[10px]" :class="step > i + 1 ? 'border-origin bg-origin/15' : 'border-current'">
            <Check v-if="step > i + 1" class="h-2.5 w-2.5" /><template v-else>{{ i + 1 }}</template>
          </span>{{ s }}
        </button>
        <span v-if="i < steps.length - 1" class="h-px w-6 bg-border" />
      </li>
    </ol>

    <div v-if="step === 1" class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <button
        v-for="t in templates"
        :key="t.id"
        type="button"
        class="flex cursor-pointer flex-col gap-1.5 rounded-xl border border-border/60 p-3.5 text-left transition hover:-translate-y-0.5 hover:border-primary/50 hover:bg-primary/5"
        :class="form.template_id === t.id ? 'glow-ring border-primary/60' : ''"
        @click="useTemplate(t)"
      >
        <span class="flex items-center justify-between gap-2 text-sm font-medium">{{ t.name }}<Badge tone="decoy">{{ decoyKindLabel[t.kind] }}</Badge></span>
        <span class="font-mono text-[11px] text-muted-foreground">{{ t.path }}</span>
        <span class="text-xs leading-relaxed text-muted-foreground">{{ t.description }}</span>
      </button>
      <button type="button" class="cursor-pointer rounded-xl border border-dashed border-border p-3.5 text-sm text-muted-foreground transition hover:border-primary/50 hover:text-foreground" @click="step = 2">
        不用模板，手动填写 →
      </button>
    </div>

    <form v-else-if="step === 2" id="decoy-form" class="flex flex-col gap-4" @submit.prevent="step = 3">
      <div class="grid grid-cols-2 gap-3">
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-muted-foreground">ID *</span>
          <Input v-model="form.id" :disabled="!isNew" class="font-mono" placeholder="dev-api" />
        </label>
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-muted-foreground">形态 *</span>
          <NativeSelect v-model="form.kind" :options="kindOptions" label="诱饵形态" />
        </label>
      </div>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">投放路径 *</span>
        <Input v-model="form.path" class="font-mono" placeholder="/portal/api/content" />
        <span class="text-[11px] text-muted-foreground">按路径段边界匹配：/portal/api 同时覆盖 /portal/api/*，但不覆盖 /portal/apis。</span>
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">归属主机（W7）{{ form.enabled ? ' *' : '' }}</span>
        <TagInput v-model="form.hosts" mono placeholder="shop.example.com、*.example.com —— 回车添加" :invalid="(h) => h.trim() === '*'" />
        <span class="text-[11px] text-muted-foreground">只有 Host 命中这些声明的请求才会被这条诱饵接管；服务绑定会在下发时自动并入该服务的域名。</span>
      </label>
      <div class="grid grid-cols-2 gap-3">
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-muted-foreground">后端蜜罐{{ form.enabled ? ' *' : '' }}</span>
          <NativeSelect v-model="form.backend" :options="backendOptions" label="后端蜜罐" />
        </label>
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-muted-foreground">内容模板标识</span>
          <Input v-model="form.content" class="font-mono" placeholder="site-developer-api" />
        </label>
      </div>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">备注</span>
        <Input v-model="form.note" placeholder="例如：面向爬虫 Agent 的 API 门户诱饵，Q4 轮换" />
      </label>
      <label class="flex items-center justify-between rounded-lg border border-border/60 px-3 py-2.5 text-sm">
        <span class="flex flex-col"><span>启用</span><span class="text-[11px] text-muted-foreground">建议先停用保存观察，再启用（影子 → 接管）。</span></span>
        <Switch v-model="form.enabled" label="启用诱饵" />
      </label>
    </form>

    <div v-else class="flex flex-col gap-3 text-sm">
      <dl class="grid grid-cols-[96px_1fr] gap-x-3 gap-y-2 rounded-xl border border-border/60 p-4">
        <dt class="text-muted-foreground">ID</dt><dd class="font-mono">{{ form.id || '—' }}</dd>
        <dt class="text-muted-foreground">形态</dt><dd>{{ decoyKindLabel[form.kind] ?? form.kind }}</dd>
        <dt class="text-muted-foreground">路径</dt><dd class="font-mono">{{ form.path }}</dd>
        <dt class="text-muted-foreground">主机</dt><dd class="flex flex-wrap gap-1"><Badge v-for="h in form.hosts" :key="h" tone="muted">{{ h }}</Badge><span v-if="!form.hosts.length" class="text-muted-foreground">未声明</span></dd>
        <dt class="text-muted-foreground">落点</dt><dd class="font-mono">{{ form.backend || '—' }}</dd>
        <dt class="text-muted-foreground">模板</dt><dd>{{ tpl?.name ?? '手动' }}</dd>
        <dt class="text-muted-foreground">状态</dt><dd><Badge :tone="form.enabled ? 'origin' : 'muted'" dot>{{ form.enabled ? '启用' : '停用' }}</Badge></dd>
      </dl>
      <ul v-if="problems.length" class="flex flex-col gap-1.5">
        <li v-for="p in problems" :key="p" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{{ p }}</li>
      </ul>
      <p v-else class="rounded-lg border border-origin/30 bg-origin/10 px-3 py-2 text-xs text-origin">本条通过本地检查；与其他诱饵 / 禁止欺骗路径的冲突会在右侧预检面板里给出。</p>
    </div>

    <template #footer>
      <Button v-if="step > 1" variant="ghost" @click="step--">上一步</Button>
      <Button v-if="step === 2" type="submit" form="decoy-form">下一步</Button>
      <Button v-if="step === 3" :disabled="problems.length > 0" @click="submit">{{ isNew ? '加入草稿' : '更新草稿' }}</Button>
    </template>
  </Sheet>
</template>
