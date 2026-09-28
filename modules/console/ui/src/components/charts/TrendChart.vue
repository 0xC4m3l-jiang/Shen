<script setup lang="ts">
import { computed } from 'vue'
import VChart from 'vue-echarts'
import { baseOption, layerColors, token } from '@/lib/charts'
import { layerMeta } from '@/lib/format'
import type { Bucket, Layer } from '@/lib/types'
import { usePrefsStore } from '@/stores/prefs'

const props = withDefaults(defineProps<{ buckets: Bucket[]; height?: string }>(), { height: '260px' })
const prefs = usePrefsStore()
const order: Layer[] = ['origin', 'mirage', 'decoy', 'fallback', 'block']

const option = computed(() => {
  void prefs.theme // 主题切换时重新读取颜色
  const colors = layerColors()
  const labels = props.buckets.map((b) =>
    new Date(b.at).toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit' }),
  )
  return {
    ...baseOption(),
    tooltip: { ...baseOption().tooltip, trigger: 'axis' },
    legend: { top: 0, right: 0, icon: 'roundRect', itemWidth: 10, itemHeight: 6, textStyle: { color: token('muted-foreground') } },
    grid: { left: 8, right: 8, top: 36, bottom: 4, containLabel: true },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: labels,
      axisLine: { lineStyle: { color: token('border') } },
      axisLabel: { color: token('muted-foreground'), fontSize: 11 },
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      splitLine: { lineStyle: { color: token('border', 0.5), type: 'dashed' } },
      axisLabel: { color: token('muted-foreground'), fontSize: 11 },
    },
    // 堆叠图里全零的系列会沿「累计总量」画一条线（看起来像是拦截很多）：只画有数据的层。
    series: order.filter((layer) => props.buckets.some((b) => (b.by_layer[layer] ?? 0) > 0)).map((layer) => ({
      name: layerMeta[layer].label,
      type: 'line',
      stack: 'total',
      smooth: 0.35,
      showSymbol: false,
      lineStyle: { width: 1.6, color: colors[layer] },
      itemStyle: { color: colors[layer] },
      areaStyle: {
        color: {
          type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: colors[layer].replace('/ 1)', '/ 0.45)') },
            { offset: 1, color: colors[layer].replace('/ 1)', '/ 0.02)') },
          ],
        },
      },
      emphasis: { focus: 'series' },
      data: props.buckets.map((b) => b.by_layer[layer] ?? 0),
    })),
  }
})
</script>

<template>
  <VChart :option="option" :style="{ height }" autoresize />
</template>
