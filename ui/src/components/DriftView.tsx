import { useEffect, useState } from 'react'
import { getDrift, reconcile } from '../api'
import type { DriftResult, ReconcileResult } from '../types'

interface DriftViewProps {
  provider: string
}

export default function DriftView({ provider }: DriftViewProps) {
  const [result, setResult] = useState<DriftResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [reconcileResult, setReconcileResult] = useState<ReconcileResult | null>(null)
  const [reconciling, setReconciling] = useState(false)
  const [reconcileError, setReconcileError] = useState<string | null>(null)

  function load() {
    setLoading(true)
    setError(null)
    getDrift(provider)
      .then(setResult)
      .catch((err: unknown) => setError(String(err)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
  }, [provider]) // eslint-disable-line react-hooks/exhaustive-deps

  async function runReconcile() {
    setReconciling(true)
    setReconcileError(null)
    setReconcileResult(null)
    try {
      const res = await reconcile(provider)
      setReconcileResult(res)
    } catch (err: unknown) {
      setReconcileError(String(err))
    } finally {
      setReconciling(false)
    }
  }

  return (
    <section>
      <h2>Drift Detection</h2>
      <p className="section-desc">
        Compare desired infrastructure state against live provider state.
      </p>

      <div className="action-row">
        <button className="btn btn-primary" onClick={load} disabled={loading}>
          {loading ? 'Checking…' : 'Refresh'}
        </button>

        {/* Reconcile drift — only offered when provider supports drift and drift exists */}
        {result?.supported && result?.any_drifted && (
          <button
            className="btn btn-secondary"
            onClick={runReconcile}
            disabled={reconciling}
          >
            {reconciling ? 'Reconciling…' : 'Reconcile drift'}
          </button>
        )}
      </div>

      {error && <p className="error">{error}</p>}

      {/* Reconcile result */}
      {reconcileError && <p className="error">{reconcileError}</p>}
      {reconcileResult && (
        <div className="reconcile-result" role="status">
          <div className="reconcile-warning" role="alert">
            <strong>Review required before merge</strong> —{' '}
            {reconcileResult.warning ||
              'reconcile produces an approximate draft; secret refs are not reconstructed.'}{' '}
            Inspect the diff carefully before merging.
          </div>
          <p>
            Draft PR ref: <code>{reconcileResult.ref}</code>
          </p>
          {/^https?:\/\//.test(reconcileResult.ref) && (
            <p>
              Open:{' '}
              <a href={reconcileResult.ref} target="_blank" rel="noopener noreferrer">
                {reconcileResult.ref}
              </a>
            </p>
          )}
          <p className="reconcile-count">
            {reconcileResult.count} change{reconcileResult.count !== 1 ? 's' : ''} drafted
          </p>
        </div>
      )}

      {result && !result.supported && (
        <p className="notice">Drift detection is not supported by this provider.</p>
      )}

      {result?.supported && !result.any_drifted && (
        <p className="no-drift">No drift detected — infrastructure matches desired state.</p>
      )}

      {result?.supported && result.any_drifted && (
        <>
          <p className="drift-count">
            <strong>{result.count}</strong> drift{result.count !== 1 ? 's' : ''} detected
          </p>
          <table>
            <thead>
              <tr>
                <th>Resource</th>
                <th>Type</th>
                <th>Field</th>
                <th>Desired</th>
                <th>Actual</th>
              </tr>
            </thead>
            <tbody>
              {result.drifts.map((d, i) => (
                <tr key={i}>
                  <td>{d.resource}</td>
                  <td>{d.type}</td>
                  <td>
                    <code>{d.field}</code>
                  </td>
                  <td className="drift-desired">
                    <code>{JSON.stringify(d.desired)}</code>
                  </td>
                  <td className="drift-actual">
                    <code>{JSON.stringify(d.actual)}</code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </section>
  )
}
