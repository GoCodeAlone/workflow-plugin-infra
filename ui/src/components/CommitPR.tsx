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

  async function submit() {
    if (!branch.trim() || specs.length === 0) return
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
          onClick={submit}
          disabled={loading || !branch.trim() || specs.length === 0}
        >
          {loading ? 'Committing…' : 'Commit & Open PR'}
        </button>
      </div>

      {error && <p className="error">{error}</p>}

      {result && (
        <div className="commit-result">
          <p>
            Branch: <code>{result.branch}</code>
          </p>
          {result.pr_url ? (
            <p>
              PR:{' '}
              <a href={result.pr_url} target="_blank" rel="noopener noreferrer">
                {result.pr_url}
              </a>
            </p>
          ) : (
            <p className="notice">No PR URL returned.</p>
          )}
        </div>
      )}
    </section>
  )
}
