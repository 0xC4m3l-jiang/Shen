<script setup lang="ts">
// 蜃楼母题（取自项目 logo）：一条青色虚线地平线，线上是「幻境之门」，线下是半透明倒影，缓慢起伏。
withDefaults(defineProps<{ height?: number }>(), { height: 220 })
</script>

<template>
  <svg
    class="pointer-events-none w-full"
    :height="height"
    viewBox="0 0 800 220"
    preserveAspectRatio="xMidYMid slice"
    aria-hidden="true"
  >
    <defs>
      <linearGradient id="mh-gate" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stop-color="hsl(var(--primary))" />
        <stop offset="1" stop-color="hsl(var(--accent))" />
      </linearGradient>
      <linearGradient id="mh-fade" x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stop-color="hsl(var(--horizon))" stop-opacity="0" />
        <stop offset=".5" stop-color="hsl(var(--horizon))" stop-opacity=".9" />
        <stop offset="1" stop-color="hsl(var(--horizon))" stop-opacity="0" />
      </linearGradient>
    </defs>
    <g class="animate-drift">
      <path d="M400 30 L470 118 H330 Z" fill="url(#mh-gate)" fill-opacity=".12" stroke="url(#mh-gate)" stroke-width="2.2" stroke-linejoin="round" />
      <circle cx="400" cy="82" r="4" fill="hsl(var(--accent))">
        <animate attributeName="r" values="3;5;3" dur="3s" repeatCount="indefinite" />
      </circle>
      <path
        d="M400 206 L470 124 H330 Z"
        fill="none"
        stroke="hsl(var(--secondary))"
        stroke-opacity=".55"
        stroke-width="1.8"
        stroke-dasharray="6 6"
        stroke-linejoin="round"
      />
    </g>
    <line x1="0" y1="121" x2="800" y2="121" stroke="url(#mh-fade)" stroke-width="1.4" stroke-dasharray="8 6">
      <animate attributeName="stroke-dashoffset" from="0" to="-28" dur="2.4s" repeatCount="indefinite" />
    </line>
    <path d="M120 176 C220 168 300 140 392 96" fill="none" stroke="hsl(var(--foreground))" stroke-opacity=".35" stroke-width="1.4" stroke-dasharray="3 6" />
  </svg>
</template>
