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

// ── Exec Environments ─────────────────────────────────────────────────────────

export interface ExecEnvs {
  exec_envs: string[]
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
  /** HTTP 207: state diverged — commit-back incomplete, retry required */
  state_diverged?: boolean
  /** Engine returned a deadline error — apply is still in flight (use GitOps path) */
  deadline_exceeded?: boolean
}

// ── Commit / PR ───────────────────────────────────────────────────────────────

export interface CommitInput {
  specs: ResourceSpec[]
  branch: string
  message: string
  exec_env?: string
}

export interface CommitResult {
  branch: string
  pr_url: string
  /** HTTP 207: state diverged — commit-back requires retry */
  state_diverged?: boolean
}

// ── Reconcile ─────────────────────────────────────────────────────────────────

export interface ReconcileResult {
  draft_pr_ref: string
  pr_url?: string
  /** Always present: reconcile output is approximate and must be reviewed before merge */
  warning: string
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
