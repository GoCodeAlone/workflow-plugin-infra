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

// The /apply route composes its response from multiple chained step outputs.
// Fields originate from different steps (see api.ts contract block):
//   - apply_result/desired_hash/provider/action_count → step.iac_provider_apply (raw)
//   - committed/state_diverged                         → step.iac_commit_back (chained)
//   - applied/deadline_exceeded                        → composed by the route
export interface ApplyResult {
  /** Composed by the route: true when the apply (and any commit-back) succeeded. */
  applied: boolean
  /** Raw output of step.iac_provider_apply. */
  apply_result?: unknown
  desired_hash?: string
  provider?: string
  action_count?: number
  /** From step.iac_commit_back: whether the commit-back committed. */
  committed?: boolean
  /** HTTP 207: state diverged — commit-back incomplete, retry required (from commit_back). */
  state_diverged?: boolean
  /** Engine returned a deadline error — apply is still in flight (use GitOps path). */
  deadline_exceeded?: boolean
}

// ── Commit / PR ───────────────────────────────────────────────────────────────

export interface CommitInput {
  specs: ResourceSpec[]
  branch: string
  message: string
}

// Mirrors step.iac_commit_back output. `ref` is the branch name (branch-push
// target) or the PR URL (gh-pr target) — one-or-the-other depending on config.
export interface CommitResult {
  committed: boolean
  ref: string
  /** HTTP 207: state diverged — commit-back requires retry. */
  state_diverged?: boolean
  reason?: string
}

// ── Reconcile ─────────────────────────────────────────────────────────────────

// Mirrors step.iac_provider_reconcile output. `ref` is the draft-PR branch/URL.
export interface ReconcileResult {
  draft: boolean
  ref: string
  /** Always present: reconcile output is approximate and must be reviewed before merge. */
  warning: string
  count: number
  state_diverged?: boolean
  reason?: string
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
