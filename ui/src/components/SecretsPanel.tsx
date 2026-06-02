import { useEffect, useState } from 'react'
import { declareSecret, listSecrets } from '../api'
import type { SecretMeta } from '../types'

const BACKENDS = ['vault', 'env', 'k8s', 'aws-secrets-manager', 'gcp-secret-manager']

export default function SecretsPanel() {
  const [secrets, setSecrets] = useState<SecretMeta[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Declare form state
  const [name, setName] = useState('')
  const [backend, setBackend] = useState(BACKENDS[0])
  const [value, setValue] = useState('')
  const [declaring, setDeclaring] = useState(false)
  const [declareOk, setDeclareOk] = useState(false)
  const [declareError, setDeclareError] = useState<string | null>(null)

  function loadSecrets() {
    setLoading(true)
    setError(null)
    listSecrets()
      .then((res) => setSecrets(res.secrets ?? []))
      .catch((err: unknown) => setError(String(err)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    loadSecrets()
  }, [])

  async function submitDeclare(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim()) return
    setDeclaring(true)
    setDeclareOk(false)
    setDeclareError(null)
    try {
      const input = value
        ? { name: name.trim(), backend, value }
        : { name: name.trim(), backend }
      const res = await declareSecret(input)
      if (res.ok) {
        setDeclareOk(true)
        setName('')
        setValue('')
        loadSecrets()
      } else {
        setDeclareError('Server returned ok=false')
      }
    } catch (err: unknown) {
      setDeclareError(String(err))
    } finally {
      setDeclaring(false)
    }
  }

  return (
    <section>
      <h2>Secrets</h2>
      <p className="section-desc">
        Metadata only — secret values are never displayed. Declare a secret to register it
        with the backend; the value field is write-only.
      </p>

      {loading && <p className="loading">Loading secrets…</p>}
      {error && <p className="error">{error}</p>}

      {!loading && (
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Backend</th>
              <th>Description</th>
            </tr>
          </thead>
          <tbody>
            {secrets.length === 0 ? (
              <tr>
                <td colSpan={3} className="empty">No secrets declared.</td>
              </tr>
            ) : (
              secrets.map((s) => (
                <tr key={`${s.backend}/${s.name}`}>
                  <td>
                    <code>{s.name}</code>
                  </td>
                  <td>{s.backend}</td>
                  <td>{s.description}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      )}

      <section className="declare-section">
        <h3>Declare Secret</h3>
        <p className="section-desc">
          The value field is <strong>write-only</strong> — it is never echoed back or
          displayed after submission.
        </p>
        <form className="declare-form" onSubmit={submitDeclare}>
          <label>
            Name
            <input
              type="text"
              placeholder="MY_SECRET"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </label>
          <label>
            Backend
            <select value={backend} onChange={(e) => setBackend(e.target.value)}>
              {BACKENDS.map((b) => (
                <option key={b} value={b}>{b}</option>
              ))}
            </select>
          </label>
          <label>
            Value <span className="write-only-hint">(write-only, optional)</span>
            <input
              type="password"
              placeholder="••••••••"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              autoComplete="new-password"
            />
          </label>
          <button
            className="btn btn-primary"
            type="submit"
            disabled={declaring || !name.trim()}
          >
            {declaring ? 'Declaring…' : 'Declare'}
          </button>
        </form>

        {declareOk && <p className="declare-ok">Secret declared successfully.</p>}
        {declareError && <p className="error">{declareError}</p>}
      </section>
    </section>
  )
}
