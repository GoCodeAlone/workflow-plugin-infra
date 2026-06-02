import { useEffect, useState } from 'react'
import { getDrift } from '../api'
import type { DriftResult } from '../types'

interface DriftViewProps {
  provider: string
}

export default function DriftView({ provider }: DriftViewProps) {
  const [result, setResult] = useState<DriftResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

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

  return (
    <section>
      <h2>Drift Detection</h2>
      <p className="section-desc">
        Compare desired infrastructure state against live provider state.
      </p>

      <button className="btn btn-primary" onClick={load} disabled={loading}>
        {loading ? 'Checking…' : 'Refresh'}
      </button>

      {error && <p className="error">{error}</p>}

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
