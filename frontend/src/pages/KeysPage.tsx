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
  token_limit: number       // raw token count, 0 = unlimited
  request_limit: number     // total requests, 0 = unlimited
  tokens_used: number       // raw token count
  requests_count: number
  total_cost_usd: number
}

// Format raw token count as human-readable string in millions
function fmtM(raw: number): string {
  if (raw === 0) return '0'
  const m = raw / 1_000_000
  return m < 0.01 ? '<0.01M' : `${m % 1 === 0 ? m.toFixed(0) : m.toFixed(2)}M`
}

function LimitBar({
  used,
  limit,
  fmtUsed,
  fmtLimit,
}: {
  used: number
  limit: number
  fmtUsed: string
  fmtLimit: string
}) {
  if (limit === 0) {
    return (
      <span style={{ color: 'var(--text3)', fontSize: 13 }}>
        {used > 0 ? fmtUsed : '—'}
      </span>
    )
  }
  const pct = Math.min((used / limit) * 100, 100)
  const cls = pct >= 100 ? 'over' : pct >= 80 ? 'warn' : ''
  return (
    <div className="progress-wrap">
      <div className="progress-bar-bg">
        <div className={`progress-bar-fill ${cls}`} style={{ width: `${pct}%` }} />
      </div>
      <span className={`progress-label ${cls}`}>
        {fmtUsed} / {fmtLimit}
      </span>
    </div>
  )
}

export default function KeysPage() {
  const qc = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [showEdit, setShowEdit] = useState(false)
  const [editKey, setEditKey] = useState<APIKey | null>(null)
  const [newSecret, setNewSecret] = useState<string | null>(null)
  const [form, setForm] = useState({
    name: '',
    note: '',
    expires_at: '',
    token_limit_m: '',   // input in millions, e.g. "2.5"
    request_limit: '',   // plain integer
  })
  const [editForm, setEditForm] = useState({
    name: '',
    note: '',
    expires_at: '',
    token_limit_m: '',
    request_limit: '',
    is_active: true,
  })
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
      setForm({ name: '', note: '', expires_at: '', token_limit_m: '', request_limit: '' })
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

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Parameters<typeof apiKeys.update>[1] }) =>
      apiKeys.update(id, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['api-keys'] })
      setShowEdit(false)
      setEditKey(null)
    },
  })

  const openEdit = (key: APIKey) => {
    setEditKey(key)
    setEditForm({
      name: key.name,
      note: key.note ?? '',
      expires_at: key.expires_at ? key.expires_at.slice(0, 16) : '',
      token_limit_m: key.token_limit > 0 ? (key.token_limit / 1_000_000).toString() : '',
      request_limit: key.request_limit > 0 ? key.request_limit.toString() : '',
      is_active: key.is_active,
    })
    setShowEdit(true)
  }

  const handleEdit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!editKey) return
    const data: Parameters<typeof apiKeys.update>[1] = {}
    if (editForm.name !== editKey.name) data.name = editForm.name
    if (editForm.note !== (editKey.note ?? '')) data.note = editForm.note || undefined
    if (editForm.expires_at !== (editKey.expires_at ? editKey.expires_at.slice(0, 16) : '')) {
      data.expires_at = editForm.expires_at ? new Date(editForm.expires_at).toISOString() : ''
    }
    if (editForm.token_limit_m !== '') data.token_limit_m = parseFloat(editForm.token_limit_m)
    if (editForm.request_limit !== '') data.request_limit = parseInt(editForm.request_limit, 10)
    if (editForm.is_active !== editKey.is_active) data.is_active = editForm.is_active
    updateMutation.mutate({ id: editKey.id, data })
  }

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault()
    const payload: Parameters<typeof apiKeys.create>[0] = { name: form.name }
    if (form.note) payload.note = form.note
    if (form.expires_at) payload.expires_at = new Date(form.expires_at).toISOString()
    if (form.token_limit_m) payload.token_limit_m = parseFloat(form.token_limit_m)
    if (form.request_limit) payload.request_limit = parseInt(form.request_limit, 10)
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
      'REM ── Wipe stale registry env vars from old router/claudehub ─────────────',
      'reg delete "HKCU\\Environment" /v ANTHROPIC_BASE_URL   /f >nul 2>&1',
      'reg delete "HKCU\\Environment" /v ANTHROPIC_API_KEY    /f >nul 2>&1',
      'reg delete "HKCU\\Environment" /v ANTHROPIC_AUTH_TOKEN /f >nul 2>&1',
      '',
      'REM ── Clear session vars ──────────────────────────────────────────────────',
      'set ANTHROPIC_BASE_URL=',
      'set ANTHROPIC_API_KEY=',
      'set ANTHROPIC_AUTH_TOKEN=',
      '',
      'REM ── Point to AiRouter ───────────────────────────────────────────────────',
      `set "ANTHROPIC_BASE_URL=${apiUrl}"`,
      `set "ANTHROPIC_API_KEY=${newSecret}"`,
      '',
      'echo AiRouter: %ANTHROPIC_BASE_URL%',
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
                <label>Token limit, M</label>
                <input
                  type="number"
                  min="0"
                  step="0.1"
                  value={form.token_limit_m}
                  onChange={(e) => setForm({ ...form, token_limit_m: e.target.value })}
                  placeholder="e.g. 5 = 5M tokens (0 or empty = unlimited)"
                />
                <span className="form-hint">Max total tokens in millions. Key stops when limit is hit.</span>
              </div>
              <div className="form-group">
                <label>Request limit</label>
                <input
                  type="number"
                  min="0"
                  step="1"
                  value={form.request_limit}
                  onChange={(e) => setForm({ ...form, request_limit: e.target.value })}
                  placeholder="e.g. 1000 (0 or empty = unlimited)"
                />
                <span className="form-hint">Max total API requests. Key stops when limit is hit.</span>
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

      {showEdit && editKey && (
        <div className="modal-overlay" onClick={() => setShowEdit(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h3>Edit API Key</h3>
            <form onSubmit={handleEdit}>
              <div className="form-group">
                <label>Name *</label>
                <input
                  value={editForm.name}
                  onChange={(e) => setEditForm({ ...editForm, name: e.target.value })}
                  placeholder="e.g. My App"
                  required
                  autoFocus
                />
              </div>
              <div className="form-group">
                <label>Note</label>
                <input
                  value={editForm.note}
                  onChange={(e) => setEditForm({ ...editForm, note: e.target.value })}
                  placeholder="Optional description"
                />
              </div>
              <div className="form-group">
                <label>Token limit, M</label>
                <input
                  type="number"
                  min="0"
                  step="0.1"
                  value={editForm.token_limit_m}
                  onChange={(e) => setEditForm({ ...editForm, token_limit_m: e.target.value })}
                  placeholder="e.g. 5 = 5M tokens (0 or empty = unlimited)"
                />
                <span className="form-hint">Max total tokens in millions. Key stops when limit is hit.</span>
              </div>
              <div className="form-group">
                <label>Request limit</label>
                <input
                  type="number"
                  min="0"
                  step="1"
                  value={editForm.request_limit}
                  onChange={(e) => setEditForm({ ...editForm, request_limit: e.target.value })}
                  placeholder="e.g. 1000 (0 or empty = unlimited)"
                />
                <span className="form-hint">Max total API requests. Key stops when limit is hit.</span>
              </div>
              <div className="form-group">
                <label>Expires At</label>
                <input
                  type="datetime-local"
                  value={editForm.expires_at}
                  onChange={(e) => setEditForm({ ...editForm, expires_at: e.target.value })}
                />
                <span className="form-hint">Leave empty to remove expiry (unlimited).</span>
              </div>
              <div className="form-group">
                <label style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <input
                    type="checkbox"
                    checked={editForm.is_active}
                    onChange={(e) => setEditForm({ ...editForm, is_active: e.target.checked })}
                  />
                  Active
                </label>
              </div>
              <div className="modal-actions">
                <button type="button" className="btn btn-ghost" onClick={() => setShowEdit(false)}>
                  Cancel
                </button>
                <button type="submit" className="btn btn-primary" disabled={updateMutation.isPending}>
                  {updateMutation.isPending ? 'Saving...' : 'Save Changes'}
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
                <th>Tokens used</th>
                <th>Requests</th>
                <th>Last used</th>
                <th>Expires</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {keys.length === 0 && (
                <tr>
                  <td colSpan={8} className="empty-row">No API keys yet. Create one!</td>
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
                  <td>
                    <LimitBar
                      used={key.tokens_used}
                      limit={key.token_limit}
                      fmtUsed={fmtM(key.tokens_used)}
                      fmtLimit={fmtM(key.token_limit)}
                    />
                  </td>
                  <td>
                    <LimitBar
                      used={key.requests_count}
                      limit={key.request_limit}
                      fmtUsed={key.requests_count.toLocaleString()}
                      fmtLimit={key.request_limit.toLocaleString()}
                    />
                  </td>
                  <td style={{ color: 'var(--text3)', fontSize: 13 }}>
                    {key.last_used_at ? new Date(key.last_used_at).toLocaleString() : '—'}
                  </td>
                  <td style={{ color: 'var(--text3)', fontSize: 13 }}>
                    {key.expires_at ? new Date(key.expires_at).toLocaleDateString() : '∞'}
                  </td>
                  <td className="actions-cell">
                    <button
                      className="btn btn-sm btn-ghost"
                      onClick={() => openEdit(key)}
                    >
                      Edit
                    </button>
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
