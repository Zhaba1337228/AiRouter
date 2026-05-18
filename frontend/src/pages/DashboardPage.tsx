import { useQuery } from '@tanstack/react-query'
import { stats } from '../api/client'
import {
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, BarChart, Bar,
  AreaChart, Area,
} from 'recharts'

interface StatsResponse {
  total_requests: number
  success_requests: number
  error_requests: number
  total_tokens: number
  total_cost_usd: number
  avg_latency_ms: number
  active_keys: number
  success_rate: number
  error_rate: number
  avg_tokens_per_request: number
  today_requests: number
  today_tokens: number
  today_cost_usd: number
  hourly_requests: number[]
  hourly_tokens: number[]
  hourly_cost_usd: number[]
}

function MetricCard({ label, value, sub, color }: { label: string; value: string; sub?: string; color: string }) {
  return (
    <div className="stat-card" style={{ borderLeft: `4px solid ${color}` }}>
      <div className="stat-value" style={{ color }}>{value}</div>
      <div className="stat-label">{label}</div>
      {sub && <div style={{ fontSize: 11, color: 'var(--text3)', marginTop: 2 }}>{sub}</div>}
    </div>
  )
}

function makeHourlyData(hourly: number[], label: string) {
  const now = new Date()
  return hourly.map((v, i) => {
    const d = new Date(now)
    d.setHours(i, 0, 0, 0)
    const labelStr = i === 0 ? '00:00' : i < 10 ? `0${i}:00` : `${i}:00`
    return { hour: labelStr, [label]: v }
  })
}

export default function DashboardPage() {
  const { data: summary, isLoading } = useQuery<StatsResponse>({
    queryKey: ['stats-summary'],
    queryFn: stats.summary,
    refetchInterval: 30000,
  })

  const { data: daily } = useQuery({
    queryKey: ['stats-daily'],
    queryFn: () => stats.daily(14),
    refetchInterval: 60000,
  })

  const fmtM = (n: number) => {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
    if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
    return String(n)
  }

  const hourlyData = summary
    ? makeHourlyData(summary.hourly_requests, 'requests')
    : []

  if (isLoading) {
    return <div className="page"><div className="loading">Loading stats...</div></div>
  }

  const s = summary

  return (
    <div className="page">
      <h2 className="page-title">Dashboard</h2>

      {/* ── Primary KPIs ── */}
      <div className="stats-grid" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))' }}>
        <MetricCard
          label="Total Requests"
          value={fmtM(s?.total_requests ?? 0)}
          color="#6366f1"
        />
        <MetricCard
          label="Success Rate"
          value={s?.success_rate ? `${s.success_rate.toFixed(1)}%` : '—'}
          sub={s ? `${s.success_requests.toLocaleString()} ok · ${s.error_requests.toLocaleString()} errors` : ''}
          color="#22c55e"
        />
        <MetricCard
          label="Today Requests"
          value={fmtM(s?.today_requests ?? 0)}
          sub={s?.today_cost_usd ? `$${s.today_cost_usd.toFixed(4)} today` : ''}
          color="#3b82f6"
        />
        <MetricCard
          label="Avg Latency"
          value={s?.avg_latency_ms ? `${Math.round(s.avg_latency_ms)}ms` : '—'}
          color="#8b5cf6"
        />
        <MetricCard
          label="Total Cost"
          value={s?.total_cost_usd ? `$${Number(s.total_cost_usd).toFixed(4)}` : '—'}
          color="#10b981"
        />
        <MetricCard
          label="Avg Tokens / Req"
          value={s?.avg_tokens_per_request ? fmtM(Math.round(s.avg_tokens_per_request)) : '—'}
          sub={s?.total_tokens ? `${fmtM(s.total_tokens)} total tokens` : ''}
          color="#f59e0b"
        />
        <MetricCard
          label="Today Tokens"
          value={fmtM(s?.today_tokens ?? 0)}
          color="#06b6d4"
        />
        <MetricCard
          label="Active Keys"
          value={String(s?.active_keys ?? '—')}
          color="#ec4899"
        />
      </div>

      {/* ── Hourly activity (last 24h) ── */}
      <div style={{ marginTop: 28 }}>
        <h3 style={{ margin: '0 0 14px', fontSize: 15, color: 'var(--text2)' }}>Requests — Last 24 Hours</h3>
        <div className="chart-card">
          {hourlyData.length === 0 ? (
            <div className="loading">No data</div>
          ) : (
            <ResponsiveContainer width="100%" height={180}>
              <AreaChart data={hourlyData}>
                <defs>
                  <linearGradient id="reqGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#6366f1" stopOpacity={0.3}/>
                    <stop offset="95%" stopColor="#6366f1" stopOpacity={0}/>
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#2d2d3a" />
                <XAxis dataKey="hour" tick={{ fill: '#9ca3af', fontSize: 11 }} interval={3} />
                <YAxis tick={{ fill: '#9ca3af', fontSize: 11 }} />
                <Tooltip contentStyle={{ background: '#1a1a2e', border: '1px solid #3d3d5c', color: '#e5e7eb', fontSize: 12 }} />
                <Area type="monotone" dataKey="requests" stroke="#6366f1" fill="url(#reqGrad)" strokeWidth={2} />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </div>
      </div>

      {/* ── Daily charts ── */}
      <div className="charts-grid" style={{ marginTop: 20 }}>
        <div className="chart-card">
          <h3>Requests per Day (14d)</h3>
          {(!daily || daily.length === 0) ? (
            <div className="loading">No data</div>
          ) : (
            <ResponsiveContainer width="100%" height={200}>
              <BarChart data={daily}>
                <CartesianGrid strokeDasharray="3 3" stroke="#2d2d3a" />
                <XAxis dataKey="date" tick={{ fill: '#9ca3af', fontSize: 11 }} />
                <YAxis tick={{ fill: '#9ca3af', fontSize: 11 }} />
                <Tooltip contentStyle={{ background: '#1a1a2e', border: '1px solid #3d3d5c', color: '#e5e7eb', fontSize: 12 }} />
                <Bar dataKey="requests" fill="#6366f1" radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          )}
        </div>

        <div className="chart-card">
          <h3>Tokens per Day (14d)</h3>
          {(!daily || daily.length === 0) ? (
            <div className="loading">No data</div>
          ) : (
            <ResponsiveContainer width="100%" height={200}>
              <BarChart data={daily}>
                <CartesianGrid strokeDasharray="3 3" stroke="#2d2d3a" />
                <XAxis dataKey="date" tick={{ fill: '#9ca3af', fontSize: 11 }} />
                <YAxis tick={{ fill: '#9ca3af', fontSize: 11 }} tickFormatter={(v) => fmtM(Number(v))} />
                <Tooltip contentStyle={{ background: '#1a1a2e', border: '1px solid #3d3d5c', color: '#e5e7eb', fontSize: 12 }} formatter={(v) => [fmtM(Number(v)), 'Tokens']} />
                <Bar dataKey="tokens" fill="#f59e0b" radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          )}
        </div>

        <div className="chart-card">
          <h3>Cost per Day, $ (14d)</h3>
          {(!daily || daily.length === 0) ? (
            <div className="loading">No data</div>
          ) : (
            <ResponsiveContainer width="100%" height={200}>
              <BarChart data={daily}>
                <CartesianGrid strokeDasharray="3 3" stroke="#2d2d3a" />
                <XAxis dataKey="date" tick={{ fill: '#9ca3af', fontSize: 11 }} />
                <YAxis tick={{ fill: '#9ca3af', fontSize: 11 }} tickFormatter={(v) => `$${Number(v).toFixed(4)}`} />
                <Tooltip contentStyle={{ background: '#1a1a2e', border: '1px solid #3d3d5c', color: '#e5e7eb', fontSize: 12 }} formatter={(v) => [`$${Number(v).toFixed(6)}`, 'Cost']} />
                <Bar dataKey="cost_usd" fill="#10b981" radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          )}
        </div>
      </div>
    </div>
  )
}
