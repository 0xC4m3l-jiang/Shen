<script setup lang="ts">
import { MotionConfig } from 'motion-v'
</script>

<template>
  <!-- 动效恒开；reduced-motion="user" 只尊重操作系统的「减少动态效果」无障碍设置 -->
  <MotionConfig reduced-motion="user">
    <RouterView />
  </MotionConfig>
</template>
