<script setup lang="ts">
import type { RouteInfo } from '../api/index'
defineProps<{ routes: RouteInfo[] }>()
</script>

<template>
  <div v-if="routes.length === 0" class="empty">暂无路由</div>
  <table v-else>
    <thead>
      <tr>
        <th>设备</th>
        <th>域名</th>
        <th>路径前缀</th>
        <th>目标</th>
      </tr>
    </thead>
    <tbody>
      <tr v-for="(r, i) in routes" :key="i">
        <td>{{ r.client_id }}</td>
        <td>{{ r.host }}</td>
        <td>{{ r.path_prefix || '/' }}</td>
        <td class="target">{{ r.target }}</td>
      </tr>
    </tbody>
  </table>
</template>

<style scoped>
.empty { color: #64748b; font-size: 14px; }
table { width: 100%; font-size: 13px; border-collapse: collapse; }
th { text-align: left; padding: 8px 6px; color: #64748b; font-weight: 500; border-bottom: 1px solid #334155; }
td { padding: 8px 6px; border-bottom: 1px solid #1e293b; }
.target { font-family: monospace; color: #38bdf8; }
</style>
