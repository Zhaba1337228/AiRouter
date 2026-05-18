import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { providers, Provider } from '../api/client'

type FormState = {
  name: string
  base_url: string
  api_key: string
  is_default: boolean
  note: string
}

const emptyForm: FormState = {
  name: '',
  base_url: '',
  api_key: '',
  is_default: false,
  note: '',
}

function MaskKey({ value }: { value: string }) {
  const [show, setShow] = useState(false)
  if (!value) return <span style={{ color: 'var(--text3)' }}>—</span>
  return (
    <span style={{ fontFamily: 'monospace', fontSize: 13 }}>
      {show ? value : value.slice(0, 6) + '••••••••••'}
      <button
        onClick={() => setShow(v => !v)}
        style={{ marginLeft: 6, background: 'none', border: 'none', color: 'var(--text3)', cursor: 'pointer', fontSize: 12 }}
      >
        {show ? 'hide' : 'show'}
      </button>
    </span>
  )
}

function ProviderForm({ form, onChange }: { form: FormState; onChange: (f: FormState) => void }) {
  const set = (key: keyof FormState) => (e: React.ChangeEvent<HTMLInputElement>) =>
    onChange({ ...form, [key]: e.target.type === 'checkbox' ? e.target.checked : e.target.value })

  return (
    <>
      <div className="form-group">
        <label>Name *</label>
        <input
          value={form.name}
          onChange={set('name')}
          placeholder="e.g. openlimits.app"
          required
          autoFocus
        />
      </div>
      <div className="form-group">
        <label>Base URL *</label>
        <input
          value={form.base_url}
          onChange={set('base_url')}
          placeholder="https://api.example.com"
          required
        />
      </div>
      <div className="form-group">
        <label>API Key</label>
        <input
          type="password"
          value={form.api_key}
          onChange={set('api_key')}
          placeholder="sk-••••••••"
          autoComplete="new-password"
        />
      </div>
      <div className="form-group">
        <label>Note</label>
        <input
          value={form.note}
          onChange={set('note')}
          placeholder="Optional description"
        />
      </div>
      <div className="form-group" style={{ flexDirection: 'row', alignItems: 'center', gap: 10 }}>
        <input
          type="checkbox"
          id="is_default_chk"
          checked={form.is_default}
          onChange={set('is_default')}
          style={{ width: 16, height: 16, cursor: 'pointer', flexShrink: 0 }}
        />
        <label htmlFor="is_default_chk" style={{ marginBottom: 0, cursor: 'pointer' }}>
          Set as default provider
        </label>
      </div>
    </>
  )
}

export default function ProvidersPage() {
  const qc = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [editProvider, setEditProvider] = useState<Provider | null>(null)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [editForm, setEditForm] = useState<FormState>(emptyForm)
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)

  const { data: list = [], isLoading } = useQuery<Provider[]>({
    queryKey: ['providers'],
    queryFn: providers.list,
  })

  const createMutation = useMutation({
    mutationFn: providers.create,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['providers'] })
      setShowCreate(false)
      setForm(emptyForm)
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Parameters<typeof providers.update>[1] }) =>
      providers.update(id, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['providers'] })
      setEditProvider(null)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: providers.delete,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['providers'] })
      setDeleteConfirm(null)
    },
  })

  const openEdit = (p: Provider) => {
    setEditProvider(p)
    setEditForm({
      name: p.name,
      base_url: p.base_url,
      api_key: p.api_key,
      is_default: p.is_default,
      note: p.note ?? '',
    })
  }

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault()
    createMutation.mutate({
      name: form.name,
      base_url: form.base_url,
      api_key: form.api_key,
      is_default: form.is_default,
      note: form.note || undefined,
    })
  }

  const handleUpdate = (e: React.FormEvent) => {
    e.preventDefault()
    if (!editProvider) return
    updateMutation.mutate({
      id: editProvider.id,
      data: {
        name: editForm.name,
        base_url: editForm.base_url,
        api_key: editForm.api_key,
        is_default: editForm.is_default,
        note: editForm.note || undefined,
      },
    })
  }

  return (
    <div className="page">
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
        <h2 className="page-title" style={{ margin: 0 }}>Providers</h2>
        <button className="btn btn-primary" onClick={() => { setShowCreate(true); setForm(emptyForm) }}>
          + Add Provider
        </button>
      </div>

      <p style={{ color: 'var(--text3)', fontSize: 13, marginBottom: 24 }}>
        Manage upstream API providers. The <strong>default</strong> active provider is used for all proxy requests (takes priority over env vars).
      </p>

      {/* ── Create modal ── */}
      {showCreate && (
        <div className="modal-overlay" onClick={() => setShowCreate(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3>Add Provider</h3>
            <form onSubmit={handleCreate}>
              <ProviderForm form={form} onChange={setForm} />
              {createMutation.isError && (
                <div className="error-msg" style={{ marginTop: 8 }}>
                  {String((createMutation.error as any)?.response?.data?.error ?? createMutation.error)}
                </div>
              )}
              <div className="modal-actions">
                <button type="button" className="btn btn-ghost" onClick={() => setShowCreate(false)}>Cancel</button>
                <button type="submit" className="btn btn-primary" disabled={createMutation.isPending}>
                  {createMutation.isPending ? 'Saving…' : 'Add Provider'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* ── Edit modal ── */}
      {editProvider && (
        <div className="modal-overlay" onClick={() => setEditProvider(null)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3>Edit Provider</h3>
            <form onSubmit={handleUpdate}>
              <ProviderForm form={editForm} onChange={setEditForm} />
              {updateMutation.isError && (
                <div className="error-msg" style={{ marginTop: 8 }}>
                  {String((updateMutation.error as any)?.response?.data?.error ?? updateMutation.error)}
                </div>
              )}
              <div className="modal-actions">
                <button type="button" className="btn btn-ghost" onClick={() => setEditProvider(null)}>Cancel</button>
                <button type="submit" className="btn btn-primary" disabled={updateMutation.isPending}>
                  {updateMutation.isPending ? 'Saving…' : 'Save'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* ── Delete confirm ── */}
      {deleteConfirm && (
        <div className="modal-overlay" onClick={() => setDeleteConfirm(null)}>
          <div className="modal" style={{ maxWidth: 380 }} onClick={e => e.stopPropagation()}>
            <h3>Delete Provider?</h3>
            <p style={{ color: 'var(--text2)', marginBottom: 20 }}>
              This cannot be undone.
            </p>
            <div className="modal-actions">
              <button className="btn btn-ghost" onClick={() => setDeleteConfirm(null)}>Cancel</button>
              <button
                className="btn btn-danger"
                disabled={deleteMutation.isPending}
                onClick={() => deleteMutation.mutate(deleteConfirm)}
              >
                {deleteMutation.isPending ? 'Deleting…' : 'Delete'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ── List ── */}
      {isLoading ? (
        <div className="loading">Loading providers…</div>
      ) : list.length === 0 ? null : (
        <div className="keys-table-wrap">
          <table className="keys-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Base URL</th>
                <th>API Key</th>
                <th>Status</th>
                <th>Note</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {list.map(p => (
                <tr key={p.id} style={{ opacity: p.is_active ? 1 : 0.5 }}>
                  <td>
                    <span style={{ fontWeight: 500 }}>{p.name}</span>
                    {p.is_default && (
                      <span style={{
                        marginLeft: 8,
                        fontSize: 11,
                        background: 'var(--primary)',
                        color: '#fff',
                        borderRadius: 4,
                        padding: '1px 6px',
                      }}>default</span>
                    )}
                  </td>
                  <td style={{ fontFamily: 'monospace', fontSize: 13 }}>{p.base_url}</td>
                  <td><MaskKey value={p.api_key} /></td>
                  <td>
                    <span
                      className={`key-badge ${p.is_active ? 'active' : 'inactive'}`}
                      style={{ cursor: 'pointer' }}
                      onClick={() => updateMutation.mutate({ id: p.id, data: { is_active: !p.is_active } })}
                      title="Click to toggle"
                    >
                      {p.is_active ? 'active' : 'inactive'}
                    </span>
                  </td>
                  <td style={{ color: 'var(--text3)', fontSize: 13 }}>{p.note ?? '—'}</td>
                  <td>
                    <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                      {!p.is_default && (
                        <button
                          className="btn btn-ghost btn-sm"
                          onClick={() => updateMutation.mutate({ id: p.id, data: { is_default: true } })}
                          disabled={updateMutation.isPending}
                        >
                          Set default
                        </button>
                      )}
                      <button className="btn btn-ghost btn-sm" onClick={() => openEdit(p)}>Edit</button>
                      <button className="btn btn-danger btn-sm" onClick={() => setDeleteConfirm(p.id)}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {list.length === 0 && !isLoading && (
        <div style={{ textAlign: 'center', padding: '48px 20px', color: 'var(--text2)', fontSize: 14 }}>
          No providers configured — using <code>UPSTREAM_BASE_URL</code> / <code>UPSTREAM_API_KEY</code> env fallback.
        </div>
      )}
    </div>
  )
}
