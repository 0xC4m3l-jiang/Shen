<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import Input from '@/components/ui/input/Input.vue'
import NativeSelect from '@/components/ui/select/NativeSelect.vue'
import Sheet from '@/components/ui/sheet/Sheet.vue'
import Switch from '@/components/ui/switch/Switch.vue'
import { clone, honeypotTypeLabel, type Honeypot, type HoneypotTemplate } from '@/lib/config'

const open = defineModel<boolean>('open', { default: false })
const props = defineProps<{ initial: Honeypot | null; isNew: boolean; templates: HoneypotTemplate[]; takenNames: string[]; types: string[] }>()
const emit = defineEmits<{ submit: [h: Honeypot] }>()

const form = ref<Honeypot>({ name: '', type: 'web-clone', addr: '', enabled: false })
watch(
  () => [open.value, props.initial] as const,
  ([o]) => {
    if (o) form.value = props.initial ? clone(props.initial) : { name: '', type: 'web-clone', addr: '', enabled: false }
  },
  { immediate: true },
)

const template = computed(() => props.templates.find((t) => t.type === form.value.type))
const typeOptions = computed(() => props.types.map((t) => ({ value: t, label: `${honeypotTypeLabel[t] ?? t}（${t}）` })))

const nameError = computed(() => {
  const n = form.value.name.trim()
  if (!n) return '逻辑名必填'
  if (!/^[a-z0-9][a-z0-9._-]{0,63}$/.test(n)) return '只允许小写字母、数字与 . _ -，且以字母或数字开头'
  if (props.takenNames.includes(n)) return '逻辑名已被其他蜜罐使用'
  return ''
})
const addrError = computed(() => {
  const a = form.value.addr.trim()
  if (!a) return '地址必填'
  if (/^https?:\/\/[^/]+/.test(a)) return ''
  return /^[^\s:/]+:\d{1,5}$|^\[[0-9a-f:]+\]:\d{1,5}$/i.test(a) ? '' : '格式为 host:port 或 http(s)://host:port'
})

function submit(): void {
  if (nameError.value || addrError.value) return
  emit('submit', { ...form.value, name: form.value.name.trim(), addr: form.value.addr.trim() })
  open.value = false
}
</script>

<template>
  <Sheet v-model:open="open" :title="isNew ? '登记蜜罐' : `编辑蜜罐 · ${initial?.name}`" description="蜜罐本体由独立部署提供，这里只登记逻辑名、类型与可拨号地址（ADR-0011）。">
    <form id="hp-form" class="flex flex-col gap-4" @submit.prevent="submit">
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">逻辑名 *</span>
        <Input v-model="form.name" :disabled="!isNew" placeholder="mirage-web" class="font-mono" />
        <span v-if="nameError && form.name" class="text-xs text-danger">{{ nameError }}</span>
        <span v-else-if="!isNew" class="text-[11px] text-muted-foreground">逻辑名被诱饵引用，创建后不可修改（先删后建）。</span>
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">类型 *</span>
        <NativeSelect v-model="form.type" :options="typeOptions" label="蜜罐类型" />
        <span v-if="template" class="rounded-lg bg-primary/5 px-3 py-2 text-xs leading-relaxed text-muted-foreground">{{ template.description }}</span>
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">地址 *</span>
        <Input v-model="form.addr" :placeholder="`10.0.0.9:${template?.default_port ?? 2222}`" class="font-mono" />
        <span v-if="addrError && form.addr" class="text-xs text-danger">{{ addrError }}</span>
        <span v-else class="text-[11px] text-muted-foreground">核心每 15 秒做一次 TCP 探测（不发应用层数据），结果显示在健康列。</span>
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">说明</span>
        <Input v-model="form.description" placeholder="例如：华东机房 · 订单库仿真" />
      </label>
      <label class="flex items-center justify-between rounded-lg border border-border/60 px-3 py-2.5 text-sm">
        <span class="flex flex-col">
          <span>启用</span>
          <span class="text-[11px] text-muted-foreground">未启用的蜜罐不会被解析，引用它的诱饵不能启用。</span>
        </span>
        <Switch v-model="form.enabled" label="启用蜜罐" />
      </label>
    </form>
    <template #footer>
      <Button variant="ghost" @click="open = false">取消</Button>
      <Button type="submit" form="hp-form" :disabled="!!nameError || !!addrError">{{ isNew ? '加入草稿' : '更新草稿' }}</Button>
    </template>
  </Sheet>
</template>
