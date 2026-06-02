import { useState } from 'react'
import { applyResources, planResources } from '../api'
import type { PlanAction, PlanResult, ResourceSpec } from '../types'

interface PlanPreviewProps {
  provider: string
  specs: ResourceSpec[]
  onPlanReady?: (result: PlanResult) => void
}

const ACTION_CLASS: Record<string, string> = {
  create: 'action-create',
  update: 'action-update',
  delete: 'action-delete',
  'no-op': 'action-noop',
}

export default function PlanPreview({ provider, specs, onPlanReady }: PlanPreviewProps) {
  const [plan, setPlan] = useState<PlanResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [applying, setApplying] = useState(false)
  const [applyResult, setApplyResult] = useState<{ applied: boolean } | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function runPlan() {
    setLoading(true)
    setError(null)
    setPlan(null)
    setApplyResult(null)
    try {
      const result = await planResources(provider, specs)
      setPlan(result)
      onPlanReady?.(result)
    } catch (err: unknown) {
      setError(String(err))
    } finally {
      setLoading(false)
    }
  }

  async function runApply() {
    if (!plan) return
    setApplying(true)
    setError(null)
    try {
      const result = await applyResources(provider, specs, plan.desired_hash)
      setApplyResult({ applied: result.applied })
    } catch (err: unknown) {
      setError(String(err))
    } finally {
      setApplying(false)
    }
  }

  return (
    <section>
      <h2>Plan Preview</h2>
      <p className="section-desc">
        Preview changes before applying. The desired_hash pins the plan to prevent drift
        between plan and apply.
      </p>

      <div className="action-row">
        <button
          className="btn btn-primary"
          onClick={runPlan}
          disabled={loading || specs.length === 0}
        >
          {loading ? 'Planning…' : 'Run Plan'}
        </button>
        {specs.length === 0 && (
          <span className="hint">Add specs in the Resources tab first.</span>
        )}
      </div>

      {error && <p className="error">{error}</p>}

      {plan && (
        <div className="plan-result">
          <div className="plan-meta">
            <span>
              <strong>{plan.plan.actions.length}</strong> action
              {plan.plan.actions.length !== 1 ? 's' : ''}
            </span>
            <code className="hash-badge" title="desired_hash">
              {plan.desired_hash}
            </code>
          </div>

          {plan.plan.actions.length === 0 ? (
            <p className="notice">No changes — infrastructure is up to date.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Action</th>
                  <th>Resource</th>
                  <th>Type</th>
                  <th>Changes</th>
                </tr>
              </thead>
              <tbody>
                {plan.plan.actions.map((a: PlanAction, i) => (
                  <tr key={i}>
                    <td>
                      <span className={`action-badge ${ACTION_CLASS[a.action] ?? ''}`}>
                        {a.action}
                      </span>
                    </td>
                    <td>{a.resource}</td>
                    <td>{a.type}</td>
                    <td>
                      {a.diff
                        ? Object.entries(a.diff).map(([field, v]) => (
                            <div key={field} className="diff-line">
                              <code>{field}</code>
                              <span className="diff-old">{JSON.stringify(v.old)}</span>
                              {' → '}
                              <span className="diff-new">{JSON.stringify(v.new)}</span>
                            </div>
                          ))
                        : '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          <div className="apply-row">
            <button
              className="btn btn-primary"
              onClick={runApply}
              disabled={applying || plan.plan.actions.length === 0}
            >
              {applying ? 'Applying…' : 'Apply'}
            </button>
            {applyResult && (
              <span className={applyResult.applied ? 'apply-ok' : 'apply-fail'}>
                {applyResult.applied ? 'Applied successfully' : 'Apply returned false'}
              </span>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
