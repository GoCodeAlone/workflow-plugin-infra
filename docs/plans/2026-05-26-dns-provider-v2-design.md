# DNS provider v2 — multi-provider adapter expansion

**Status:** Draft (cycle 2 — adversarial cycle 1 findings applied)
**Author:** codingsloth@pm.me
**Date:** 2026-05-26
**Predecessor:** docs/plans/2026-05-25-dns-ownership-policy-design.md (v1: DO + Cloudflare)
**Guidance:** /Users/jon/workspace/docs/design-guidance.md

## Revision history

- **cycle 1 (initial draft)**: 6 providers, dual-aliasing, GCP triple-form auth, AWS assume-role, GoDaddy two-key, Namecheap `api_user` + `sandbox=true` — most cred-key shapes invented rather than verified against upstream.
- **cycle 2 (this revision)**: cred keys aligned to verified upstream `libdns/*` struct JSON tags (verified via `go doc` against `proxy.golang.org` 2026-05-26). Dual-aliasing dropped. AWS assume-role + GCP inline JSON deferred to v3. Azure managed-identity surfaced. Namecheap whole-zone-replace risk surfaced as Critical pre-merge check. Per-provider docs files (no merge contention). GoDaddy ships with API-restriction warning.

## Goal

Extend `internal/dnsprovider/NewAdapter` switch to support Route53, GCP Cloud DNS, Azure DNS, Namecheap, GoDaddy, Hover. v1 contract `NewAdapter(provider string, creds map[string]string) (dnspolicy.Adapter, error)` already shipped multi-cred; v2 is implementation per provider + cred-key documentation.

## Global Design Guidance

Source: `/Users/jon/workspace/docs/design-guidance.md`

| guidance | design response |
|---|---|
| Primary language Go, stdlib-first | All adapters Go; libdns + Hover client = only new deps |
| Dogfood workflow ecosystem | v2 extends existing `internal/dnsprovider/` switch; no new binaries, no new plugin repo |
| Reuse over rebuild | Hover client extracted from workflow-plugin-hover via `pkg/hoverclient` (issue #25 filed) instead of copying 582 LOC |
| libdns/cloud-sdks isolated in `internal/<provider>/` | Each adapter lives in `internal/dnsprovider/<provider>.go`; gate + step code stays vendor-free |
| Secrets never logged | Cred-map values never appear in error messages; missing-cred errors name only the key. Engine-side template-expansion errors checked separately (see Security Review row "engine log scrubbing") |
| Cross-driver parity | All 6 providers implement same `dnspolicy.Adapter` interface (GetTXT/UpsertTXT/UpsertRecord/DeleteRecord) |
| No mock-first | Unit tests use stub libdns providers + table-driven cred validation; live cloud opt-in via env (deferred to v3) |
| Plugin minEngineVersion declared | unchanged (no engine ABI change in v2) |
| Goreleaser v2 + GitHub Release | unchanged |

## Architecture

Each adapter follows v1 `doAdapter` shape (see `internal/dnsprovider/digitalocean.go` as canonical reference):

```go
type <prov>Adapter struct{ provider *libdns<prov>.Provider }

func new<Prov>Adapter(creds map[string]string) (dnspolicy.Adapter, error) {
    c := ExpandCredsMap(creds)
    // validate required keys; return "<prov>: missing creds.<key>" per missing key
    return &<prov>Adapter{provider: &libdns<prov>.Provider{
        // map cred-keys 1:1 to upstream struct fields (see cred contracts table)
    }}, nil
}

func (a *<prov>Adapter) GetTXT(...) ([]string, error)      { /* delegate via libdns.RecordGetter */ }
func (a *<prov>Adapter) UpsertTXT(...) error               { /* per-provider RRset semantics — see table */ }
func (a *<prov>Adapter) UpsertRecord(...) error            { /* per-provider RRset semantics — see table */ }
func (a *<prov>Adapter) DeleteRecord(...) error            { /* delegate via libdns.RecordDeleter */ }
```

`NewAdapter` switch grows from 2 → 8 cases.

## Cred contracts per provider — verified against upstream

Verified via `go doc github.com/libdns/<provider>.Provider` on 2026-05-26. Cred-key names match upstream JSON tags exactly to prevent the cred-key-rename bug class.

### Route53 (`libdns/route53 v1.6.2`)

| YAML cred key | maps to upstream field (JSON tag) | required? |
|---|---|---|
| `region` | `Region` (`region`) | yes |
| `access_key_id` | `AccessKeyId` (`access_key_id`) | yes (unless ambient creds via env) |
| `secret_access_key` | `SecretAccessKey` (`secret_access_key`) | yes (unless ambient creds via env) |
| `session_token` | `SessionToken` (`session_token`) | optional (for STS temp creds) |
| `profile` | `Profile` (`profile`) | optional (alternative to access_key_id/secret_access_key) |

If `access_key_id` + `secret_access_key` empty AND `profile` empty → libdns falls back to AWS env vars / ambient creds. Adapter does NOT enforce all-three-empty; lets upstream resolve.

**Deferred to v3**: `assume_role_arn` — requires aws-sdk-go-v2 `stscreds.NewAssumeRoleProvider` chain BEFORE libdns provider construction. Adds 30+ transitive modules. Not v2 scope.

### GCP Cloud DNS (`libdns/googleclouddns v1.2.0`)

| YAML cred key | maps to upstream field (JSON tag) | required? |
|---|---|---|
| `gcp_project` | `Project` (`gcp_project`) | yes |
| `service_account_path` | `ServiceAccountJSON` (`gcp_application_default`) — path to JSON file | optional (alternative to ADC) |

If `service_account_path` empty → libdns uses Application Default Credentials (ADC) — picks up `GOOGLE_APPLICATION_CREDENTIALS` env or GKE workload identity. No explicit `adc=true` knob needed; absence-of-path == ADC.

**Deferred to v3**: inline JSON cred (`service_account_json`). Upstream `ServiceAccountJSON` field is a **file path**, not JSON content (per upstream docs). Supporting inline JSON requires writing tempfile + setting env + cleanup — security-sensitive (secrets on disk) + new failure paths. Out of v2 scope. Workaround: callers use `{{ env "GCP_SA_PATH" }}` + provision the file via init container / secret mount.

### Azure DNS (`libdns/azure v0.5.0`)

| YAML cred key | maps to upstream field (JSON tag) | required? |
|---|---|---|
| `subscription_id` | `SubscriptionId` (`subscription_id`) | yes |
| `resource_group_name` | `ResourceGroupName` (`resource_group_name`) | yes |
| `tenant_id` | `TenantId` (`tenant_id`) | optional* |
| `client_id` | `ClientId` (`client_id`) | optional* |
| `client_secret` | `ClientSecret` (`client_secret`) | optional* |

*Auth modes (per upstream docs):
- **Service principal**: ALL of `tenant_id` + `client_id` + `client_secret` set.
- **Managed identity**: ALL of `tenant_id` + `client_id` + `client_secret` empty. Upstream uses ambient Azure managed identity.

Adapter enforces: either all three set or all three empty. Mixed (2 set + 1 empty) → reject with error naming the missing key.

### Namecheap (`libdns/namecheap v1.0.0`)

| YAML cred key | maps to upstream field (JSON tag) | required? |
|---|---|---|
| `api_key` | `APIKey` (`api_key`) | yes |
| `user` | `User` (`user`) | yes |
| `client_ip` | `ClientIP` (`client_ip`) | **yes** (adapter strictly requires it) |
| `api_endpoint` | `APIEndpoint` (`api_endpoint`) | optional (defaults to production) |

`client_ip` made strictly required by adapter (even though upstream allows empty + discovery fallback): self-hosted CI runners on private subnets (per workspace guidance) cannot rely on discovery; whitelisted IP must be explicit. Missing → reject.

**Namecheap whole-zone-replace pre-merge check (Critical)**: upstream Namecheap API `namecheap.domains.dns.setHosts` replaces ALL records in a zone per call. Before PR 4 merges, implementer MUST verify libdns/namecheap's `SetRecords` either (a) uses an additive endpoint, OR (b) does internal merge-with-existing before calling setHosts. If neither, the adapter MUST implement merge-with-existing in `UpsertRecord`/`UpsertTXT` to avoid wiping unrelated records. Verification commit + test that exercises 2-record-zone-with-1-foreign-record scenario is gating.

### GoDaddy (`libdns/godaddy v1.1.0`)

| YAML cred key | maps to upstream field (JSON tag) | required? |
|---|---|---|
| `api_token` | `APIToken` (`api_token`) — format: `<sso-key>:<sso-secret>` | yes |

GoDaddy's production API auth header is `sso-key <key>:<secret>`; upstream wraps this as a single `APIToken` string. Adapter accepts the concatenated form directly.

**GoDaddy API restriction warning**: GoDaddy revoked public DNS API access for accounts with fewer than 50 domains (reported 2024-Q1, unresolved as of upstream v1.1.0 release Aug 2025). Adapter ships with explicit warning in `docs/providers/godaddy.md`: API may return 403 unauthorized for small-account holders. Pre-merge: implementer verifies a real API call (with whatever test account is available) returns either 200 or the documented 403 — both acceptable; silent failure modes not.

### Hover (`workflow-plugin-hover/pkg/hoverclient`)

| YAML cred key | required? |
|---|---|
| `username` | yes |
| `password` | yes |
| `totp_secret` | optional |

Adapter wraps `pkg/hoverclient` (extracted via workflow-plugin-hover#25). Cred-key shape stabilized post-extraction. Sequenced after that PR ships + tag pinned.

## NewAdapter switch (v2 final shape)

```go
func NewAdapter(provider string, creds map[string]string) (dnspolicy.Adapter, error) {
    switch strings.ToLower(strings.TrimSpace(provider)) {
    case "digitalocean":      return newDigitalOceanAdapter(creds)
    case "cloudflare":        return newCloudflareAdapter(creds)
    case "route53":           return newRoute53Adapter(creds)
    case "googleclouddns":    return newGoogleCloudDNSAdapter(creds)
    case "azuredns":          return newAzureAdapter(creds)
    case "namecheap":         return newNamecheapAdapter(creds)
    case "godaddy":           return newGoDaddyAdapter(creds)
    case "hover":             return newHoverAdapter(creds)
    default:
        return nil, fmt.Errorf("%w %q (supported: digitalocean, cloudflare, route53, googleclouddns, azuredns, namecheap, godaddy, hover)", ErrUnknownProvider, provider)
    }
}
```

**Single canonical key per provider** (libdns-aligned). Cloud-shorthand aliases (`aws`/`gcp`/`azure`) deferred to v3 — add when a real consumer requests, with parameterized switch-dispatch test.

## RRset semantics per provider (informs UpsertTXT / UpsertRecord implementation)

| provider | upstream model | adapter UpsertTXT shape | pre-merge verification |
|---|---|---|---|
| Route53 | ChangeBatch (atomic CREATE/UPSERT/DELETE) — libdns `SetRecords` wraps as UPSERT | `SetRecords` direct (atomic upsert) | unit test: GetRecords → SetRecords idempotent |
| GCP Cloud DNS | Changes API (atomic add/delete batch) — libdns `SetRecords` does delete-then-add internally | `SetRecords` direct | unit test: GetRecords → SetRecords replaces |
| Azure DNS | RecordSet PUT (whole RRset replace per HTTP request) — libdns `SetRecords` direct map | `SetRecords` direct | unit test: GetRecords → SetRecords replaces |
| Namecheap | `setHosts` replaces WHOLE ZONE — verify libdns wraps or whether merge-with-existing required | `UpsertTXT` must Get-merge-Set OR libdns does it; **gating verification** | integration-shape test: 2-record-zone, upsert 1 → other survives. If fails, adapter Get-merges before Set |
| GoDaddy | per-record PUT (record-level) — libdns `SetRecords` maps directly | `SetRecords` direct | unit test: GetRecords → SetRecords replaces |
| Hover | scraped HTML (per-record CRUD) | `UpsertRecord` direct call | unit test against pkg/hoverclient stub |
| DigitalOcean (v1) | per-record API requiring ID — libdns `SetRecords` requires ID + `AppendRecords` for new | DELETE-all + APPEND (RRset-replace dance, already shipped) | — |
| Cloudflare (v1) | per-record API — libdns `SetRecords` matches on name+type | `SetRecords` direct | — |

Pre-merge verification rows are gating commits (test must pass) — caught early in PR review, not at runtime.

## Cred-key documentation (per-provider files, no merge contention)

`docs/providers/README.md` — index + general cred-key conventions (env-expansion, secret-handling).
`docs/providers/<provider>.md` — one file per provider with cred-key table + YAML example + provider-specific gotchas. Each PR touches only its own file. PR 1 (Route53) also adds README.md skeleton.

## PR grouping (per-provider — user choice)

6 PRs in `workflow-plugin-infra` + 1 prerequisite issue in `workflow-plugin-hover`:

| PR # | Provider | Branch | Files | Blocked on |
|---|---|---|---|---|
| 1 | Route53 | feat/dns-provider-v2-route53 | adapter + test + switch case + `docs/providers/route53.md` + `docs/providers/README.md` skeleton | — |
| 2 | GCP Cloud DNS | feat/dns-provider-v2-gcp | adapter + test + switch case + `docs/providers/googleclouddns.md` | — |
| 3 | Azure DNS | feat/dns-provider-v2-azure | adapter + test + switch case + `docs/providers/azuredns.md` | — |
| 4 | Namecheap | feat/dns-provider-v2-namecheap | adapter + test + switch case + `docs/providers/namecheap.md` + whole-zone-replace verification commit | — |
| 5 | GoDaddy | feat/dns-provider-v2-godaddy | adapter + test + switch case + `docs/providers/godaddy.md` (with API-restriction warning) | — |
| 6 | Hover | feat/dns-provider-v2-hover | adapter + test + switch case + `docs/providers/hover.md` | workflow-plugin-hover#25 (pkg/hoverclient extract + tag) |

PRs 1-5 are independently mergeable + parallelizable (no shared file). PR 6 sequenced after hover#25 ships + tag pinned.

**User-intent reconciliation note**: user chose "all equal priority (sweep them together)". PR 6 has cross-repo prereq; if hover#25 takes longer than v2 main batch, PR 6 lands in followup sweep — explicitly acknowledged here so it isn't surprising at execute time.

**Operator-time retro item**: log time-to-merge across 6 PRs vs hypothetical single sweep PR; feed into future sweep-vs-split heuristics.

## Assumptions

- **A1**: libdns adapter packages (`route53 v1.6.2`, `googleclouddns v1.2.0`, `azure v0.5.0`, `namecheap v1.0.0`, `godaddy v1.1.0`) expose `GetRecords/SetRecords/AppendRecords/DeleteRecords` per `libdns.Record` interface (verified via `go doc` 2026-05-26 — all 5 provide this set).
- **A2**: `ExpandCredsMap` correctly handles all value shapes (env-var substitution via `os.ExpandEnv` on each string value). Existing v1 behavior preserved.
- **A3**: `workflow-plugin-hover` extraction (issue #25) is feasible — `internal/hover/client.go` (508 LOC) + `internal/hover/totp.go` (74 LOC) are stdlib-only HTTP client + TOTP. Clean move + visibility shift.
- **A4**: Each provider's token scope is managed via the provider's own ACL (AWS IAM policies, GCP IAM roles, Azure RBAC, Namecheap API whitelist, GoDaddy keys, Hover user). Ownership gate is defense in depth, not primary auth.
- **A5**: Unit tests with stub libdns providers (interface-satisfying mocks) are sufficient v2 validation (per user choice). Live integration tests deferred to v3.
- **A6**: No proto changes (creds already `map[string]string` from v1 strict-contracts cutover).
- **A7**: Cred-key names exactly match upstream struct JSON tags (verified 2026-05-26 — cycle-2 revision is the source of truth, not the cycle-1 invented names).
- **A8**: Engine-side template-expansion errors (e.g., `{{ env "FOO" }}` resolving empty string) are scrubbed by the workflow engine's logger redaction layer. **Open question for verification**: spot-check `workflow` engine config-expansion path during PR 1 implementation; if not scrubbed, file followup.
- **A9**: Namecheap whole-zone-replace risk is bounded by pre-merge gating verification (PR 4 RRset semantics test) — if upstream wraps adequately, no extra adapter code; if not, adapter implements Get-merge-Set. Either way, no zone wipe.

## Self-challenge — top 3 doubts (post cycle-2)

1. **GoDaddy ships dead code if 50-domain restriction is real for everyone.** Mitigation: PR 5 pre-merge test against a real GoDaddy account (any test API key) — if 403 universal, defer and drop PR 5. Adapter code is small + reversible.
2. **Namecheap whole-zone-replace risk** could still slip through if test scenario isn't comprehensive. Mitigation: pre-merge verification commit in PR 4 must include a 3-record-zone-2-foreign-records integration-shape test (using stub upstream that records calls). Reviewer rejects PR 4 without this commit.
3. **Hover sequencing.** PR 6 sits idle until hover#25 ships. If hover client extraction surfaces unexpected refactor surface (e.g., shared types across `internal/` and `pkg/`), the prereq slips. Acceptable — PRs 1-5 deliver bulk of v2 value; PR 6 ships in followup sweep when ready.

## Security Review

| concern | response |
|---|---|
| auth/authz | Each provider's native ACL (IAM/RBAC/API-key whitelist). Gate is layer 2. |
| secrets | Cred values via `map[string]string` already; never logged. Missing-cred errors name only the key. Per-provider error wrappers strip values. |
| engine log scrubbing | Open question (A8). Spot-check during PR 1; file followup if unverified. |
| PII/logging | No PII in cred maps. Audit log (v1) records owner + operation only. |
| abuse case | Compromised cred = compromised provider account. Gate stops cross-owner mutation within the compromised scope, but cannot stop owner-self exfiltration. Documented in v1 trust model. |
| deps/trust | 5 new libdns adapter modules (all maintained by libdns org) + 1 new internal dep (`pkg/hoverclient` from our own org). Supply-chain risk audited via go.sum + workflow-plugin-supply-chain. Post-merge: count go.sum delta; if >300 lines, file build-tag-isolation followup. |
| least privilege | Per-provider docs recommend scoped tokens (e.g., DNS-only IAM policy, not full admin). |
| inline JSON / tempfile risk | Deferred — GCP `service_account_json` form not v2 (path + ADC only). No secrets-on-disk surface added in v2. |

## Infrastructure Impact

- New Go deps: 5 libdns adapter modules + `pkg/hoverclient` (deferred until hover#25 ships). Route53 alone pulls aws-sdk-go-v2/{config,credentials,service/route53,…} (~30 transitive modules).
- No runtime infra changes (plugin binary unchanged in shape).
- No engine ABI change; `minEngineVersion` stays at 0.64.0.
- Adds ~1500-2000 LOC across 6 adapters + 6 test files + 6 docs files.
- CI: existing GitHub Actions matrix unchanged; goreleaser pulls new transitive deps on tag.
- **Post-merge dep audit**: count go.sum delta; run workflow-plugin-supply-chain scan on resulting binary; if delta > 300 lines, file build-tag-isolation followup (per-provider build tags so consumers pay only for what they use).

## Multi-Component Validation

Per v2 unit-test-only strategy:

- Per-adapter unit tests: cred validation (missing keys → error per key); switch dispatch (case-insensitive single canonical key per provider); `dnspolicy.Adapter` interface compliance.
- Per-adapter RRset-semantics tests per the table above (gating for Namecheap especially).
- No live cloud calls in CI.
- End-to-end validation happens when first multisite tenant on Route53/GCP/Azure exercises the gate path (gate → adapter → provider API → real record). Deferred but tracked.

## Rollback

- Per-PR revert. Each adapter is independent. Reverting PR N removes that provider from supported set; `NewAdapter` returns "unknown provider" error for its key.
- No data migration. No DNS-side state changes from these PRs alone (adapters are infrastructure for future YAML-driven applies).
- Go module pin rollback handled by `go.mod` revert.
- **Rollback window assumption**: PR-N may be safely reverted only while zero pipelines reference its provider key in YAML. Once a YAML pins `provider: <key>`, removal is a breaking change requiring deprecation cycle. Documented in `docs/providers/README.md` as a stability note.

## Out of scope

- Live integration tests (deferred to v3)
- gocodealone-dns mirror extension (separate work)
- workflow#779 cross-driver ownership-tagging beyond DNS (separate work)
- New providers beyond the 6 listed (Vultr, Linode, NS1, etc.)
- Cloudflare migration to multi-cred (v1 single-token still works; refactor optional)
- WhoAmI for token-bound owner verification (v2 followup)
- Provider aliases (`aws`/`gcp`/`azure` shorthand) — deferred to v3
- AWS assume-role chain (`assume_role_arn`) — deferred to v3
- GCP inline JSON cred form — deferred to v3
- Build-tag per-provider isolation — followup if go.sum delta > 300

## Followups (filed post-merge)

- workflow#779 (cross-driver IaC tagging) — Hover/Namecheap/GoDaddy DNS provider class participation
- Live integration test harness — opt-in env-gated for Route53/GCP/Azure
- AWS assume-role v3 expansion
- GCP inline-JSON cred form (if shell-trick env+path proves insufficient)
- Cloudflare migration to multi-cred (parity with v2 adapters)
- Provider alias dispatch (`aws`/`gcp`/`azure`) if consumer-driven demand surfaces
- Engine-side template-expansion log scrubbing audit (A8 open question)
- Build-tag isolation per provider if dep delta excessive
