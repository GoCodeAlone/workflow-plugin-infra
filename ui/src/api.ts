// ─────────────────────────────────────────────────────────────────────────────
// HTTP response-shape contract (workflow v0.72.0)
//
// The SPA expects each route to return EXACTLY these field names. The scenario
// (PR11) wires each route's terminal `step.json_response` to emit precisely
// these fields — the apply route composes `applied`/`state_diverged`/
// `deadline_exceeded` from the chained apply→commit_back step outputs plus the
// deadline path. This block is the contract PR11 must satisfy, documented here
// so PR11 isn't guessing.
//
//   GET  /exec-envs   → { exec_envs: string[] }
//
//   POST /plan        → { plan: { actions: PlanAction[] }, desired_hash }
//                       (request body: { provider, specs, exec_env? })
//
//   POST /apply       → composed from multiple chained step outputs:
//                         from step.iac_provider_apply (raw):
//                           apply_result, desired_hash, provider, action_count
//                         from step.iac_commit_back (chained):
//                           committed, state_diverged?, reason?
//                         composed by the route:
//                           applied (success of apply + any commit-back),
//                           deadline_exceeded? (long-apply deadline path)
//                       (request body: { provider, specs, desired_hash, exec_env? })
//                       Pre-flight: the route runs step.iac_secret_reachability
//                         ({ all_reachable, secrets: [{ref, reachable, reason}] })
//                         and returns HTTP 409 when not all secrets are reachable.
//
//   POST /commit      → step.iac_commit_back: { committed, ref, state_diverged?, reason? }
//                         ref = branch name (branch-push) OR PR URL (gh-pr).
//                       (request body: { specs, branch, message } — the commit
//                        route does NOT consume exec_env, so it is not sent.)
//
//   POST /reconcile   → step.iac_provider_reconcile:
//                         { draft, ref, warning, count, state_diverged?, reason? }
//                       (request body: { provider })
//
// HTTP 207 (Multi-Status) is a partial success: the body carries
// state_diverged=true and the UI must surface it (Retry commit-back), not throw.
// ─────────────────────────────────────────────────────────────────────────────

import type {
  ApplyResult,
  CommitInput,
  CommitResult,
  DriftResult,
  ExecEnvs,
  PlanResult,
  ProviderCatalog,
  ReconcileResult,
  Resource,
  ResourceSpec,
} from './types'

declare global {
  interface Window {
    __WORKFLOW_INFRA_UI__?: {
      api_base_path?: string
    }
  }
}

export function getApiBasePath(): string {
  return window.__WORKFLOW_INFRA_UI__?.api_base_path ?? '/api/infra'
}

function joinPath(base: string, path: string): string {
  const left = base.endsWith('/') ? base.slice(0, -1) : base
  const right = path.startsWith('/') ? path : `/${path}`
  return `${left}${right}`
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const url = joinPath(getApiBasePath(), path)
  const res = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  // 207 Multi-Status is a partial-success: commit-back may have diverged.
  // Parse the body so callers can inspect state_diverged rather than treating
  // it as an error. Guard against an empty 207 body — a 207 with no payload is
  // meaningless to the UI and should surface as an error, not an unhandled
  // JSON parse rejection.
  if (res.status === 207) {
    const text = await res.text()
    if (!text.trim()) {
      throw new Error(`${method} ${path} (207): empty body — expected partial-status payload`)
    }
    return JSON.parse(text) as T
  }
  if (!res.ok) {
    const text = await res.text()
    throw new Error(`${method} ${path} (${res.status}): ${text}`)
  }
  return res.json() as Promise<T>
}

// ── Resources ────────────────────────────────────────────────────────────────

export function listResources(provider?: string): Promise<{ resources: Resource[] }> {
  const qs = provider ? `?provider=${encodeURIComponent(provider)}` : ''
  return request<{ resources: Resource[] }>('GET', `/resources${qs}`)
}

// ── Catalog ───────────────────────────────────────────────────────────────────

export function getProviderCatalog(provider: string): Promise<ProviderCatalog> {
  return request<ProviderCatalog>('GET', `/providers/${encodeURIComponent(provider)}/catalog`)
}

// ── Exec Environments ─────────────────────────────────────────────────────────

export function getExecEnvs(): Promise<ExecEnvs> {
  return request<ExecEnvs>('GET', '/exec-envs')
}

// ── Plan ──────────────────────────────────────────────────────────────────────

export function planResources(
  provider: string,
  specs: ResourceSpec[],
  exec_env?: string,
): Promise<PlanResult> {
  return request<PlanResult>('POST', '/plan', { provider, specs, ...(exec_env ? { exec_env } : {}) })
}

// ── Apply ─────────────────────────────────────────────────────────────────────

export function applyResources(
  provider: string,
  specs: ResourceSpec[],
  desired_hash: string,
  exec_env?: string,
): Promise<ApplyResult> {
  return request<ApplyResult>('POST', '/apply', {
    provider,
    specs,
    desired_hash,
    ...(exec_env ? { exec_env } : {}),
  })
}

// ── Commit / PR ───────────────────────────────────────────────────────────────

export function commitSpecs(input: CommitInput): Promise<CommitResult> {
  return request<CommitResult>('POST', '/commit', input)
}

// ── Reconcile ─────────────────────────────────────────────────────────────────

export function reconcile(provider: string): Promise<ReconcileResult> {
  return request<ReconcileResult>('POST', '/reconcile', { provider })
}

// ── Drift ─────────────────────────────────────────────────────────────────────

export function getDrift(provider?: string): Promise<DriftResult> {
  const qs = provider ? `?provider=${encodeURIComponent(provider)}` : ''
  return request<DriftResult>('GET', `/drift${qs}`)
}
