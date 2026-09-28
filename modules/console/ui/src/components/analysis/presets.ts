// 常见 OpenAI 兼容服务商的预设（只是填表便利：地址与模型名以服务商文档为准，可随时修改）。
export interface ProviderPreset {
  id: string
  name: string
  base_url: string
  models: string[]
  keyUrl?: string
  note: string
}

export const providerPresets: ProviderPreset[] = [
  {
    id: 'deepseek',
    name: 'DeepSeek',
    base_url: 'https://api.deepseek.com',
    models: ['deepseek-chat', 'deepseek-reasoner'],
    keyUrl: 'https://platform.deepseek.com/api_keys',
    note: 'deepseek-chat 适合快速研判；deepseek-reasoner 推理更深但更慢、更贵',
  },
  {
    id: 'openai',
    name: 'OpenAI',
    base_url: 'https://api.openai.com/v1',
    models: ['gpt-4o-mini', 'gpt-4o'],
    keyUrl: 'https://platform.openai.com/api-keys',
    note: '需要能访问 api.openai.com 的出站网络',
  },
  {
    id: 'qwen',
    name: '通义千问（兼容模式）',
    base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    models: ['qwen-plus', 'qwen-max'],
    keyUrl: 'https://bailian.console.aliyun.com/',
    note: '阿里云百炼的 OpenAI 兼容端点',
  },
  {
    id: 'moonshot',
    name: 'Kimi（Moonshot）',
    base_url: 'https://api.moonshot.cn/v1',
    models: ['moonshot-v1-32k', 'moonshot-v1-8k'],
    keyUrl: 'https://platform.moonshot.cn/console/api-keys',
    note: '长上下文，适合一次分析较多流量',
  },
  {
    id: 'ollama',
    name: '本机 Ollama',
    base_url: 'http://127.0.0.1:11434/v1',
    models: ['qwen2.5:7b'],
    note: '数据不出本机；容器内访问宿主请用 http://host.docker.internal:11434/v1，API Key 随便填（如 ollama-local）',
  },
]
