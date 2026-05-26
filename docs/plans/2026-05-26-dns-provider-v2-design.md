# DNS provider v2 — multi-provider adapter expansion

**Status:** Draft
**Author:** codingsloth@pm.me
**Date:** 2026-05-26
**Predecessor:** docs/plans/2026-05-25-dns-ownership-policy-design.md (v1: DO + Cloudflare)
**Guidance:** /Users/jon/workspace/docs/design-guidance.md

## Goal

Extend `internal/dnsprovider/NewAdapter` switch to support Route53, GCP Cloud DNS, Azure DNS, Namecheap, GoDaddy, Hover. v1 contract `NewAdapter(provider string, creds map[string]string) (dnspolicy.Adapter, error)` already shipped multi-cred; v2 is implementation per provider + cred-key documentation.

## Global Design Guidance

Source: `/Users/jon/workspace/docs/design-guidance.md`

| guidance | design response |
|---|---|
| Primary language Go, stdlib-first | All adapters Go; libdns + Hover client = only deps |
| Dogfood workflow ecosystem | v2 extends existing `internal/dnsprovider/` switch; no new binaries, no new plugin repo |
| Reuse over rebuild | Hover client extracted from workflow-plugin-hover via `pkg/hoverclient` (issue #25 filed) instead of copying 582 LOC |
| libdns/cloud-sdks isolated in `internal/<provider>/` | Each adapter lives in `internal/dnsprovider/<provider>.go`; gate + step code stays vendor-free |
| Secrets never logged | Cred-map values never appear in error messages; missing-cred errors name only the key |
| Cross-driver parity | All 6 providers implement same `dnspolicy.Adapter` interface (GetTXT/UpsertTXT/UpsertRecord/DeleteRecord) |
| No mock-first | Unit tests use stub libdns providers + table-driven cred validation; live cloud opt-in via env (deferred) |
| Plugin minEngineVersion declared | unchanged (no engine ABI change in v2) |
| Goreleaser v2 + GitHub Release | unchanged |

## Architecture

Each adapter follows v1 `doAdapter` shape:

```go
type <prov>Adapter struct{ provider *libdns<prov>.Provider }

func new<Prov>Adapter(creds map[string]string) (dnspolicy.Adapter, error) {
    expanded := ExpandCredsMap(creds)
    // validate required keys; return clear "missing creds.<key>" per missing key
    return &<prov>Adapter{provider: &libdns<prov>.Provider{...}}, nil
}

func (a *<prov>Adapter) GetTXT(...) ([]string, error)      { /* delegate */ }
func (a *<prov>Adapter) UpsertTXT(...) error               { /* RRset-replace */ }
func (a *<prov>Adapter) UpsertRecord(...) error            { /* generic */ }
func (a *<prov>Adapter) DeleteRecord(...) error            { /* generic */ }
```

`NewAdapter` switch grows from 2 → 8 cases. Default branch lists supported set in error.

## Cred contracts per provider

Verified libdns adapter versions (proxy.golang.org 2026-05-26):

| provider | switch key | libdns import (version) | required cred keys | optional |
|---|---|---|---|---|
| Route53 | `aws`, `route53` | `github.com/libdns/route53 v1.6.2` | `access_key`, `secret_key`, `region` | `session_token`, `profile` (mutually exclusive with access_key); `assume_role_arn` |
| GCP Cloud DNS | `gcp`, `googleclouddns` | `github.com/libdns/googleclouddns v1.2.0` | `project_id` + ONE of: `service_account_json` (inline JSON), `service_account_path` (file), `adc=true` (ADC) | — |
| Azure DNS | `azure`, `azuredns` | `github.com/libdns/azure v0.5.0` | `tenant_id`, `client_id`, `client_secret`, `subscription_id`, `resource_group_name` | — |
| Namecheap | `namecheap` | `github.com/libdns/namecheap v1.0.0` | `api_key`, `api_user`, `client_ip` (Namecheap requires whitelisted source IP) | `sandbox=true` for sandbox API |
| GoDaddy | `godaddy` | `github.com/libdns/godaddy v1.1.0` | `api_key`, `api_secret` | — |
| Hover | `hover` | `github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient v0.3.0+` | `username`, `password` | `totp_secret` |

All cred values pass through `ExpandCredsMap` (env-var expansion) before validation.

### Aliasing rationale

Switch accepts multiple keys per provider (`aws` + `route53`, `gcp` + `googleclouddns`, `azure` + `azuredns`) because YAML authors say "aws" not "route53"; conversely DNS-savvy authors use the libdns name. Both work; switch is case-insensitive (v1 behavior preserved).

## NewAdapter switch (v2 final shape)

```go
func NewAdapter(provider string, creds map[string]string) (dnspolicy.Adapter, error) {
    switch strings.ToLower(strings.TrimSpace(provider)) {
    case "digitalocean":               return newDigitalOceanAdapter(creds)
    case "cloudflare":                 return newCloudflareAdapter(creds)
    case "aws", "route53":             return newRoute53Adapter(creds)
    case "gcp", "googleclouddns":      return newGoogleCloudDNSAdapter(creds)
    case "azure", "azuredns":          return newAzureAdapter(creds)
    case "namecheap":                  return newNamecheapAdapter(creds)
    case "godaddy":                    return newGoDaddyAdapter(creds)
    case "hover":                      return newHoverAdapter(creds)
    default:
        return nil, fmt.Errorf("%w %q (supported: digitalocean, cloudflare, aws, gcp, azure, namecheap, godaddy, hover)", ErrUnknownProvider, provider)
    }
}
```

## Cred-key documentation

New file `docs/providers/credentials.md` documents required keys per provider with YAML examples + env-var patterns. Each constructor's missing-cred error references the doc:

```go
return nil, fmt.Errorf("aws-route53: missing creds.access_key (required: access_key, secret_key, region; see docs/providers/credentials.md)")
```

## PR grouping (per-provider — user choice)

6 PRs in `workflow-plugin-infra` + 1 prerequisite issue in `workflow-plugin-hover`:

| PR # | Provider | Branch | Files | Blocked on |
|---|---|---|---|---|
| 1 | Route53 | feat/dns-provider-v2-route53 | adapter + test + switch case + docs row | — |
| 2 | GCP Cloud DNS | feat/dns-provider-v2-gcp | adapter + test + switch case + docs row | — |
| 3 | Azure DNS | feat/dns-provider-v2-azure | adapter + test + switch case + docs row | — |
| 4 | Namecheap | feat/dns-provider-v2-namecheap | adapter + test + switch case + docs row | — |
| 5 | GoDaddy | feat/dns-provider-v2-godaddy | adapter + test + switch case + docs row | — |
| 6 | Hover | feat/dns-provider-v2-hover | adapter + test + switch case + docs row | workflow-plugin-hover#25 (pkg/hoverclient extract + tag) |

PR 1 also adds `docs/providers/credentials.md` skeleton. PRs 2-6 append their provider row to that doc.

PRs 1-5 are independently mergeable. PR 6 sequenced after hover#25 ships + tag pinned.

## Assumptions

- **A1**: libdns adapter packages (`route53 v1.6.2`, `googleclouddns v1.2.0`, `azure v0.5.0`, `namecheap v1.0.0`, `godaddy v1.1.0`) expose stable `GetRecords/SetRecords/AppendRecords/DeleteRecords` per `libdns.Record` interface. Spot-verified via tag presence on proxy.golang.org; implementer verifies per import during PR work.
- **A2**: `ExpandCredsMap` correctly handles all value shapes (env-var substitution on each string value). JSON blobs as escaped strings work via env expansion. Existing v1 behavior preserved.
- **A3**: `workflow-plugin-hover` extraction (issue #25) is feasible — `internal/hover/client.go` (508 LOC) + `internal/hover/totp.go` (74 LOC) are stdlib-only HTTP client + TOTP. Clean move + visibility shift.
- **A4**: Each provider's token scope is managed via the provider's own ACL (AWS IAM policies, GCP IAM roles, Azure RBAC, Namecheap API whitelist, GoDaddy keys, Hover user). Ownership gate is defense in depth, not primary auth.
- **A5**: Unit tests with stub libdns providers (interface-satisfying mocks) are sufficient v2 validation (per user choice). Live integration tests deferred to v3 or per-tenant adoption.
- **A6**: No proto changes (creds already `map[string]string` from v1 strict-contracts cutover).
- **A7**: GCP service-account JSON dual-form (inline JSON string vs file path vs ADC) handled by the constructor: detect `service_account_json` first (raw JSON), `service_account_path` second (file read), `adc=true` last (no creds; use ambient ADC). Caller picks one; multiple set = error.
- **A8**: AWS optional `assume_role_arn` triggers `sts:AssumeRole` via `stscreds.NewAssumeRoleProvider`. Without it, static creds are used directly. Optional `session_token` allows temporary credentials.
- **A9**: Namecheap `client_ip` is validated by Namecheap server-side (returns API error if mismatched). Constructor passes value through; runtime validation only.

## Self-challenge — top 3 doubts

1. **GCP service-account triple-form ergonomics.** Three ways to auth (inline JSON, file path, ADC) means the constructor has triple branching + per-form error paths. Simpler: pick one (inline JSON only, callers can read file → env themselves). But ADC is genuinely useful for GKE-hosted workflows. Compromise: support inline JSON + ADC (drop file path; redundant with env expansion of inline). Decision: **inline JSON + ADC only**. File path becomes `{{ env "GCP_SA_JSON" }}` shell trick.
2. **Namecheap `client_ip` is a runtime-only failure mode.** Adapter can't pre-validate (no whoami). First gate read or write returns "Invalid IP" from Namecheap. Mitigation: doc warns explicitly; missing-cred error names `client_ip` as required so users don't ship empty.
3. **Hover blocked-on cross-repo work.** PR 6 cannot land until workflow-plugin-hover ships pkg/hoverclient + tag. If that takes weeks, PR 6 stalls. Acceptable: PRs 1-5 are the bulk of v2 value (5 cloud providers). PR 6 is the long-tail. User explicitly chose "all equal priority" but cross-repo prereq has its own pace.

## Security Review

| concern | response |
|---|---|
| auth/authz | Each provider's native ACL (IAM/RBAC/API-key whitelist). Gate is layer 2. |
| secrets | Cred values via `map[string]string` already; never logged. Missing-cred errors name only the key. Per-provider error wrappers strip values. |
| PII/logging | No PII in cred maps. Audit log (v1) records owner + operation only. |
| abuse case | Compromised cred = compromised provider account. Gate stops cross-owner mutation within the compromised scope, but cannot stop owner-self exfiltration. Documented in v1 trust model. |
| deps/trust | 5 new libdns adapters (all maintained by libdns org) + 1 new internal dep (`pkg/hoverclient` from our own org). Supply-chain risk audited via go.sum + workflow-plugin-supply-chain. |
| least privilege | Per-provider docs recommend scoped tokens (e.g., DNS-only IAM policy, not full admin). |

## Infrastructure Impact

- New Go deps: 5 libdns adapter modules + `pkg/hoverclient` (deferred until hover#25 ships)
- No runtime infra changes (plugin binary unchanged in shape)
- No engine ABI change; `minEngineVersion` stays at 0.64.0
- Adds ~1500-2000 LOC across 6 adapters + 6 test files + 1 docs file
- CI: existing GitHub Actions matrix unchanged; goreleaser pulls new transitive deps on tag

## Multi-Component Validation

Per v2 unit-test-only strategy:

- Per-adapter unit tests: cred validation (missing keys → error per key); switch dispatch (case-insensitive, both alias keys per provider hit same constructor); `dnspolicy.Adapter` interface compliance.
- No live cloud calls in CI.
- End-to-end validation happens when first multisite tenant on Route53/GCP/Azure exercises the gate path (gate → adapter → provider API → real record). Deferred but tracked.

## Rollback

- Per-PR revert. Each adapter is independent. Reverting PR N removes that provider from supported set; `NewAdapter` returns "unknown provider" error for its key.
- No data migration. No DNS-side state changes from these PRs alone (adapters are infrastructure for future YAML-driven applies).
- Go module pin rollback handled by `go.mod` revert.

## Out of scope

- Live integration tests (deferred)
- gocodealone-dns mirror extension (separate work, design-guidance-cited issue chain)
- workflow#779 cross-driver ownership-tagging beyond DNS (separate work)
- New provider beyond the 6 listed (Vultr, Linode, NS1, etc.)
- Cloudflare migration to multi-cred (v1 single-token still works; refactor optional)
- WhoAmI for token-bound owner verification (v2 followup)

## Followups (filed post-merge)

- workflow#779 (cross-driver IaC tagging) — Hover/Namecheap/GoDaddy DNS provider class participation
- Live integration test harness — opt-in env-gated for Route53/GCP/Azure
- GCP file-path cred form (if shell-trick `{{ env }}` proves insufficient)
- Cloudflare migration to multi-cred (parity with v2 adapters)
