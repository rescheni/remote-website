const BASE = '/api'

export interface ClientInfo {
  id: string
  connected: string
  last_seen: string
  req_count: number
  bytes_in: number
  bytes_out: number
  routes: RouteInfo[]
  route_count: number
}

export interface RouteInfo {
  client_id: string
  host: string
  path_prefix: string
  target: string
  type: string
  remote_port: number
}

export interface StatsInfo {
  total_requests: number
  total_bytes_in: number
  total_bytes_out: number
  online_clients: number
}

export async function fetchClients(): Promise<ClientInfo[]> {
  const res = await fetch(`${BASE}/clients`)
  return res.json()
}

export async function fetchRoutes(): Promise<RouteInfo[]> {
  const res = await fetch(`${BASE}/routes`)
  return res.json()
}

export async function fetchStats(): Promise<StatsInfo> {
  const res = await fetch(`${BASE}/stats`)
  return res.json()
}

export async function addRoute(clientId: string, route: RouteInfo): Promise<void> {
  await fetch(`${BASE}/routes`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ client_id: clientId, route }),
  })
}

export async function deleteRoute(clientId: string, idx: number): Promise<void> {
  await fetch(`${BASE}/routes?client=${encodeURIComponent(clientId)}&idx=${idx}`, {
    method: 'DELETE',
  })
}
