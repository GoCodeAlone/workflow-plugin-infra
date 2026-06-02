// ── Runtime config injected by the workflow engine ──────────────────────────

export interface InfraUIConfig {
  api_base_path: string
}

// ── Catalog ──────────────────────────────────────────────────────────────────

export interface ProviderCatalog {
  regions: string[]
  types: string[]
  source: 'live' | 'static'
}

// ── Resources ────────────────────────────────────────────────────────────────

export type ResourceStatus = 'active' | 'pending' | 'error' | 'unknown'

export interface Resource {
  name: string
  type: string
  provider: string
  status: ResourceStatus
}

export interface ResourceSpec {
  name: string
  type: string
  provider: string
  region: string
  [key: string]: unknown
}

// ── Plan / Apply ──────────────────────────────────────────────────────────────

export interface PlanAction {
  action: 'create' | 'update' | 'delete' | 'no-op' | string
  resource: string
  type: string
  diff?: Record<string, { old: unknown; new: unknown }>
}

export interface PlanResult {
  plan: {
    actions: PlanAction[]
  }
  desired_hash: string
}

export interface ApplyResult {
  applied: boolean
  result: unknown
}

// ── Commit / PR ───────────────────────────────────────────────────────────────

export interface CommitInput {
  specs: ResourceSpec[]
  branch: string
  message: string
}

export interface CommitResult {
  branch: string
  pr_url: string
}

// ── Drift ─────────────────────────────────────────────────────────────────────

export interface DriftEntry {
  resource: string
  type: string
  field: string
  desired: unknown
  actual: unknown
}

export interface DriftResult {
  any_drifted: boolean
  drifts: DriftEntry[]
  count: number
  supported: boolean
}

// ── Secrets ───────────────────────────────────────────────────────────────────

export interface SecretMeta {
  name: string
  backend: string
  description: string
}

export interface SecretDeclareInput {
  name: string
  backend: string
  value?: string
}

export interface SecretDeclareResult {
  ok: boolean
}

// ── View tab names ────────────────────────────────────────────────────────────

export type Tab = 'resources' | 'plan' | 'commit' | 'drift' | 'secrets'
