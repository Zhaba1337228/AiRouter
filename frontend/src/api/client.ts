import axios from 'axios'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8200'

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

// API Keys
export const apiKeys = {
  list: () => client.get('/admin/keys').then((r) => r.data),
  create: (data: {
    name: string
    note?: string
    expires_at?: string
    /** Token limit in millions (0 = unlimited). e.g. 2.5 = 2 500 000 tokens */
    token_limit_m?: number
    /** Max total requests (0 = unlimited) */
    request_limit?: number
  }) => client.post('/admin/keys', data).then((r) => r.data),
  delete: (id: string) => client.delete(`/admin/keys/${id}`).then((r) => r.data),
  toggle: (id: string, is_active: boolean) =>
    client.patch(`/admin/keys/${id}/toggle`, { is_active }).then((r) => r.data),
}

// Stats
export const stats = {
  summary: () => client.get('/admin/stats').then((r) => r.data),
  daily: (days = 7) => client.get(`/admin/stats/daily?days=${days}`).then((r) => r.data),
}

// Logs
export const logs = {
  list: (limit = 50, offset = 0) =>
    client.get(`/admin/logs?limit=${limit}&offset=${offset}`).then((r) => r.data),
}

// Settings
export const settings = {
  get: () => client.get('/admin/settings').then((r) => r.data as Record<string, string>),
  put: (data: Record<string, string>) => client.put('/admin/settings', data).then((r) => r.data),
}
