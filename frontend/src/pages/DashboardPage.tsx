import { useQuery } from '@tanstack/react-query'
import { stats } from '../api/client'
import {
  LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, BarChart, Bar,
} from 'recharts'

export default function DashboardPage() {
  const { data: summary, isLoading: loadingSummary } = useQuery({
    queryKey: ['stats-summary'],
    queryFn: stats.summary,
    refetchInterval: 30000,
  })

  const { data: daily, isLoading: loadingDaily } = useQuery({
    queryKey: ['stats-daily'],
    queryFn: () => stats.daily(14),
    refetchInterval: 60000,
  })

  const cards = [
    { label: 'Total Requests', value: summary?.total_requests ?? '—', color: '#6366f1' },
    { label: 'Success', value: summary?.success_requests ?? '—', color: '#22c55e' },
    { label: 'Errors', value: summary?.error_requests ?? '—', color: '#ef4444' },
    { label: 'Total Tokens', value: summary?.total_tokens ? Number(summary.total_tokens).toLocaleString() : '—', color: '#f59e0b' },
    { label: 'Avg Latency', value: summary?.avg_latency_ms ? `${Math.round(summary.avg_latency_ms)}ms` : '—', color: '#8b5cf6' },
    { label: 'Active Keys', value: summary?.active_keys ?? '—', color: '#06b6d4' },
  ]

  return (
    <div className="page">
      <h2 className="page-title">Dashboard</h2>

      {loadingSummary ? (
        <div className="loading">Loading stats...</div>
      ) : (
        <div className="stats-grid">
          {cards.map((c) => (
            <div className="stat-card" key={c.label} style={{ borderLeft: `4px solid ${c.color}` }}>
              <div className="stat-value" style={{ color: c.color }}>{c.value}</div>
              <div className="stat-label">{c.label}</div>
            </div>
          ))}
        </div>
      )}

      <div className="charts-grid">
        <div className="chart-card">
          <h3>Requests per Day (14d)</h3>
          {loadingDaily ? (
            <div className="loading">Loading chart...</div>
          ) : (
            <ResponsiveContainer width="100%" height={220}>
              <LineChart data={daily ?? []}>
                <CartesianGrid strokeDasharray="3 3" stroke="#2d2d3a" />
                <XAxis dataKey="date" tick={{ fill: '#9ca3af', fontSize: 12 }} />
                <YAxis tick={{ fill: '#9ca3af', fontSize: 12 }} />
                <Tooltip contentStyle={{ background: '#1a1a2e', border: '1px solid #3d3d5c', color: '#e5e7eb' }} />
                <Line type="monotone" dataKey="requests" stroke="#6366f1" strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          )}
        </div>

        <div className="chart-card">
          <h3>Tokens per Day (14d)</h3>
          {loadingDaily ? (
            <div className="loading">Loading chart...</div>
          ) : (
            <ResponsiveContainer width="100%" height={220}>
              <BarChart data={daily ?? []}>
                <CartesianGrid strokeDasharray="3 3" stroke="#2d2d3a" />
                <XAxis dataKey="date" tick={{ fill: '#9ca3af', fontSize: 12 }} />
                <YAxis tick={{ fill: '#9ca3af', fontSize: 12 }} />
                <Tooltip contentStyle={{ background: '#1a1a2e', border: '1px solid #3d3d5c', color: '#e5e7eb' }} />
                <Bar dataKey="tokens" fill="#f59e0b" radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          )}
        </div>
      </div>
    </div>
  )
}
