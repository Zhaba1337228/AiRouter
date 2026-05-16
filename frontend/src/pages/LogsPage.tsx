import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { logs } from '../api/client'

interface RequestLog {
  id: number
  api_key_prefix: string | null
  model: string | null
  endpoint: string
  method: string
  status_code: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cost_usd: number
  latency_ms: number
  error_message: string | null
  created_at: string
}

const PAGE_SIZE = 50

function formatCost(cost: number): string {
  if (!cost || cost === 0) return '—'
  if (cost < 0.000001) return '<$0.000001'
  return '$' + cost.toFixed(6)
}

export default function LogsPage() {
  const [page, setPage] = useState(0)

  const { data: logList = [], isLoading, isFetching } = useQuery<RequestLog[]>({
    queryKey: ['logs', page],
    queryFn: () => logs.list(PAGE_SIZE, page * PAGE_SIZE),
    refetchInterval: 15000,
  })

  return (
    <div className="page">
      <div className="page-header">
        <h2 className="page-title">Request Logs</h2>
        <span className="badge badge-blue">{isFetching ? 'Refreshing...' : 'Live'}</span>
      </div>

      {isLoading ? (
        <div className="loading">Loading logs...</div>
      ) : (
        <>
          <div className="table-wrap">
            <table className="table table-sm">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Key</th>
                  <th>Model</th>
                  <th>Status</th>
                  <th>In tokens</th>
                  <th>Out tokens</th>
                  <th>Total</th>
                  <th>Cost</th>
                  <th>Latency</th>
                  <th>Error</th>
                </tr>
              </thead>
              <tbody>
                {logList.length === 0 && (
                  <tr>
                    <td colSpan={10} className="empty-row">No logs yet</td>
                  </tr>
                )}
                {logList.map((log) => (
                  <tr key={log.id}>
                    <td className="nowrap">{new Date(log.created_at).toLocaleString()}</td>
                    <td><code>{log.api_key_prefix ?? '—'}</code></td>
                    <td>{log.model ?? '—'}</td>
                    <td>
                      <span className={`badge ${log.status_code < 400 ? 'badge-green' : 'badge-red'}`}>
                        {log.status_code}
                      </span>
                    </td>
                    <td>{log.prompt_tokens > 0 ? log.prompt_tokens.toLocaleString() : '—'}</td>
                    <td>{log.completion_tokens > 0 ? log.completion_tokens.toLocaleString() : '—'}</td>
                    <td>{log.total_tokens > 0 ? log.total_tokens.toLocaleString() : '—'}</td>
                    <td style={{ color: '#f59e0b', fontWeight: 500 }}>{formatCost(log.cost_usd)}</td>
                    <td>{log.latency_ms}ms</td>
                    <td className="error-cell">{log.error_message ?? '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="pagination">
            <button
              className="btn btn-ghost btn-sm"
              disabled={page === 0}
              onClick={() => setPage((p) => Math.max(0, p - 1))}
            >
              ← Prev
            </button>
            <span>Page {page + 1}</span>
            <button
              className="btn btn-ghost btn-sm"
              disabled={logList.length < PAGE_SIZE}
              onClick={() => setPage((p) => p + 1)}
            >
              Next →
            </button>
          </div>
        </>
      )}
    </div>
  )
}
