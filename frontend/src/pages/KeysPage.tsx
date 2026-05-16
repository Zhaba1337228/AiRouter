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
}

export default function KeysPage() {
  const qc = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [newSecret, setNewSecret] = useState<string | null>(null)
  const [form, setForm] = useState({ name: '', note: '', expires_at: '' })
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
      setForm({ name: '', note: '', expires_at: '' })
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
    const payload: { name: string; note?: string; expires_at?: string } = { name: form.name }
    if (form.note) payload.note = form.note
    if (form.expires_at) payload.expires_at = new Date(form.expires_at).toISOString()
    createMutation.mutate(payload)
  }

  const copyKey = () => {
    if (newSecret) {
      navigator.clipboard.writeText(newSecret)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
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
          <p><strong>Save this key — it won't be shown again!</strong></p>
          <div className="secret-row">
            <code className="secret-value">{newSecret}</code>
            <button className="btn btn-sm" onClick={copyKey}>
              {copied ? 'Copied!' : 'Copy'}
            </button>
          </div>
          <button className="btn btn-ghost btn-sm" onClick={() => setNewSecret(null)}>
            Dismiss
          </button>
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
                />
              </div>
              <div className="form-group">
                <label>Note</label>
                <input
                  value={form.note}
                  onChange={(e) => setForm({ ...form, note: e.target.value })}
                  placeholder="Optional note"
                />
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
                  {createMutation.isPending ? 'Creating...' : 'Create'}
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
                <th>Key Prefix</th>
                <th>Status</th>
                <th>Created</th>
                <th>Last Used</th>
                <th>Expires</th>
                <th>Note</th>
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
                  <td>{key.name}</td>
                  <td><code>{key.key_prefix}</code></td>
                  <td>
                    <span className={`badge ${key.is_active ? 'badge-green' : 'badge-red'}`}>
                      {key.is_active ? 'Active' : 'Disabled'}
                    </span>
                  </td>
                  <td>{new Date(key.created_at).toLocaleDateString()}</td>
                  <td>{key.last_used_at ? new Date(key.last_used_at).toLocaleString() : '—'}</td>
                  <td>{key.expires_at ? new Date(key.expires_at).toLocaleDateString() : '∞'}</td>
                  <td>{key.note ?? '—'}</td>
                  <td className="actions-cell">
                    <button
                      className="btn btn-sm btn-ghost"
                      onClick={() => toggleMutation.mutate({ id: key.id, is_active: !key.is_active })}
                    >
                      {key.is_active ? 'Disable' : 'Enable'}
                    </button>
                    <button
                      className="btn btn-sm btn-danger"
                      onClick={() => {
                        if (confirm(`Delete key "${key.name}"?`)) deleteMutation.mutate(key.id)
                      }}
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
