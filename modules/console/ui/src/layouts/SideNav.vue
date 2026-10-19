<script setup lang="ts">
import { ChevronsLeft, ChevronsRight } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { groupedNav } from '@/lib/nav'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const route = useRoute()
const collapsed = ref(false)
// 分组由 nav.ts 决定（顺序即使用顺序）；这里只负责「按权限过滤 + 渲染」。
const groups = computed(() => groupedNav((p) => auth.can(p)))
const isActive = (name: string) => route.name === name || (name === 'services' && route.name === 'service-detail')
</script>

<template>
  <aside
    class="glass fixed bottom-4 left-4 top-20 z-30 flex flex-col gap-1 overflow-y-auto rounded-2xl p-2 transition-[width] duration-300"
    :class="collapsed ? 'w-[60px]' : 'w-56'"
  >
    <nav class="flex flex-1 flex-col" aria-label="功能导航">
      <div v-for="(section, index) in groups" :key="section.group" :class="index > 0 && 'mt-2'">
        <!-- 分组标题：收起时不显示（只留图标，靠 title 提示） -->
        <p
          v-if="!collapsed"
          class="px-3 pb-1 pt-2 text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground/75"
        >
          {{ section.group }}
        </p>
        <!-- 收起时用一条细线代替分组标题，保持分组可感知 -->
        <div v-else-if="index > 0" class="mx-2 mb-1 border-t border-border/60" aria-hidden="true" />
        <div class="flex flex-col gap-1">
          <RouterLink
            v-for="item in section.items"
            :key="item.name"
            :to="{ name: item.name }"
            class="group relative flex items-center gap-3 overflow-hidden rounded-xl px-3 py-2.5 text-sm transition-all duration-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            :class="
              isActive(item.name)
                ? 'bg-primary/10 text-foreground'
                : 'text-muted-foreground hover:bg-primary/5 hover:text-foreground'
            "
            :title="item.hint ?? item.desc"
            :aria-current="isActive(item.name) ? 'page' : undefined"
          >
            <span
              class="absolute inset-y-2 left-0 w-[3px] rounded-full bg-gradient-to-b from-primary to-accent transition-opacity"
              :class="isActive(item.name) ? 'opacity-100 shadow-[0_0_10px_hsl(var(--primary))]' : 'opacity-0'"
            />
            <component
              :is="item.icon"
              class="h-[18px] w-[18px] shrink-0 transition-transform group-hover:scale-110"
              :class="isActive(item.name) && 'text-primary'"
            />
            <span v-if="!collapsed" class="flex min-w-0 flex-col">
              <span class="truncate font-medium">{{ item.label }}</span>
              <span class="truncate text-[11px] text-muted-foreground/90">{{ item.desc }}</span>
            </span>
          </RouterLink>
        </div>
      </div>
    </nav>
    <button
      type="button"
      class="mt-1 flex cursor-pointer items-center justify-center rounded-lg py-2 text-muted-foreground transition hover:bg-primary/10 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      :aria-label="collapsed ? '展开导航' : '收起导航'"
      @click="collapsed = !collapsed"
    >
      <ChevronsRight v-if="collapsed" class="h-4 w-4" />
      <ChevronsLeft v-else class="h-4 w-4" />
    </button>
  </aside>
  <div
    class="shrink-0 transition-[width] duration-300"
    :class="collapsed ? 'w-[76px]' : 'w-60'"
    aria-hidden="true"
  />
</template>
