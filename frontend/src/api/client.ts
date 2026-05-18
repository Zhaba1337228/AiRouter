import axios from 'axios'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8200'

// Admin API path — set at build time via VITE_ADMIN_PATH env var.
// Must match the ADMIN_PATH env var on the backend.
const ADMIN_PATH = (import.meta.env.VITE_ADMIN_PATH || 'admin').replace(/^\/+|\/+$/g, '')

export const adminBase = `/${ADMIN_PATH}`

export const client = axios.create({
  baseURL: API_URL,
})

client.interceptors.request.use((config) => {
  const token = localStorage.getItem('admin_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

client.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem('admin_token')
      window.location.href = '/login'
    }
    return Promise.reject(err)
  }
)

const a = (path: string) => `${adminBase}${path}`

// API Keys
export const apiKeys = {
  list: () => client.get(a('/keys')).then((r) => r.data),
  create: (data: {
    name: string
    note?: string
    expires_at?: string
    /** Token limit in millions (0 = unlimited). e.g. 2.5 = 2 500 000 tokens */
    token_limit_m?: number
    /** Max total requests (0 = unlimited) */
    request_limit?: number
  }) => client.post(a('/keys'), data).then((r) => r.data),
  update: (id: string, data: {
    name?: string
    note?: string
    expires_at?: string
    token_limit_m?: number
    request_limit?: number
    is_active?: boolean
  }) => client.patch(a(`/keys/${id}`), data).then((r) => r.data),
  delete: (id: string) => client.delete(a(`/keys/${id}`)).then((r) => r.data),
  toggle: (id: string, is_active: boolean) =>
    client.patch(a(`/keys/${id}/toggle`), { is_active }).then((r) => r.data),
}

// Stats
export const stats = {
  summary: () => client.get(a('/stats')).then((r) => r.data),
  daily: (days = 7) => client.get(a(`/stats/daily?days=${days}`)).then((r) => r.data),
}

// Logs
export const logs = {
  list: (limit = 50, offset = 0) =>
    client.get(a(`/logs?limit=${limit}&offset=${offset}`)).then((r) => r.data),
}

// Settings
export const settings = {
  get: () => client.get(a('/settings')).then((r) => r.data as Record<string, string>),
  put: (data: Record<string, string>) => client.put(a('/settings'), data).then((r) => r.data),
}

// Chat (test panel)
export const chat = {
  send: (data: unknown) => client.post(a('/chat'), data).then((r) => r.data),
  models: () => client.get(a('/models')).then((r) => r.data),
}

// Providers
export interface Provider {
  id: string
  name: string
  base_url: string
  api_key: string
  is_active: boolean
  is_default: boolean
  note: string | null
  created_at: string
  updated_at: string
}

export const providers = {
  list: () => client.get(a('/providers')).then((r) => r.data as Provider[]),
  create: (data: {
    name: string
    base_url: string
    api_key: string
    is_default?: boolean
    note?: string
  }) => client.post(a('/providers'), data).then((r) => r.data as Provider),
  update: (id: string, data: {
    name?: string
    base_url?: string
    api_key?: string
    is_active?: boolean
    is_default?: boolean
    note?: string
  }) => client.patch(a(`/providers/${id}`), data).then((r) => r.data as Provider),
  delete: (id: string) => client.delete(a(`/providers/${id}`)).then((r) => r.data),
}
