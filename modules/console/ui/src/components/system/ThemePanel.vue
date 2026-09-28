<script setup lang="ts">
import { Check } from 'lucide-vue-next'
import SpotlightCard from '@/components/fx/SpotlightCard.vue'
import { themeMeta } from '@/lib/format'
import type { ThemeId } from '@/lib/types'
import { usePrefsStore } from '@/stores/prefs'

// 深色 / 亮色两套主题。**点击才切换**并保存到服务端（换设备登录依然生效）；鼠标划过不会改变页面。
const prefs = usePrefsStore()
const ids = Object.keys(themeMeta) as ThemeId[]
</script>

<template>
  <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
    <SpotlightCard
      v-for="id in ids"
      :key="id"
      as="button"
      :beam="prefs.theme === id"
      class="cursor-pointer p-0 text-left"
      :class="prefs.theme === id && 'glow-ring'"
      :aria-pressed="prefs.theme === id"
      @click="prefs.save({ theme: id })"
    >
      <div class="relative h-40 overflow-hidden rounded-t-2xl" :style="{ background: themeMeta[id].swatch[0] }">
        <div
          class="absolute -inset-10 opacity-70 blur-2xl"
          :style="{ background: `radial-gradient(60% 50% at 30% 20%, ${themeMeta[id].swatch[1]}, transparent), radial-gradient(50% 40% at 80% 30%, ${themeMeta[id].swatch[2]}, transparent)` }"
        />
        <div class="absolute inset-x-6 top-1/2 border-t border-dashed" :style="{ borderColor: themeMeta[id].swatch[1] }" />
        <div class="absolute left-6 top-6 flex gap-2">
          <span class="h-2 w-14 rounded-full" :style="{ background: themeMeta[id].swatch[1] }" />
          <span class="h-2 w-8 rounded-full" :style="{ background: themeMeta[id].swatch[2] }" />
        </div>
        <div class="absolute bottom-5 left-6 right-6 flex gap-2">
          <span v-for="c in themeMeta[id].swatch" :key="c" class="h-6 flex-1 rounded-md border border-white/10" :style="{ background: c }" />
        </div>
      </div>
      <div class="flex items-center justify-between p-4">
        <div>
          <div class="font-medium">{{ themeMeta[id].name }}</div>
          <div class="text-xs text-muted-foreground">{{ themeMeta[id].desc }}</div>
        </div>
        <span v-if="prefs.theme === id" class="flex items-center gap-1 text-xs text-primary"><Check class="h-4 w-4" />使用中</span>
        <span v-else class="text-xs text-muted-foreground">点击切换</span>
      </div>
    </SpotlightCard>
  </div>
</template>
