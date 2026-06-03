import { useEffect, useState } from 'react'
import { applyResources, getExecEnvs, planResources } from '../api'
import type { ApplyResult, PlanAction, PlanResult, ResourceSpec } from '../types'

interface PlanPreviewProps {
  provider: string
  specs: ResourceSpec[]
  onPlanReady?: (result: PlanResult) => void
  onRequestCommitTab?: () => void
}

const ACTION_CLASS: Record<string, string> = {
  create: 'action-create',
  update: 'action-update',
  delete: 'action-delete',
  'no-op': 'action-noop',
}

export default function PlanPreview({ provider, specs, onPlanReady, onRequestCommitTab }: PlanPreviewProps) {
  const [plan, setPlan] = useState<PlanResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [applying, setApplying] = useState(false)
  const [applyResult, setApplyResult] = useState<ApplyResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Exec-env picker state
  const [execEnvs, setExecEnvs] = useState<string[]>([])
  const [execEnv, setExecEnv] = useState<string>('')
  const [execEnvsLoading, setExecEnvsLoading] = useState(false)

  useEffect(() => {
    setExecEnvsLoading(true)
    getExecEnvs()
      .then((res) => {
        const envs = res.exec_envs ?? []
        setExecEnvs(envs)
        if (envs.length > 0 && !execEnv) {
          setExecEnv(envs[0])
        }
      })
      .catch(() => {
        // exec-envs endpoint may not be wired yet in older engine versions;
        // treat gracefully — exec_env stays empty (engine uses default)
        setExecEnvs([])
      })
      .finally(() => setExecEnvsLoading(false))
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  async function runPlan() {
    setLoading(true)
    setError(null)
    setPlan(null)
    setApplyResult(null)
    try {
      const result = await planResources(provider, specs, execEnv || undefined)
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
      const result = await applyResources(provider, specs, plan.desired_hash, execEnv || undefined)
      setApplyResult(result)
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

      {/* ── Exec-env picker ──────────────────────────────────────────────────── */}
      <div className="filter-row">
        <label>
          Execution environment
          {execEnvsLoading ? (
            <select disabled><option>Loading…</option></select>
          ) : execEnvs.length > 0 ? (
            <select value={execEnv} onChange={(e) => setExecEnv(e.target.value)}>
              {execEnvs.map((e) => (
                <option key={e} value={e}>{e}</option>
              ))}
            </select>
          ) : (
            <select disabled><option>— default (no exec_env wired) —</option></select>
          )}
        </label>
        {execEnvs.length > 0 && (
          <span className="hint">
            Execution environment is passed to both plan and apply.
          </span>
        )}
      </div>

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

            {applyResult && !applyResult.state_diverged && !applyResult.deadline_exceeded && (
              <span className={applyResult.applied ? 'apply-ok' : 'apply-fail'}>
                {applyResult.applied ? 'Applied successfully' : 'Apply returned false'}
              </span>
            )}

            {/* 207 / state-diverged: commit-back needs retry — direct user to Commit tab */}
            {applyResult?.state_diverged && (
              <div className="state-diverged-banner" role="alert">
                <strong>State diverged</strong> — the apply completed but commit-back was
                interrupted. Use the{' '}
                <button
                  className="btn-inline-link"
                  onClick={() => onRequestCommitTab?.()}
                >
                  Commit / PR tab
                </button>{' '}
                to retry the commit-back (idempotent).
              </div>
            )}

            {/* Deadline exceeded: long apply still in flight — recommend GitOps path */}
            {applyResult?.deadline_exceeded && (
              <div className="deadline-banner" role="alert">
                <strong>Apply is still in flight</strong> — the engine deadline was reached
                before the apply completed. The recommended path is to{' '}
                <button
                  className="btn-inline-link"
                  onClick={() => onRequestCommitTab?.()}
                >
                  commit specs via GitOps
                </button>{' '}
                (Commit / PR tab) so the engine can reconcile at its own pace rather than
                waiting for a synchronous response.
              </div>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
