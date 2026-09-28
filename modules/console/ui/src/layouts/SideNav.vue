<script setup lang="ts">
import { ChevronsLeft, ChevronsRight } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { navItems } from '@/lib/nav'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const route = useRoute()
const collapsed = ref(false)
const items = computed(() => navItems.filter((i) => auth.can(i.perm)))
const isActive = (name: string) => route.name === name || (name === 'services' && route.name === 'service-detail')
</script>

<template>
  <aside
    class="glass fixed bottom-4 left-4 top-20 z-30 flex flex-col gap-1 rounded-2xl p-2 transition-[width] duration-300"
    :class="collapsed ? 'w-[60px]' : 'w-56'"
  >
    <nav class="flex flex-1 flex-col gap-1" aria-label="功能导航">
      <RouterLink
        v-for="item in items"
        :key="item.name"
        :to="{ name: item.name }"
        class="group relative flex items-center gap-3 overflow-hidden rounded-xl px-3 py-2.5 text-sm transition-all duration-200"
        :class="isActive(item.name) ? 'bg-primary/10 text-foreground' : 'text-muted-foreground hover:bg-primary/5 hover:text-foreground'"
        :title="collapsed ? `${item.label}：${item.desc}` : item.desc"
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
          <span class="truncate text-[11px] text-muted-foreground/80">{{ item.desc }}</span>
        </span>
      </RouterLink>
    </nav>
    <button
      type="button"
      class="flex cursor-pointer items-center justify-center rounded-lg py-2 text-muted-foreground transition hover:bg-primary/10 hover:text-foreground"
      :aria-label="collapsed ? '展开导航' : '收起导航'"
      @click="collapsed = !collapsed"
    >
      <ChevronsRight v-if="collapsed" class="h-4 w-4" />
      <ChevronsLeft v-else class="h-4 w-4" />
    </button>
  </aside>
  <div class="shrink-0 transition-[width] duration-300" :class="collapsed ? 'w-[76px]' : 'w-60'" aria-hidden="true" />
</template>
