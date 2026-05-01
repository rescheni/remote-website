<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { fetchClients, fetchStats } from '../api/index'
import type { ClientInfo, StatsInfo } from '../api/index'
import ClientList from '../components/ClientList.vue'
import RouteTable from '../components/RouteTable.vue'

const clients = ref<ClientInfo[]>([])
const stats = ref<StatsInfo>({ total_requests: 0, total_bytes_in: 0, total_bytes_out: 0, online_clients: 0 })
const selectedClient = ref<ClientInfo | null>(null)
let timer: ReturnType<typeof setInterval> | null = null

async function refresh() {
  try {
    const [c, s] = await Promise.all([fetchClients(), fetchStats()])
    clients.value = c
    stats.value = s
    if (selectedClient.value) {
      const found = c.find(x => x.id === selectedClient.value!.id)
      selectedClient.value = found || null
    }
  } catch { /* ignore */ }
}

function selectClient(c: ClientInfo) {
  selectedClient.value = c
}

onMounted(() => { refresh(); timer = setInterval(refresh, 5000) })
onUnmounted(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <div class="dashboard">
    <header class="header">
      <h1>Relay Tunnel</h1>
      <div class="stat-cards">
        <div class="card"><span class="val">{{ stats.online_clients }}</span><span class="lbl">Online</span></div>
        <div class="card"><span class="val">{{ stats.total_requests }}</span><span class="lbl">Requests</span></div>
        <div class="card"><span class="val">{{ fmtBytes(stats.total_bytes_in) }}</span><span class="lbl">In</span></div>
        <div class="card"><span class="val">{{ fmtBytes(stats.total_bytes_out) }}</span><span class="lbl">Out</span></div>
      </div>
    </header>
    <main class="main">
      <section class="panel">
        <h2>Devices</h2>
        <ClientList :clients="clients" @select="selectClient" />
      </section>
      <section class="panel">
        <h2>Route Management</h2>
        <RouteTable :routes="selectedClient?.routes || []" :selected-client="selectedClient" @refresh="refresh" />
      </section>
    </main>
  </div>
</template>

<script lang="ts">
function fmtBytes(n: number): string {
  if (n < 1024) return n + ' B'
  if (n < 1048576) return (n / 1024).toFixed(1) + ' KB'
  return (n / 1048576).toFixed(1) + ' MB'
}
</script>

<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0f172a; color: #e2e8f0; }
.dashboard { max-width: 1200px; margin: 0 auto; padding: 24px; }
.header { margin-bottom: 32px; }
.header h1 { font-size: 28px; margin-bottom: 16px; color: #f8fafc; }
.stat-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 12px; }
.card { background: #1e293b; border-radius: 8px; padding: 16px; display: flex; flex-direction: column; gap: 4px; }
.card .val { font-size: 24px; font-weight: 700; color: #38bdf8; }
.card .lbl { font-size: 12px; color: #94a3b8; text-transform: uppercase; }
.main { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; }
@media (max-width: 768px) { .main { grid-template-columns: 1fr; } }
.panel { background: #1e293b; border-radius: 8px; padding: 20px; }
.panel h2 { font-size: 16px; margin-bottom: 12px; color: #94a3b8; text-transform: uppercase; letter-spacing: 0.05em; }
</style>
