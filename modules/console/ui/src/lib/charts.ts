// ECharts 按需注册（只打包用到的图表与组件），以及「从 CSS 变量取主题色」。
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart, FunnelChart, LineChart, PieChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TitleComponent, TooltipComponent } from 'echarts/components'
import type { Layer } from './types'

use([CanvasRenderer, LineChart, PieChart, BarChart, FunnelChart, GridComponent, TooltipComponent, LegendComponent, TitleComponent])

/** 读取当前主题的某个令牌（HSL 三元组）并转成 CSS 颜色。 */
export function token(name: string, alpha = 1): string {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(`--${name}`).trim()
  if (!raw) return 'transparent'
  return `hsl(${raw} / ${alpha})`
}

export function layerColors(): Record<Layer, string> {
  return {
    origin: token('origin'),
    fallback: token('warn'),
    mirage: token('mirage'),
    decoy: token('decoy'),
    block: token('danger'),
  }
}

/** 所有图表共用的基础外观（透明背景、主题字色、细网格）。 */
export function baseOption() {
  return {
    backgroundColor: 'transparent',
    textStyle: { color: token('muted-foreground'), fontFamily: 'PingFang SC, system-ui, sans-serif' },
    tooltip: {
      backgroundColor: token('popover', 0.95),
      borderColor: token('primary', 0.35),
      textStyle: { color: token('foreground') },
      extraCssText: 'backdrop-filter: blur(10px); border-radius: 10px;',
    },
    animationDuration: 700,
    animationEasing: 'cubicOut' as const,
  }
}
