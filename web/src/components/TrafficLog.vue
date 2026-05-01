<script setup lang="ts">
defineProps<{ entries?: { time: string; client: string; method: string; path: string; status: number }[] }>()
</script>

<template>
  <div class="empty">实时流量日志将在后续版本中添加</div>
</template>

<style scoped>
.empty { color: #64748b; font-size: 14px; }
</style>
