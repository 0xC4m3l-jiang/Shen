<script setup lang="ts">
import { computed } from 'vue'
import VChart from 'vue-echarts'
import { baseOption, token } from '@/lib/charts'
import { usePrefsStore } from '@/stores/prefs'

export interface DonutSlice {
  name: string
  value: number
  color: string
}

const props = withDefaults(defineProps<{ slices: DonutSlice[]; center?: string; sub?: string; height?: string }>(), {
  height: '240px',
})
const prefs = usePrefsStore()

const option = computed(() => {
  void prefs.theme
  return {
    ...baseOption(),
    tooltip: { ...baseOption().tooltip, trigger: 'item', formatter: '{b}：{c}（{d}%）' },
    title: {
      text: props.center ?? '',
      subtext: props.sub ?? '',
      left: 'center',
      top: '38%',
      textStyle: { color: token('foreground'), fontSize: 24, fontWeight: 600 },
      subtextStyle: { color: token('muted-foreground'), fontSize: 12 },
    },
    series: [
      {
        type: 'pie',
        radius: ['62%', '82%'],
        padAngle: 2,
        itemStyle: { borderRadius: 6 },
        label: { show: false },
        emphasis: { scale: true, scaleSize: 6 },
        data: props.slices.filter((s) => s.value > 0).map((s) => ({ name: s.name, value: s.value, itemStyle: { color: s.color } })),
      },
    ],
  }
})
</script>

<template>
  <VChart :option="option" :style="{ height }" autoresize />
</template>
