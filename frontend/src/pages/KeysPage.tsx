import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiKeys } from '../api/client'

interface APIKey {
  id: string
  name: string
  key_prefix: string
  is_active: boolean
  created_at: string
  last_used_at: string | null
  expires_at: string | null
  note: string | null
  budget_usd: number
  tokens_used: number
  total_cost_usd: number
}

function BudgetUsage({ spent, budget }: { spent: number; budget: number }) {
  const fmt = (v: number) => v < 0.01 ? `$${v.toFixed(4)}` : `$${v.toFixed(2)}`
  if (budget === 0) {
    return (
      <span style={{ color: 'var(--text3)', fontSize: 13 }}>
        {spent > 0 ? fmt(spent) : '—'}
      </span>
    )
  }
  const pct = Math.min((spent / budget) * 100, 100)
  const cls = pct >= 100 ? 'over' : pct >= 80 ? 'warn' : ''
  return (
    <div className="progress-wrap">
      <div className="progress-bar-bg">
        <div className={`progress-bar-fill ${cls}`} style={{ width: `${pct}%` }} />
      </div>
      <span className={`progress-label ${cls}`}>
        {fmt(spent)} / {fmt(budget)}
      </span>
    </div>
  )
}

export default function KeysPage() {
  const qc = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [newSecret, setNewSecret] = useState<string | null>(null)
  const [form, setForm] = useState({ name: '', note: '', expires_at: '', budget_usd: '' })
  const [copied, setCopied] = useState(false)

  const { data: keys = [], isLoading } = useQuery<APIKey[]>({
    queryKey: ['api-keys'],
    queryFn: apiKeys.list,
  })

  const createMutation = useMutation({
    mutationFn: apiKeys.create,
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: ['api-keys'] })
      setNewSecret(data.secret)
      setShowCreate(false)
      setForm({ name: '', note: '', expires_at: '', budget_usd: '' })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: apiKeys.delete,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['api-keys'] }),
  })

  const toggleMutation = useMutation({
    mutationFn: ({ id, is_active }: { id: string; is_active: boolean }) =>
      apiKeys.toggle(id, is_active),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['api-keys'] }),
  })

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault()
    const payload: { name: string; note?: string; expires_at?: string; budget_usd?: number } = {
      name: form.name,
    }
    if (form.note) payload.note = form.note
    if (form.expires_at) payload.expires_at = new Date(form.expires_at).toISOString()
    if (form.budget_usd) payload.budget_usd = parseFloat(form.budget_usd)
    createMutation.mutate(payload)
  }

  const copyKey = () => {
    if (newSecret) {
      navigator.clipboard.writeText(newSecret)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  const downloadBat = () => {
    if (!newSecret) return
    const apiUrl = import.meta.env.VITE_API_URL || 'http://localhost:8200'
    const content = [
      '@echo off',
      'REM Clear any previous router / Anthropic env vars',
      'set ANTHROPIC_BASE_URL=',
      'set ANTHROPIC_API_KEY=',
      'set ANTHROPIC_AUTH_TOKEN=',
      '',
      'REM Set AiRouter endpoint and key',
      `set "ANTHROPIC_BASE_URL=${apiUrl}"`,
      `set "ANTHROPIC_API_KEY=${newSecret}"`,
      '',
      'echo AiRouter configured: %ANTHROPIC_BASE_URL%',
      'echo.',
      'claude',
    ].join('\r\n')
    const blob = new Blob([content], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'claude-airouter.bat'
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="page">
      <div className="page-header">
        <h2 className="page-title">API Keys</h2>
        <button className="btn btn-primary" onClick={() => setShowCreate(true)}>
          + New Key
        </button>
      </div>

      {newSecret && (
        <div className="secret-banner">
          <p>Save this key — it won't be shown again!</p>
          <div className="secret-row">
            <code className="secret-value">{newSecret}</code>
            <button className="btn btn-sm btn-ghost" onClick={copyKey}>
              {copied ? '✓ Copied' : 'Copy'}
            </button>
          </div>
          <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
            <button className="btn btn-sm btn-primary" onClick={downloadBat} title="Download .bat that sets env vars and launches Claude CLI">
              Download .bat
            </button>
            <button className="btn btn-ghost btn-sm" onClick={() => setNewSecret(null)}>
              Dismiss
            </button>
          </div>
        </div>
      )}

      {showCreate && (
        <div className="modal-overlay" onClick={() => setShowCreate(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h3>Create API Key</h3>
            <form onSubmit={handleCreate}>
              <div className="form-group">
                <label>Name *</label>
                <input
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder="e.g. My App"
                  required
                  autoFocus
                />
              </div>
              <div className="form-group">
                <label>Note</label>
                <input
                  value={form.note}
                  onChange={(e) => setForm({ ...form, note: e.target.value })}
                  placeholder="Optional description"
                />
              </div>
              <div className="form-group">
                <label>Budget, $</label>
                <input
                  type="number"
                  min="0"
                  step="0.01"
                  value={form.budget_usd}
                  onChange={(e) => setForm({ ...form, budget_usd: e.target.value })}
                  placeholder="e.g. 5.00 (0 = unlimited)"
                />
                <span className="form-hint">Max spend in USD. Key stops working when budget is hit.</span>
              </div>
              <div className="form-group">
                <label>Expires At</label>
                <input
                  type="datetime-local"
                  value={form.expires_at}
                  onChange={(e) => setForm({ ...form, expires_at: e.target.value })}
                />
              </div>
              <div className="modal-actions">
                <button type="button" className="btn btn-ghost" onClick={() => setShowCreate(false)}>
                  Cancel
                </button>
                <button type="submit" className="btn btn-primary" disabled={createMutation.isPending}>
                  {createMutation.isPending ? 'Creating...' : 'Create Key'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {isLoading ? (
        <div className="loading">Loading keys...</div>
      ) : (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Key</th>
                <th>Status</th>
                <th>Spent / Budget</th>
                <th>Last used</th>
                <th>Expires</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {keys.length === 0 && (
                <tr>
                  <td colSpan={7} className="empty-row">No API keys yet. Create one!</td>
                </tr>
              )}
              {keys.map((key) => (
                <tr key={key.id}>
                  <td>
                    <div style={{ fontWeight: 600, fontSize: 13.5 }}>{key.name}</div>
                    {key.note && <div style={{ color: 'var(--text2)', fontSize: 12, marginTop: 2 }}>{key.note}</div>}
                  </td>
                  <td><code style={{ fontSize: 12.5, color: 'var(--text3)' }}>{key.key_prefix}</code></td>
                  <td>
                    <span className={`badge ${key.is_active ? 'badge-green' : 'badge-red'}`}>
                      {key.is_active ? 'Active' : 'Disabled'}
                    </span>
                  </td>
                  <td><BudgetUsage spent={key.total_cost_usd} budget={key.budget_usd} /></td>
                  <td style={{ color: 'var(--text3)', fontSize: 13 }}>
                    {key.last_used_at ? new Date(key.last_used_at).toLocaleString() : '—'}
                  </td>
                  <td style={{ color: 'var(--text3)', fontSize: 13 }}>
                    {key.expires_at ? new Date(key.expires_at).toLocaleDateString() : '∞'}
                  </td>
                  <td className="actions-cell">
                    <button
                      className="btn btn-sm btn-ghost"
                      onClick={() => toggleMutation.mutate({ id: key.id, is_active: !key.is_active })}
                    >
                      {key.is_active ? 'Disable' : 'Enable'}
                    </button>
                    <button
                      className="btn btn-sm btn-danger"
                      onClick={() => { if (confirm(`Delete "${key.name}"?`)) deleteMutation.mutate(key.id) }}
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
