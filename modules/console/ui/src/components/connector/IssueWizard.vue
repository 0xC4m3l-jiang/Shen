<script setup lang="ts">
import { ArrowRight, Check, Copy, KeyRound, ShieldCheck } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { Button } from '@/components/ui/button'
import Input from '@/components/ui/input/Input.vue'
import Sheet from '@/components/ui/sheet/Sheet.vue'
import { api, ApiError } from '@/lib/api'
import type { ConnectorCredential } from '@/lib/types'

// 签发向导（黄金时刻）：服务信息 → 一次性密钥 → 带真实密钥的接入代码。
// 第三步是整个产品的「零文档上手」点：管理员把整段代码直接转发给开发者。
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ created: [c: ConnectorCredential] }>()

const step = ref(1)
const name = ref('')
const hosts = ref('')
const owner = ref('')
const busy = ref(false)
const error = ref('')
const key = ref('')
const credential = ref<ConnectorCredential | null>(null)
const saved = ref(false)
const copied = ref('')

const parsedHosts = computed(() =>
  hosts.value
    .split(/[\s,，]+/)
    .map((h) => h.trim())
    .filter(Boolean),
)
const canSubmit = computed(() => name.value.trim() !== '' && parsedHosts.value.length > 0)

watch(open, (v) => {
  if (!v) return
  step.value = 1
  name.value = ''
  hosts.value = ''
  owner.value = ''
  error.value = ''
  key.value = ''
  credential.value = null
  saved.value = false
  copied.value = ''
})

async function issue() {
  busy.value = true
  error.value = ''
  await api
    .post<{ credential: ConnectorCredential; key: string }>('/api/v1/connectors/credentials', {
      name: name.value.trim(),
      hosts: parsedHosts.value,
      owner: owner.value.trim(),
    })
    .then((r) => {
      credential.value = r.credential
      key.value = r.key
      step.value = 2
    })
    .catch((err: unknown) => {
      console.error('签发凭证失败', err)
      error.value = err instanceof ApiError ? err.message : '网络错误'
    })
    .finally(() => (busy.value = false))
}

function copy(text: string, tag: string) {
  void navigator.clipboard?.writeText(text).then(() => {
    copied.value = tag
    setTimeout(() => (copied.value = ''), 1600)
  })
}

const sdkCode = computed(() => `// 业务侧接入（Go）：安装依赖后直接运行
import connector "shen/modules/connector/sdk"

c, err := connector.Connect(ctx, connector.Config{
    Gateway:  "shen-gw.example.com:9446", // 换成你的网关地址
    AuthKey:  "${key.value}",             // 接入凭证（仅此一份）
    Upstream: "http://127.0.0.1:8080",    // 本地真实业务地址
    Name:     "${name.value.trim()}",
    Hosts:    ${JSON.stringify(parsedHosts.value)},
})
if err != nil { log.Fatal(err) }
defer c.Close() // 断线自动重连`)

const binaryCode = computed(() => `# 非 Go 业务：独立连接器进程（一个容器/进程即可）
export SHEN_CONNECTOR_GATEWAY=shen-gw.example.com:9446
export SHEN_CONNECTOR_KEY='${key.value}'
export SHEN_CONNECTOR_UPSTREAM=http://127.0.0.1:8080
export SHEN_CONNECTOR_NAME='${name.value.trim()}'
export SHEN_CONNECTOR_HOSTS='${parsedHosts.value.join(',')}'
shen-connector`)

const dockerCode = computed(() => `# docker-compose.yml（业务旁边的 sidecar）
services:
  shen-connector:
    image: shen/connector:latest
    restart: unless-stopped
    network_mode: "service:business" # 与业务同网络命名空间
    environment:
      SHEN_CONNECTOR_GATEWAY: shen-gw.example.com:9446
      SHEN_CONNECTOR_KEY: '${key.value}'
      SHEN_CONNECTOR_UPSTREAM: http://127.0.0.1:8080
      SHEN_CONNECTOR_NAME: '${name.value.trim()}'
      SHEN_CONNECTOR_HOSTS: '${parsedHosts.value.join(',')}'`)
</script>

<template>
  <Sheet v-model:open="open" title="签发接入凭证" :description="step < 3 ? '三步完成：服务信息 → 保存密钥 → 交给开发者' : '接入代码'" width="max-w-2xl">
    <!-- 步骤指示 -->
    <div class="flex items-center gap-2 text-xs">
      <template v-for="(s, i) in ['服务信息', '一次性密钥', '接入代码']" :key="s">
        <span
          class="flex items-center gap-1.5 rounded-full border px-2.5 py-1 transition"
          :class="step > i ? 'border-primary/50 bg-primary/10 text-primary' : step === i + 1 ? 'border-primary text-foreground' : 'border-border text-muted-foreground'"
        >
          <Check v-if="step > i + 1 || (step === 3 && i === 2)" class="h-3 w-3" />{{ i + 1 }}. {{ s }}
        </span>
        <ArrowRight v-if="i < 2" class="h-3 w-3 text-muted-foreground/60" />
      </template>
    </div>

    <!-- 第 1 步：服务信息 -->
    <div v-if="step === 1" class="flex flex-col gap-4">
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">服务名 *</span>
        <Input v-model="name" placeholder="例如：商城（须与凭证一致，连接器不得改名）" maxlength="64" />
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">域名白名单 *</span>
        <Input v-model="hosts" placeholder="shop.example.com, api.example.com（逗号分隔；支持 *.example.com 通配）" />
        <span class="text-xs text-muted-foreground">连接器只能声明这些域名的子集；其他服务已登记的域名会被拒绝。</span>
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-muted-foreground">负责人</span>
        <Input v-model="owner" placeholder="例如：电商组" maxlength="64" />
      </label>
      <p v-if="error" role="alert" class="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{{ error }}</p>
    </div>

    <!-- 第 2 步：一次性密钥 -->
    <div v-else-if="step === 2" class="flex flex-col gap-4">
      <div class="rounded-lg border border-warn/40 bg-warn/10 px-3 py-2.5 text-xs leading-5 text-warn">
        密钥只显示这一次：关闭后控制台只剩哈希，任何人都无法再查看。请立即复制保存。
      </div>
      <div class="glass flex items-center gap-3 rounded-xl p-4">
        <KeyRound class="h-5 w-5 shrink-0 text-primary" />
        <code class="min-w-0 flex-1 break-all font-mono text-sm text-foreground">{{ key }}</code>
        <Button variant="outline" size="sm" @click="copy(key, 'key')">
          <Copy class="h-3.5 w-3.5" />{{ copied === 'key' ? '已复制' : '复制' }}
        </Button>
      </div>
      <label class="flex cursor-pointer items-center gap-2 text-sm">
        <input v-model="saved" type="checkbox" class="h-4 w-4 cursor-pointer accent-[hsl(var(--primary))]" />
        <span>我已安全保存这份密钥（勾选后才能继续）</span>
      </label>
    </div>

    <!-- 第 3 步：接入代码（注入真实密钥） -->
    <div v-else class="flex flex-col gap-4">
      <p class="text-sm text-muted-foreground">下面三段代码已注入 <span class="text-foreground">本服务的真实密钥</span>，可直接整段转发给开发者。</p>
      <div class="flex flex-col gap-3">
        <details v-for="[tag, title, code] in [['sdk', 'Go SDK（进程内接入）', sdkCode], ['bin', 'shen-connector 二进制（任意技术栈）', binaryCode], ['docker', 'Docker Compose（sidecar）', dockerCode]]" :key="tag" class="glass group rounded-xl" open>
          <summary class="flex cursor-pointer items-center justify-between px-4 py-3 text-sm font-medium">
            {{ title }}
            <Button variant="ghost" size="sm" @click.prevent="copy(code, tag)">
              <Copy class="h-3.5 w-3.5" />{{ copied === tag ? '已复制' : '复制代码' }}
            </Button>
          </summary>
          <pre class="max-h-72 overflow-auto border-t border-border/60 px-4 py-3 font-mono text-xs leading-5 text-foreground">{{ code }}</pre>
        </details>
      </div>
      <p class="flex items-center gap-1.5 text-xs text-muted-foreground">
        <ShieldCheck class="h-3.5 w-3.5" />零信任要点：业务零入站暴露 · TLS 通道 · 一 key 一服务 · 吊销 30 秒内生效。
      </p>
    </div>

    <template #footer>
      <Button v-if="step === 1" variant="ghost" @click="open = false">取消</Button>
      <Button v-if="step === 1" :disabled="!canSubmit || busy" :loading="busy" @click="issue">签发并显示密钥</Button>
      <Button v-if="step === 2" variant="ghost" @click="open = false">放弃（密钥作废需吊销重签）</Button>
      <Button v-if="step === 2" :disabled="!saved" @click="step = 3; credential && emit('created', credential)">
        下一步：生成接入代码<ArrowRight class="h-4 w-4" />
      </Button>
      <Button v-if="step === 3" @click="open = false"><Check class="h-4 w-4" />完成</Button>
    </template>
  </Sheet>
</template>
