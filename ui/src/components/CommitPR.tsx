import { useState } from 'react'
import { commitSpecs } from '../api'
import type { CommitResult, ResourceSpec } from '../types'

interface CommitPRProps {
  specs: ResourceSpec[]
}

export default function CommitPR({ specs }: CommitPRProps) {
  const [branch, setBranch] = useState('infra/update')
  const [message, setMessage] = useState('chore: update infrastructure specs')
  const [result, setResult] = useState<CommitResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Shared commit path used by both the initial submit and the idempotent
  // retry-commit-back action. The `loading` guard is shared, so a second
  // invocation (e.g. double-click, or retry while a commit is in flight)
  // short-circuits before issuing a concurrent request.
  async function doCommit() {
    if (loading || !branch.trim() || specs.length === 0) return
    setLoading(true)
    setError(null)
    setResult(null)
    try {
      const res = await commitSpecs({ specs, branch: branch.trim(), message: message.trim() })
      setResult(res)
    } catch (err: unknown) {
      setError(String(err))
    } finally {
      setLoading(false)
    }
  }

  return (
    <section>
      <h2>Commit / PR</h2>
      <p className="section-desc">
        Commit the queued resource specs to a branch and open a pull request.
      </p>

      {specs.length === 0 && (
        <p className="notice">No specs queued. Add specs in the Resources tab first.</p>
      )}

      <div className="commit-form">
        <label>
          Branch
          <input
            type="text"
            value={branch}
            onChange={(e) => setBranch(e.target.value)}
            placeholder="infra/update"
          />
        </label>
        <label>
          Commit message
          <input
            type="text"
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            placeholder="chore: update infrastructure specs"
          />
        </label>
        <button
          className="btn btn-primary"
          onClick={doCommit}
          disabled={loading || !branch.trim() || specs.length === 0}
        >
          {loading ? 'Committing…' : 'Commit & Open PR'}
        </button>
      </div>

      {error && <p className="error">{error}</p>}

      {result && !result.state_diverged && (
        <div className="commit-result">
          <p>
            branch/PR ref: <code>{result.ref}</code>
          </p>
          {/^https?:\/\//.test(result.ref) && (
            <p>
              Open:{' '}
              <a href={result.ref} target="_blank" rel="noopener noreferrer">
                {result.ref}
              </a>
            </p>
          )}
          {!result.committed && (
            <p className="notice">commit-back reported committed=false.</p>
          )}
        </div>
      )}

      {/* 207 / state-diverged: commit-back was interrupted — show explicit retry action */}
      {result?.state_diverged && (
        <div className="state-diverged-banner" role="alert">
          <strong>State diverged</strong> — the commit-back was interrupted before
          completion{result.reason ? ` (${result.reason})` : ''}. The operation is
          idempotent: retrying will reconcile the branch without duplicating changes.
          <div className="state-diverged-actions">
            <button
              className="btn btn-warning"
              onClick={doCommit}
              disabled={loading || specs.length === 0}
            >
              {loading ? 'Retrying…' : 'Retry commit-back'}
            </button>
          </div>
          {result.ref && (
            <p className="state-diverged-branch">
              Partial branch/PR ref: <code>{result.ref}</code>
            </p>
          )}
        </div>
      )}
    </section>
  )
}
