import { defineComponent, h, type VNode } from 'vue'

// 极简 Markdown 渲染：标题 / 列表 / 代码块 / 引用 / 粗体 / 行内代码。
// **只生成 VNode、从不使用 v-html** —— 模型输出可能复述攻击载荷（如 <script>），
// 走 Vue 的文本节点天然被转义，不存在把载荷当成页面标记执行的可能。

function inline(text: string): (VNode | string)[] {
  const out: (VNode | string)[] = []
  const re = /(\*\*[^*]+\*\*|`[^`]+`)/g
  let last = 0
  for (const m of text.matchAll(re)) {
    if (m.index! > last) out.push(text.slice(last, m.index))
    const tok = m[0]
    out.push(
      tok.startsWith('**')
        ? h('strong', { class: 'font-semibold text-foreground' }, tok.slice(2, -2))
        : h('code', { class: 'rounded bg-muted px-1 py-0.5 font-mono text-[12px]' }, tok.slice(1, -1)),
    )
    last = m.index! + tok.length
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

function render(src: string): VNode[] {
  const lines = src.replace(/\r\n/g, '\n').split('\n')
  const nodes: VNode[] = []
  let i = 0
  while (i < lines.length) {
    const line = lines[i]
    if (line.startsWith('```')) {
      const body: string[] = []
      for (i++; i < lines.length && !lines[i].startsWith('```'); i++) body.push(lines[i])
      i++
      nodes.push(h('pre', { class: 'my-2 overflow-x-auto rounded-lg bg-background/70 p-3 font-mono text-[12px] leading-5' }, body.join('\n')))
      continue
    }
    const heading = /^(#{1,4})\s+(.*)$/.exec(line)
    if (heading) {
      const size = heading[1].length <= 2 ? 'text-[15px]' : 'text-sm'
      nodes.push(h('p', { class: `mt-3 mb-1 font-semibold text-foreground ${size}` }, inline(heading[2])))
      i++
      continue
    }
    const listRe = /^\s*([-*]|\d+[.)])\s+(.*)$/
    if (listRe.test(line)) {
      const ordered = /^\s*\d/.test(line)
      const items: VNode[] = []
      for (; i < lines.length && listRe.test(lines[i]); i++) items.push(h('li', inline(listRe.exec(lines[i])![2])))
      nodes.push(h(ordered ? 'ol' : 'ul', { class: `my-1.5 flex flex-col gap-1 pl-5 ${ordered ? 'list-decimal' : 'list-disc'}` }, items))
      continue
    }
    if (line.startsWith('>')) {
      nodes.push(h('blockquote', { class: 'my-1.5 border-l-2 border-primary/50 pl-3 text-muted-foreground' }, inline(line.replace(/^>\s?/, ''))))
      i++
      continue
    }
    if (line.trim() === '') {
      i++
      continue
    }
    const para: string[] = []
    for (; i < lines.length && lines[i].trim() !== '' && !/^(```|#{1,4}\s|>|\s*([-*]|\d+[.)])\s)/.test(lines[i]); i++) para.push(lines[i])
    nodes.push(h('p', { class: 'my-1.5 leading-6' }, inline(para.join('\n'))))
  }
  return nodes
}

export default defineComponent({
  name: 'MarkdownText',
  props: { text: { type: String, required: true } },
  setup(props) {
    return () => h('div', { class: 'whitespace-pre-wrap break-words text-sm' }, render(props.text))
  },
})
