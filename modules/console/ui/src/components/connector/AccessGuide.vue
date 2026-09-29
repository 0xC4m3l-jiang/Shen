<script setup lang="ts">
import { BookOpen, Copy, ServerCog, Terminal, TriangleAlert } from 'lucide-vue-next'
import { ref } from 'vue'
import { Button } from '@/components/ui/button'

// 常驻接入指南：不含密钥（占位符形态）；签发向导的第三步才是带密钥的定制版。
const copied = ref('')
function copy(text: string, tag: string) {
  void navigator.clipboard?.writeText(text).then(() => {
    copied.value = tag
    setTimeout(() => (copied.value = ''), 1600)
  })
}

const sdkCode = `// Go 业务：进程内三行接入
import connector "shen/modules/connector/sdk"

c, err := connector.Connect(ctx, connector.Config{
    Gateway:  "shen-gw.example.com:9446",
    AuthKey:  os.Getenv("SHEN_CONNECTOR_KEY"), // 接入凭证（控制台签发）
    Upstream: "http://127.0.0.1:8080",
    Name:     "shop",
    Hosts:    []string{"shop.example.com"},
})
defer c.Close() // 断线自动重连`

const binaryCode = `# 任意技术栈：独立连接器进程
export SHEN_CONNECTOR_GATEWAY=shen-gw.example.com:9446
export SHEN_CONNECTOR_KEY="<接入凭证>"           # 控制台签发
export SHEN_CONNECTOR_UPSTREAM=http://127.0.0.1:8080
export SHEN_CONNECTOR_NAME=shop
export SHEN_CONNECTOR_HOSTS=shop.example.com
shen-connector`

// 排障表：症状 → 原因 → 处置（运维与开发者各自的救火手册）。
const troubles = [
  { symptom: '连接被拒：接入凭证无效', cause: 'key 错误、已重置或从未签发', fix: '核对 key；重置凭证后用新 key（旧 key 立即作废）' },
  { symptom: '连接被拒：凭证已吊销', cause: '管理员吊销了该凭证', fix: '联系管理员重新签发' },
  { symptom: '域名超出凭证白名单', cause: '连接器声明的域名不在签发时的白名单里', fix: '管理员重签凭证并扩大白名单，或改连接器 Hosts' },
  { symptom: '连接成功但请求 502（local upstream unavailable）', cause: '连接器找不到本地业务（Upstream 配错或业务没起）', fix: '检查 SHEN_CONNECTOR_UPSTREAM 与业务进程状态' },
  { symptom: '请求 502（没有在线的连接器会话）', cause: '凭证在白名单但连接器离线', fix: '看连接器进程与网络；控制台卡片状态应为「离线」' },
  { symptom: '在线后频繁掉线', cause: '网络抖动或心跳（15s）超时', fix: 'SDK 自动指数退避重连；持续掉线查连接器侧出口网络' },
  { symptom: 'TLS 证书错误', cause: '网关地址不对或自签证书未信任', fix: '确认网关地址；自签证书仅限本地开发（TLS_INSECURE=true）' },
]

const envs = [
  ['SHEN_CONNECTOR_GATEWAY', '网关地址（host:port）——必填'],
  ['SHEN_CONNECTOR_KEY', '接入凭证（shc-...）——必填'],
  ['SHEN_CONNECTOR_UPSTREAM', '本地真实业务地址——必填'],
  ['SHEN_CONNECTOR_NAME', '服务名（与凭证一致）——必填'],
  ['SHEN_CONNECTOR_HOSTS', '声明域名（逗号分隔，白名单子集）——必填'],
  ['SHEN_CONNECTOR_TLS_INSECURE', 'true = 跳过证书校验（仅本地开发）'],
]
</script>

<template>
  <details class="glass rounded-2xl">
    <summary class="flex cursor-pointer items-center gap-2 px-5 py-4 text-sm font-medium">
      <BookOpen class="h-4 w-4 text-primary" />接入指南（SDK · 二进制 · 零信任 · 排障）
    </summary>
    <div class="flex flex-col gap-5 border-t border-border/60 px-5 py-5">
      <!-- 两种接入形态 -->
      <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <div class="flex flex-col gap-2">
          <div class="flex items-center justify-between">
            <span class="flex items-center gap-1.5 text-sm font-medium"><ServerCog class="h-4 w-4 text-primary" />Go SDK</span>
            <Button variant="ghost" size="sm" @click="copy(sdkCode, 'sdk')"><Copy class="h-3.5 w-3.5" />{{ copied === 'sdk' ? '已复制' : '复制' }}</Button>
          </div>
          <pre class="overflow-auto rounded-lg border border-border/60 bg-background/60 p-3 font-mono text-xs leading-5">{{ sdkCode }}</pre>
        </div>
        <div class="flex flex-col gap-2">
          <div class="flex items-center justify-between">
            <span class="flex items-center gap-1.5 text-sm font-medium"><Terminal class="h-4 w-4 text-primary" />shen-connector 二进制</span>
            <Button variant="ghost" size="sm" @click="copy(binaryCode, 'bin')"><Copy class="h-3.5 w-3.5" />{{ copied === 'bin' ? '已复制' : '复制' }}</Button>
          </div>
          <pre class="overflow-auto rounded-lg border border-border/60 bg-background/60 p-3 font-mono text-xs leading-5">{{ binaryCode }}</pre>
        </div>
      </div>

      <!-- env 参数表 -->
      <div>
        <h4 class="mb-2 text-sm font-medium">连接器环境变量</h4>
        <dl class="grid grid-cols-[280px_1fr] gap-x-4 gap-y-1.5 text-xs">
          <template v-for="[k, v] in envs" :key="k">
            <dt class="font-mono text-primary">{{ k }}</dt>
            <dd class="text-muted-foreground">{{ v }}</dd>
          </template>
        </dl>
      </div>

      <!-- 零信任说明 -->
      <div>
        <h4 class="mb-2 text-sm font-medium">零信任接入的五个保证</h4>
        <ul class="grid list-disc gap-1 pl-5 text-xs leading-6 text-muted-foreground md:grid-cols-2">
          <li>业务零入站暴露：连接器只向外拨号，防火墙可以拒绝一切入站</li>
          <li>全程 TLS：密钥只在 TLS 通道内的握手中出现一次，控制台只落哈希</li>
          <li>一 key 一服务：凭证绑定服务名与域名白名单，声明超出即拒</li>
          <li>吊销即时：吊销在网关 30 秒拉取周期内生效，存量连接随后被拒</li>
          <li>演进路线：协议已预留 mTLS 双向证书位（v2 双因子）</li>
        </ul>
      </div>

      <!-- 排障表 -->
      <div>
        <h4 class="mb-2 flex items-center gap-1.5 text-sm font-medium"><TriangleAlert class="h-4 w-4 text-warn" />排障：症状 → 原因 → 处置</h4>
        <div class="overflow-x-auto">
          <table class="w-full min-w-[720px] text-xs">
            <thead>
              <tr class="text-left text-[11px] uppercase tracking-wider text-muted-foreground">
                <th class="py-2 pr-4 font-medium">症状</th>
                <th class="py-2 pr-4 font-medium">常见原因</th>
                <th class="py-2 font-medium">处置</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in troubles" :key="row.symptom" class="border-t border-border/50">
                <td class="py-2 pr-4 font-mono">{{ row.symptom }}</td>
                <td class="py-2 pr-4 text-muted-foreground">{{ row.cause }}</td>
                <td class="py-2">{{ row.fix }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </details>
</template>
