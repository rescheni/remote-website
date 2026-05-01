<script setup lang="ts">
import type { ClientInfo } from '../api/index'
defineProps<{ clients: ClientInfo[] }>()
const emit = defineEmits<{ select: [client: ClientInfo] }>()

function fmtBytes(n: number): string {
  if (n < 1024) return n + 'B'
  if (n < 1048576) return (n / 1024).toFixed(1) + 'K'
  return (n / 1048576).toFixed(1) + 'M'
}
</script>

<template>
  <div v-if="clients.length === 0" class="empty">no online devices</div>
  <div v-else class="list">
    <div v-for="c in clients" :key="c.id" class="item">
      <div class="dot" />
      <div class="info">
        <div class="name">{{ c.id }}</div>
        <div class="meta">{{ c.route_count }} routes · last seen {{ c.last_seen }}</div>
      </div>
      <div class="stats">
        <span>{{ c.req_count }} req</span>
        <span>↓{{ fmtBytes(c.bytes_in) }} ↑{{ fmtBytes(c.bytes_out) }}</span>
      </div>
      <button class="btn" @click="emit('select', c)">Routes</button>
    </div>
  </div>
</template>

<style scoped>
.empty { color: #64748b; font-size: 14px; }
.item { display: flex; align-items: center; gap: 12px; padding: 10px 0; border-bottom: 1px solid #334155; }
.item:last-child { border-bottom: none; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: #22c55e; flex-shrink: 0; }
.info { flex: 1; }
.name { font-size: 14px; font-weight: 600; }
.meta { font-size: 12px; color: #64748b; margin-top: 2px; }
.stats { display: flex; gap: 16px; font-size: 12px; color: #94a3b8; }
.btn {
  padding: 4px 12px; background: #334155; color: #e2e8f0; border: none;
  border-radius: 4px; cursor: pointer; font-size: 12px;
}
.btn:hover { background: #475569; }
</style>
