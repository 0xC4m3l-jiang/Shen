<script setup lang="ts">
import { Plus, X } from 'lucide-vue-next'
import { ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import Input from '@/components/ui/input/Input.vue'
import Sheet from '@/components/ui/sheet/Sheet.vue'
import Switch from '@/components/ui/switch/Switch.vue'
import { api, ApiError } from '@/lib/api'
import type { Service, ServiceInput } from '@/lib/types'

// 登记 / 编辑一个反向链接 Web 服务。只改管控台的登记表 —— 不下发任何策略。
const open = defineModel<boolean>('open', { default: false })
const props = defineProps<{ service?: Service | null; presetHost?: string }>()
const emit = defineEmits<{ saved: [svc: Service] }>()

const blank = (): ServiceInput => ({ name: '', upstream: '', hosts: [], owner: '', description: '', enabled: true })
const form = ref<ServiceInput>(blank())
const hostDraft = ref('')
const busy = ref(false)
const error = ref('')

watch(open, (v) => {
  if (!v) return
  error.value = ''
  hostDraft.value = ''
  form.value = props.service
    ? { name: props.service.name, upstream: props.service.upstream, hosts: [...props.service.hosts], owner: props.service.owner, description: props.service.description, enabled: props.service.enabled }
    : { ...blank(), hosts: props.presetHost ? [props.presetHost] : [] }
})

function addHost() {
  for (const h of hostDraft.value.split(/[\s,，]+/)) {
    const v = h.trim().toLowerCase()
    if (v && !form.value.hosts.includes(v)) form.value.hosts.push(v)
  }
  hostDraft.value = ''
}

async function save() {
  addHost()
  busy.value = true
  error.value = ''
  const req = props.service
    ? api.put<Service>(`/api/v1/services/${props.service.id}`, { ...form.value, version: props.service.version })
    : api.post<Service>('/api/v1/services', form.value)
  await req
    .then((svc) => {
      emit('saved', svc)
      open.value = false
    })
    .catch((err: unknown) => {
      console.error('保存登记失败', err)
      error.value = err instanceof ApiError ? err.message : '网络错误'
    })
    .finally(() => (busy.value = false))
}
</script>

<template>
  <Sheet
    v-model:open="open"
    :title="service ? `编辑：${service.name}` : '登记反向链接服务'"
    description="每个被引擎保护的 Web 服务作为独立项目登记；按域名把流量归到服务。生效路由仍由核心配置与策略面决定。"
  >
    <form id="svc-form" class="flex flex-col gap-4" @submit.prevent="save">
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">服务名称 *</span>
        <Input v-model="form.name" placeholder="例如：商城主站" maxlength="64" />
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">上游地址 *</span>
        <Input v-model="form.upstream" placeholder="http://10.0.1.20:8080" />
        <span class="text-xs text-muted-foreground">仅用于登记与展示，管控台不会主动连接它。</span>
      </label>
      <div class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">域名 *（精确域名或 *.example.com）</span>
        <div class="flex flex-wrap gap-1.5">
          <span v-for="(h, i) in form.hosts" :key="h" class="inline-flex items-center gap-1 rounded-full border border-primary/30 bg-primary/10 px-2.5 py-0.5 font-mono text-xs text-primary">
            {{ h }}
            <button type="button" class="cursor-pointer hover:text-danger" :aria-label="`移除 ${h}`" @click="form.hosts.splice(i, 1)"><X class="h-3 w-3" /></button>
          </span>
        </div>
        <div class="flex gap-2">
          <Input v-model="hostDraft" placeholder="shop.example.com（回车添加，可用逗号分隔多个）" @keydown.enter.prevent="addHost" />
          <Button variant="outline" size="icon" aria-label="添加域名" @click="addHost"><Plus /></Button>
        </div>
      </div>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">负责人</span>
        <Input v-model="form.owner" placeholder="例如：电商平台组 · 张工" maxlength="64" />
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">描述</span>
        <textarea
          v-model="form.description"
          rows="3"
          maxlength="512"
          class="rounded-md border border-input bg-background/40 px-3 py-2 text-sm outline-none transition hover:border-primary/40 focus:border-primary/70 focus:ring-2 focus:ring-primary/25"
          placeholder="例如：对外商城，含 /admin 历史后台入口；诱饵路由由安全组维护"
        />
      </label>
      <label class="flex items-center justify-between rounded-lg border border-border/60 px-3 py-2.5 text-sm">
        <span class="flex flex-col">
          <span>参与流量归类</span>
          <span class="text-xs text-muted-foreground">停用后该服务的流量显示为「未登记」</span>
        </span>
        <Switch v-model="form.enabled" label="参与流量归类" />
      </label>
      <p v-if="error" role="alert" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{{ error }}</p>
    </form>
    <template #footer>
      <Button variant="ghost" @click="open = false">取消</Button>
      <Button type="submit" form="svc-form" :loading="busy">保存登记</Button>
    </template>
  </Sheet>
</template>
