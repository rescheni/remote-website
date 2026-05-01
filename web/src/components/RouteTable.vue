<script setup lang="ts">
import { ref } from 'vue'
import type { RouteInfo, ClientInfo } from '../api/index'
import { deleteRoute, addRoute } from '../api/index'

const props = defineProps<{ routes: RouteInfo[]; selectedClient: ClientInfo | null }>()
const emit = defineEmits<{ refresh: [] }>()

const showAdd = ref(false)
const newRoute = ref<RouteInfo>({
  client_id: '', host: '', path_prefix: '', target: '', type: 'http', remote_port: 0
})
const error = ref('')

async function handleDelete(clientId: string, idx: number) {
  await deleteRoute(clientId, idx)
  emit('refresh')
}

async function handleAdd() {
  if (!props.selectedClient) return
  error.value = ''
  try {
    await addRoute(props.selectedClient.id, {
      ...newRoute.value,
      client_id: props.selectedClient.id,
    })
    showAdd.value = false
    newRoute.value = { client_id: '', host: '', path_prefix: '', target: '', type: 'http', remote_port: 0 }
    emit('refresh')
  } catch (e) {
    error.value = 'Failed to add route'
  }
}
</script>

<template>
  <div>
    <div v-if="!selectedClient" class="hint">Select a device to manage routes</div>
    <template v-else>
      <div class="header-row">
        <span class="client-name">{{ selectedClient.id }} routes</span>
        <button class="btn-primary" @click="showAdd = !showAdd">{{ showAdd ? 'Cancel' : '+ Add' }}</button>
      </div>

      <div v-if="showAdd" class="add-form">
        <select v-model="newRoute.type" class="field">
          <option value="http">HTTP</option>
          <option value="tcp">TCP</option>
        </select>
        <input v-if="newRoute.type === 'http'" v-model="newRoute.host" class="field" placeholder="Host (e.g. app.example.com)" />
        <input v-if="newRoute.type === 'http'" v-model="newRoute.path_prefix" class="field" placeholder="Path prefix (optional)" />
        <input v-model="newRoute.target" class="field" placeholder="Target (e.g. localhost:3000)" />
        <input v-if="newRoute.type === 'tcp'" v-model.number="newRoute.remote_port" class="field short" placeholder="Port (e.g. 2222)" type="number" />
        <button class="btn-primary" @click="handleAdd">Save</button>
        <span v-if="error" class="err">{{ error }}</span>
      </div>

      <table v-if="selectedClient.routes.length > 0">
        <thead>
          <tr>
            <th>Type</th>
            <th>Host/Port</th>
            <th>Path</th>
            <th>Target</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(r, i) in selectedClient.routes" :key="i">
            <td><span :class="['badge', r.type === 'tcp' ? 'tcp' : 'http']">{{ r.type }}</span></td>
            <td>{{ r.type === 'tcp' ? ':' + r.remote_port : r.host }}</td>
            <td>{{ r.path_prefix || '/' }}</td>
            <td class="target">{{ r.target }}</td>
            <td><button class="btn-del" @click="handleDelete(selectedClient.id, i)">Del</button></td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">No routes. Add one above.</div>
    </template>
  </div>
</template>

<style scoped>
.hint, .empty { color: #64748b; font-size: 14px; }
.header-row { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.client-name { font-weight: 600; font-size: 14px; }
.add-form { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
.field { background: #0f172a; border: 1px solid #334155; color: #e2e8f0; padding: 6px 8px; border-radius: 4px; font-size: 13px; }
.field.short { width: 100px; }
.btn-primary { padding: 6px 14px; background: #2563eb; color: #fff; border: none; border-radius: 4px; cursor: pointer; font-size: 12px; }
.btn-primary:hover { background: #1d4ed8; }
.btn-del { padding: 2px 8px; background: none; color: #ef4444; border: 1px solid #ef4444; border-radius: 4px; cursor: pointer; font-size: 11px; }
.btn-del:hover { background: #ef4444; color: #fff; }
.badge { padding: 2px 6px; border-radius: 3px; font-size: 11px; font-weight: 600; }
.badge.http { background: #065f46; color: #6ee7b7; }
.badge.tcp { background: #1e3a5f; color: #93c5fd; }
.err { color: #ef4444; font-size: 12px; }
table { width: 100%; font-size: 13px; border-collapse: collapse; }
th { text-align: left; padding: 8px 6px; color: #64748b; font-weight: 500; border-bottom: 1px solid #334155; }
td { padding: 8px 6px; border-bottom: 1px solid #1e293b; }
.target { font-family: monospace; color: #38bdf8; }
</style>
