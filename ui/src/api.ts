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
  SecretDeclareInput,
  SecretDeclareResult,
  SecretMeta,
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
  // it as an error.
  if (res.status === 207) {
    return res.json() as Promise<T>
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

// ── Secrets ───────────────────────────────────────────────────────────────────

export function listSecrets(): Promise<{ secrets: SecretMeta[] }> {
  return request<{ secrets: SecretMeta[] }>('GET', '/secrets')
}

export function declareSecret(input: SecretDeclareInput): Promise<SecretDeclareResult> {
  return request<SecretDeclareResult>('POST', '/secrets', input)
}
