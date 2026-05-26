# DNS import + provider decoupling — phased design

**Status:** Draft (cycle 2 — full architectural pivot after adversarial cycle 1)
**Author:** codingsloth@pm.me
**Date:** 2026-05-26
**Predecessor:** `docs/plans/2026-05-26-dns-provider-v2-design.md` (v2 in-process libdns adapter pattern)
**Guidance:** `/Users/jon/workspace/docs/design-guidance.md`
**Adversarial-review trigger:** cycle 1's contract-refactor architecture (`DNSProvider` gRPC service inside workflow-plugin-infra w/ peer-dispatch) was ruled FAIL — three Criticals: peer-dispatch RPC does not exist on `EngineCallbackService`, `infra.dns_record` step handler also calls `dnsprovider.NewAdapter` (caller-list claim falsified), and `wfctl plugin infra dns import` collides with reserved `plugin`/`infra` command names. See cycle-1 transcript in this conversation. This cycle replaces the architecture with engine-native primitives and phases the work.

## Goal

Deliver three outcomes in one phased plan:

1. **DNS state import** from every DNS-capable provider plugin (DigitalOcean, Cloudflare, Namecheap, Hover) using the engine's existing `wfctl infra import` strict-contract path. No new contract, no new RPC, no peer-dispatch SDK gap.
2. **Decoupling refactor**: relocate the libdns-using DNS handling code (`dnspolicy`/`dnsgate`/`dnsaudit`/`admincli`) out of `workflow-plugin-infra` so the orchestrator plugin no longer carries provider-specific dependencies. Provider DNS handling lives in the respective provider plugins (already true for `infra.dns` resource driver; the new state extends the same boundary to policy-gate concerns).
3. **Cross-provider validation scenarios** in `workflow-scenarios` proving state import/export round-trips, intact zone-transfer across providers (DNS records move from provider A to provider B losslessly), and delegation (parent zone at provider A delegates a subdomain to provider B; subdomain records managed independently).

The original user ask ("import existing DNS from CF/NC/DO") is the floor. The decoupling + validation work follows the user's clarifications (cross-plugin coupling concern + workflow-scenarios test request).

## Why this architecture (not the cycle-1 contract refactor)

`wfctl infra import` already exists (`workflow/cmd/wfctl/infra.go:1021` — `runInfraImport`). It already dispatches via the strict contract:

- `resolveIaCProvider(ctx, providerType, providerCfg)` → loads provider plugin via gRPC
- `provider.Import(ctx, cloudID, spec.Type)` → strict-contract `IaCProvider.Import` (REQUIRED per `iac.proto:32`)
- `store.SaveResource(ctx, state)` → persists into IaC state backend

Every provider plugin (CF, NC, DO, Hover) already implements `IaCProvider.Import` for `infra.dns` (verified in `workflow-plugin-cloudflare/internal/iacserver.go:159`, equivalents in NC/DO/Hover). The only missing piece for full-account import is **enumeration** — knowing which zones exist at the provider so the operator (or a bulk-wrapper command) can iterate Import calls.

Enumeration is also already a strict contract: `IaCProviderEnumerator.EnumerateAll(resourceType)` (optional service per `iac.proto:64`). DO's implementation today handles `infra.spaces_key` only — DNS is a gap across every provider. Closing that gap is Phase 1.

The cycle-1 contract refactor was solving the orthogonal problem of policy-gate code coupling. That problem is real (workflow-plugin-infra imports libdns/* for dnsgate/dnsaudit/admincli) but does not block import. Phases 3a + 3b solve it the right way: move the code into wfctl where it has direct access to provider resource drivers via the existing engine-host gRPC client.

## Global Design Guidance

Source: `/Users/jon/workspace/docs/design-guidance.md`

| guidance | design response |
|---|---|
| wfctl is user-facing CLI; no new bare binaries | Bulk-import helper ships as `wfctl infra import-all` builtin; policy admin commands move to `wfctl dns-policy <subcommand>` builtin |
| Plugin contracts via typed gRPC; no `structpb`/`Any` | Uses existing strict `IaCProvider.Import` + `IaCProviderEnumerator.EnumerateAll`; no new contract |
| Plugin Contracts & Extensibility — contracts in orchestrator plugin only when peer plugins need them | No new cross-plugin contract; engine-native primitives sufficient. The guidance section added 2026-05-26 stands as durable knowledge for future designs but does not need to be exercised here. |
| Reuse over rebuild | Reuses existing `wfctl infra import`, `IaCProviderEnumerator`, provider plugins' existing resource drivers |
| libdns/cloud-sdks isolated in `internal/<provider>/` | After Phase 3b, workflow-plugin-infra is libdns-free; libdns lives only inside provider plugin `internal/drivers/` packages where it was always meant to be |
| Cross-driver parity (≥2 drivers before declaring done) | Phase 1 covers 4 providers (DO, CF, NC, Hover); Phase 4 validates parity via cross-provider scenarios |
| No mock-first development | Phase 4 scenarios exercise real plugin loading + real engine + (Phase 5) real cloud accounts for DO + Hover |
| Secrets never logged | Each provider plugin reads its own creds from module config (existing pattern); admincli migration drops the per-command `--token` flag, eliminating one cred-flow exposure |
| Audit trail for state-mutating ops | dnsaudit JSONL trail moves with admincli into wfctl; appended at apply-time + admin command time |
| Goreleaser v2 + ldflag Version | All affected repos already conform |
| Plugin minEngineVersion + capabilities populated | EnumerateAll capability advertised by each provider plugin via existing capability flag pattern |

## Phases

The work decomposes into five phases. Phases 1 and 2 are independent; Phase 3 depends on Phase 1+2 only via shared verification; Phase 4 depends on Phase 1 (it imports state via the Phase 1 primitives); Phase 5 is a pointer to a separate design that consumes Phases 1-4.

### Phase 1 — Provider EnumerateAll for `infra.dns` (4 parallel PRs)

Each provider plugin grows `IaCProviderEnumerator.EnumerateAll(ctx, "infra.dns")`:

- `workflow-plugin-digitalocean`: `internal/provider.go` `EnumerateAll` switch adds `case "infra.dns"` calling godo `Domains.List` (paginated). Each `*godo.Domain` becomes `*interfaces.ResourceOutput` with `Outputs.zone=<name>`, `Outputs.zone_id=<name>` (DO uses domain name as ID), `Outputs.ttl=<ttl>`.
- `workflow-plugin-cloudflare`: corresponding `EnumerateAll` impl using `cloudflare-go/v7` `client.Zones().List`. Output includes `Outputs.zone_id=<cf-zone-uuid>`, `Outputs.account_id`.
- `workflow-plugin-namecheap`: uses `go-namecheap-sdk` `Domains.GetList`. Output includes `Outputs.zone=<name>`, `Outputs.is_using_our_dns` (NC's authority flag).
- `workflow-plugin-hover`: uses `pkg/hoverclient` (extracted last session) `ListDomains`. Output includes `Outputs.zone=<name>`, `Outputs.expires_at`.

Each ResourceOutput's `ProviderID` is set to the value the provider plugin's `IaCProvider.Import` expects for that zone (matches the contract precedent: DO uses domain name as cloud ID; CF uses zone UUID; etc. — confirm per-provider during implementation).

ContractRegistry advertisement (already wired in each plugin) gains the EnumerateAll entry per the optional-service pattern.

Per-provider live integration test (env-gated `INFRA_DNS_ENUMERATE_LIVE=1`) exercises EnumerateAll against a real account; runs on self-hosted runner.

**PRs**: 4. Each isolated to its provider plugin repo. Parallel-safe.

### Phase 2 — wfctl bulk import helper (1 PR, workflow)

`workflow/cmd/wfctl/infra.go` grows a sibling to `runInfraImport`:

```
wfctl infra import-all --type <resourceType> --provider <providerName> [--config <path>] [--env <name>] [--dry-run]
```

Behavior:
- Resolves the provider via `resolveIaCProvider`.
- Calls `provider.EnumerateAll(ctx, resourceType)`.
- For each `ResourceOutput`: synthesizes a `ResourceSpec` (Name = sanitized zone name, Type = resourceType, ProviderID = output.ProviderID); calls `provider.Import(ctx, ProviderID, resourceType)`; saves via the existing state store.
- `--dry-run` prints the would-be imports without persisting; non-zero exit if any zone import fails (per-zone errors logged + summary at end).
- Failure isolation: continue on per-zone error; mark in summary; exit non-zero if ≥1 failure.

This is a thin wrapper around existing primitives; no new gRPC, no new contract. Lives in wfctl as a builtin alongside `infra import`.

**PR**: 1 (workflow repo).

### Phase 3 — DNS policy code relocation (workflow-plugin-infra → wfctl)

**Goal**: workflow-plugin-infra carries no libdns/* deps. Policy-gate concerns (admin commands, gate hook, audit log) live in wfctl where the engine has direct provider-driver access.

**Phase 3a — wfctl additions (1 PR, workflow)**:

- New package `workflow/dns/policy` (relocated from `workflow-plugin-infra/internal/dnspolicy/`): pure-Go policy parser/serializer. No I/O. Imported by both wfctl commands and the apply-gate hook.
- New package `workflow/dns/gate` (relocated from `workflow-plugin-infra/internal/dnsgate/`): apply-time policy check. Operates on `ResourceSpec` + provider `Driver.Read` to fetch current TXT records. Registered as a pre-apply hook for resource type `infra.dns` in the `wfctl infra apply` flow.
- New package `workflow/dns/audit` (relocated from `workflow-plugin-infra/internal/dnsaudit/`): JSONL append. Trail path migrates from `${XDG_STATE_HOME}/wfctl/plugins/infra/dns-audit.jsonl` → `${XDG_STATE_HOME}/wfctl/dns-audit.jsonl` (one-time migration: read old path on first run, append into new path, atomic move).
- New wfctl commands (siblings to `wfctl infra`):
  - `wfctl dns-policy show --zone <zone> --provider <name>` (replaces `wfctl infra-dns policy show`)
  - `wfctl dns-policy set --zone <zone> --provider <name> --owner <name> [--delegate ...]` (replaces `wfctl infra-dns set-policy`)
  - `wfctl dns-policy transfer-ownership --zone <zone> --provider <name> --from <owner> --to <owner>` (replaces `wfctl infra-dns transfer-ownership`)
  - `wfctl dns-policy drift --zone <zone> --provider <name>` (replaces `wfctl infra-dns drift`)
- Each wfctl command:
  - Resolves provider via `resolveIaCProvider`.
  - Resolves `ResourceDriver("infra.dns")` from the provider.
  - For policy R/W: calls `driver.Read(zoneRef)` → parses TXT records via `workflow/dns/policy` → mutates → calls `driver.Update(zoneRef, updatedSpec)`. Operates against the EXISTING strict-contract `IaCResourceDriver` surface — no new RPCs.
- Update `wfctl infra apply` to invoke the gate hook for any `infra.dns` resource action (Create/Update). Gate failure → action aborted before driver call.

**Phase 3b — workflow-plugin-infra strip (1 PR, workflow-plugin-infra)**:

DELETE:
```
internal/dnspolicy/             (relocated to workflow/dns/policy)
internal/dnsgate/               (relocated to workflow/dns/gate)
internal/dnsaudit/              (relocated to workflow/dns/audit)
internal/admincli/              (commands moved to wfctl)
internal/dnsprovider/           (entire dir — libdns wrappers no longer used)
```

DROP from `go.mod`:
```
github.com/libdns/libdns
github.com/libdns/cloudflare
github.com/libdns/namecheap
github.com/libdns/digitalocean
github.com/libdns/route53
github.com/libdns/googleclouddns
github.com/libdns/azure
github.com/libdns/godaddy   (if present after v2)
```

The `infra.dns_record` typed step's handler at `internal/plugin.go:150` (caller-list miss from cycle-1 adversarial) — that handler currently calls `dnsprovider.NewAdapter` to perform per-step DNS record mutation. Migration: rewrite the step handler to dispatch via the engine's `provider.ResourceDriver("infra.dns")` path. The step receives a target provider name + zone; resolves the driver via the engine context (the same way other typed steps resolve providers — pattern lives in `workflow/module/pipeline_step_*.go` for IaC-touching steps). After the rewrite, the step handler holds no libdns/* import.

Update `plugin.json`:
- Remove `cliCommands` entry for `infra-dns` (commands moved to wfctl builtins).
- Remove the now-unused module/step factory registrations.

This PR depends on Phase 3a being merged (the relocated code must exist in wfctl before workflow-plugin-infra can strip it).

**Phase 3 PRs**: 2, sequential (3a then 3b).

### Phase 4 — workflow-scenarios DNS orchestration tests

`workflow-scenarios` grows new scenarios under `dns/`:

1. **`dns/import-export-roundtrip/`** — for each provider (DO/CF/NC/Hover): config YAML declaring an `infra.dns` resource → `wfctl infra import-all --provider <p> --type infra.dns` → assert state file populated → `wfctl infra plan` against same config produces a NoOp diff (proves import shape matches what the provider would Read back).

2. **`dns/cross-provider-transfer/`** — full zone migration: DO holds `example-old.test` with N records → export state → rewrite state w/ provider=cloudflare → `wfctl infra apply` against CF → assert all N records present at CF with identical (type, name, data, ttl) tuples. Per-record-type matrix: A, AAAA, CNAME, MX, TXT, SRV, CAA, NS. (Excludes provider-specific extras intentionally — those are documented to be lossy across providers.)

3. **`dns/delegation/`** — parent zone at DO holds NS records for `child.example.test` pointing to CF nameservers → CF holds `child.example.test` zone with managed A/AAAA records → both managed in same `wfctl infra apply` run → assert dig-resolves correctly (or simulated equivalent for scenario test runner). Tests the "delegation from one provider to another with records managed within" pattern.

Test runner gating: scenarios that require live cloud creds opt in via env (`WORKFLOW_SCENARIO_LIVE_DO=1` etc.). Local scenarios use stubbed provider plugin processes serving canned EnumerateAll/Import responses (workflow-scenarios already has a stub-plugin harness pattern from prior IaC scenario work).

**PR**: 1 (workflow-scenarios), but landed AFTER Phase 1 (needs EnumerateAll across providers).

### Phase 5 — gocodealone-dns catalog refresh (pointer)

This phase is a pointer, not in-scope work for this plan. A separate design doc will be filed at `gocodealone-dns/docs/design/2026-05-26-catalog-refresh-design.md` covering:

- Drop the doctl shell-script importer in `.github/workflows/import-dns.yml`.
- Rewrite the workflow to call `wfctl infra import-all --type=infra.dns --provider=<p>` for each provider configured for the catalog.
- Restructure on-disk layout: `dns/<provider>/<domain>/state.json` (workflow ResourceState shape) — supersedes current `dns/<domain>/records.yaml`+`metadata.yaml`.
- Backfill 16 existing DO zones into the new layout.
- Initial activation: DO + Hover (no new creds needed today; both already wired into the gocodealone DO account + Hover account).
- Pending: CF + NC creds provided by operator later → activate those providers in same workflow.

Phase 5 design happens AFTER Phases 1-4 are merged (it consumes their primitives). When Phase 5 starts, this design's owner will scaffold the gocodealone-dns design doc + open the implementation plan there. **Zero gocodealone-dns references appear anywhere in workflow-plugin-infra or workflow or workflow-scenarios; the business-domain boundary is honored.**

## PR Grouping

| PR | repo | phase | scope | depends on |
|---|---|---|---|---|
| 1 | workflow-plugin-digitalocean | 1 | EnumerateAll("infra.dns") | — |
| 2 | workflow-plugin-cloudflare | 1 | EnumerateAll("infra.dns") | — |
| 3 | workflow-plugin-namecheap | 1 | EnumerateAll("infra.dns") | — |
| 4 | workflow-plugin-hover | 1 | EnumerateAll("infra.dns") | — |
| 5 | workflow | 2 | `wfctl infra import-all` | PRs 1+2+3+4 (for cross-provider parity smoke) — can land before, but its e2e proof needs at least one provider's PR landed |
| 6 | workflow | 3a | dns/policy + dns/gate + dns/audit packages + wfctl dns-policy commands + apply-time gate hook + migrate `infra.dns_record` step handler to engine driver path | PRs 1+2+3+4 (step handler migration verified against new EnumerateAll path) |
| 7 | workflow-plugin-infra | 3b | strip libdns + admincli + dnspolicy/gate/audit + cliCommands removal | PR 6 |
| 8 | workflow-scenarios | 4 | dns/import-export-roundtrip, dns/cross-provider-transfer, dns/delegation | PRs 1+2+3+4+5 (needs import-all primitive) |
| (deferred) | gocodealone-dns | 5 | separate design + plan; not in this plan | PRs 5+8 |

8 PRs total. PRs 1-4 parallel. PR 5 can land in parallel with 1-4 but its e2e proof needs ≥1 provider PR. PR 6 sequenced after 1-4. PR 7 sequenced after 6. PR 8 sequenced after 5.

## Data flow (bulk import path, Phase 1+2)

```
user runs: wfctl infra import-all --provider digitalocean --type infra.dns --config infra.yaml
    ↓
runInfraImportAll() in workflow/cmd/wfctl/infra.go
    ↓
resolveIaCProvider(ctx, "digitalocean", providerCfg)
    ↓ host loads workflow-plugin-digitalocean as gRPC client
provider.EnumerateAll(ctx, "infra.dns")
    ↓ wfctl→plugin process gRPC call (strict contract iac.proto:64)
DOProvider.EnumerateAll → godo Domains.List → []*ResourceOutput
    ↓ returned to wfctl
for each output:
    spec = ResourceSpec{Name: sanitized(output.Outputs.zone), Type: "infra.dns", ProviderID: output.ProviderID}
    state = provider.Import(ctx, spec.ProviderID, "infra.dns")
        ↓ wfctl→plugin process gRPC call (strict contract iac.proto:32)
    DOProvider.Import → godo Domains.Records.List → ResourceState w/ AppliedConfig.records
        ↓ returned to wfctl
    store.SaveResource(ctx, state)
        ↓ IaC state backend write
```

Zero new RPCs. Zero new plugin contracts. Existing engine surface end-to-end.

## Data flow (policy-gate path, Phase 3)

```
user runs: wfctl dns-policy set --zone example.test --provider digitalocean --owner ratchet --delegate multisite:www,admin
    ↓
runDNSPolicySet() in workflow/cmd/wfctl/dns_policy.go
    ↓
resolveIaCProvider(ctx, "digitalocean", providerCfg)
    ↓
driver, _ := provider.ResourceDriver("infra.dns")
    ↓
current := driver.Read(ctx, zoneRef)
    ↓ gRPC to plugin (strict contract iac.proto IaCResourceDriver.Read)
existing := parsePolicyTXT(current.Outputs["records"])
    ↓ workflow/dns/policy library, pure Go
updated := policy.Apply(existing, owner, delegates)
    ↓ pure Go
newSpec := overlayTXTOnSpec(current, updated.SerializeToTXT())
    ↓
driver.Update(ctx, zoneRef, newSpec)
    ↓ gRPC to plugin (strict contract iac.proto IaCResourceDriver.Update)
audit.AppendJSONL(action="set-policy", zone, owner, delegates, timestamp)
    ↓ workflow/dns/audit library
```

Same RPC surface (`IaCResourceDriver.Read` + `Update`), already part of the strict contract. The policy logic is pure Go that operates on the resource-level spec.

## Multi-Component Validation

- **Phase 1**: each provider plugin's EnumerateAll unit-tested with stubbed cloud client + paginated fixture. Live test gated on env. Cross-driver parity verified by Phase 4 scenarios.
- **Phase 2**: `wfctl infra import-all` smoke-tested against at least one Phase-1-completed provider in real plugin loading mode (Docker compose stack or equivalent). NOT mock-only.
- **Phase 3a/b**: end-to-end test that `wfctl dns-policy set` against a stubbed provider plugin (workflow-plugin-cloudflare in a test mode w/ httptest backend) succeeds; resource driver `Read` + `Update` calls observed at the plugin boundary. Migration verification: `infra.dns_record` step handler exercised in a workflow pipeline against a stub provider; asserts the step works after libdns removal.
- **Phase 4**: scenarios themselves ARE the multi-component proof — they exercise the real boundary.
- **Phase 5**: gocodealone-dns design specifies its own validation (out of scope here).

## Security Review

- Phase 1: EnumerateAll uses provider plugin's existing initialized client; reads creds from the plugin's standard config block (`iac.provider.<name>.config`); never crosses cred values across the contract wire. Live tests on self-hosted runner.
- Phase 2: import-all wrapper is privilege-equivalent to running `wfctl infra import` per zone; no new attack surface.
- Phase 3a/b: the policy-gate move into wfctl tightens cred boundary. Old admincli commands took `--token` flags (creds passed inline on CLI); new wfctl commands read provider config from the resolved infra config file (`infra.yaml`), so creds live in one place and never pass through process arguments. dnsaudit JSONL trail continues to record state-mutating actions (set-policy, transfer-ownership); read commands (show/drift) skip the audit. Trail path migration is one-time and additive.
- Phase 3b: removing libdns deps from workflow-plugin-infra removes one transitive supply-chain exposure surface (7+ libdns sub-libraries).
- Phase 4: scenarios that run against live providers gated by env vars (`WORKFLOW_SCENARIO_LIVE_*=1`). Cred secrets sourced from GH org-level secrets, never embedded in scenario YAML.

## Infrastructure Impact

- 7 in-scope PRs across 4 repos (1 workflow, 2 workflow-plugin-infra+the policy relocation has separate phases, 4 provider plugins, 1 workflow-scenarios) + 1 pointer to gocodealone-dns.
- Each provider plugin gets a minor version bump (new capability advertised).
- workflow gets a minor version bump (new wfctl subcommands + relocated dns/policy/gate/audit packages).
- workflow-plugin-infra gets a major version bump (capability surface shrinks: cliCommands removed + module/step factories may change; concrete diff at Phase 3b time).
- No DB migrations. No new cloud resources. No production deploy.
- Live tests require self-hosted runner static egress (NC IP allowlist + responsible-rate-limit posture).
- One state-trail path migration in Phase 3a (`${XDG_STATE_HOME}/wfctl/plugins/infra/dns-audit.jsonl` → `${XDG_STATE_HOME}/wfctl/dns-audit.jsonl`); first wfctl run after upgrade appends old trail to new + leaves old file in place for one release cycle then removed in a follow-up.

## Rollback

- PR 1-4 (provider EnumerateAll): per-PR revert. Each provider's EnumerateAll is additive; revert removes the capability advertisement and the impl. No downstream caller is broken because Phase 2's `import-all` will simply report "EnumerateAll not supported by provider" for any reverted provider.
- PR 5 (wfctl import-all): revert removes the subcommand. `wfctl infra import` continues to work for per-zone import.
- PR 6 (wfctl dns-policy + dns packages): revert removes the new commands + apply-time gate hook + relocated packages. workflow-plugin-infra's existing admincli + dnspolicy/gate/audit code is still in place (PR 7 hasn't run yet) — system reverts to the prior state cleanly.
- PR 7 (workflow-plugin-infra strip): revert restores libdns deps + admincli + dnspolicy/gate/audit. Coupled with reverting PR 6, the system returns to pre-refactor behavior. Order matters: PR 7 must be reverted BEFORE PR 6 if both are being rolled back (else workflow-plugin-infra has policy code that doesn't compile against missing imports).
- PR 8 (scenarios): revert removes scenarios. No runtime impact.

## Assumptions

- A1: `IaCProviderEnumerator.EnumerateAll` is the right enumeration RPC for DNS zones (verified: `iac.proto:64-67`; existing DO EnumerateAll path proves the pattern works for "infra" resource types).
- A2: `IaCProvider.Import(ctx, cloudID, resourceType)` returns enough state to round-trip through `IaCResourceDriver.Read` for `infra.dns` (verified: CF + NC + DO all already implement Import for infra.dns).
- A3: wfctl can register new builtin commands (`wfctl dns-policy *`, `wfctl infra import-all`) without conflict with the reserved-command list. `dns-policy` and `import-all` are not in `reservedCLICommands` map (`workflow/cmd/wfctl/plugin_cli_commands.go:14-43`) — `import-all` is a subcommand of `infra`, not a top-level, so it does not even need to clear that map.
- A4: `wfctl infra apply` has a hook point for pre-action gates per resource type. Needs verification at Phase 3a implementation start; if not present, Phase 3a grows a minimal hook-registry contribution. The pattern is small.
- A5: workflow-scenarios test runner can drive `wfctl infra import-all` end-to-end against a stub provider plugin (matches existing IaC scenario harness pattern).
- A6: The provider plugins' `IaCProvider.Import` is implemented for `infra.dns` in such a way that the returned `ResourceState.AppliedConfig.records` matches the shape a subsequent `IaCResourceDriver.Read` would produce. If lossy, Phase 4's import-export-roundtrip scenario will fail — that failure is informative, not blocking the design (it surfaces a bug in the provider's Import impl).

## Non-Goals

- New gRPC contract for DNS provider operations. The strict-contract `IaCProvider` + `IaCProviderEnumerator` cover the import path.
- Peer-plugin dispatch from within a plugin process. Not needed for any in-scope work; the engine (wfctl) drives all peer plugin calls.
- Workflow SDK extensions (`InvokeService` on `EngineCallbackService`, `AdditionalServices` hook on `IaCServeOptions`). Adversarial cycle 1 identified these as load-bearing for the dropped contract architecture; not needed for the engine-native approach.
- aws/azure/gcp/godaddy/r53 EnumerateAll implementations. Same pattern can be applied per provider in follow-up plans; not blocking import for the four providers in scope.
- Cryptographic plugin-identity attestation (belongs to workflow-plugin-supply-chain).
- gocodealone-dns catalog refresh (Phase 5 — separate design, separate plan).
- `infra.dns` IaC resource lifecycle changes (Create/Read/Update/Delete already implemented per provider; no change needed).

## Open Questions

- O1: Per-provider zone-record-set field shape. For example, DO records have `(weight, port, flags, tag)` extras for SRV/CAA; CF has `proxied` boolean; NC has `email_type` zone-level field. The existing `IaCProvider.Import` impl in each provider plugin already decides how to surface these in `ResourceState.AppliedConfig`. Phase 4's import-export-roundtrip scenario validates whether the chosen shape round-trips losslessly. Bugs in per-provider Import impls → file as follow-up issues in respective repos; not blocking this design.
- O2: Phase 3a's apply-time gate hook — exact wiring point in `wfctl infra apply`. Two candidate locations: (i) inside the per-action loop where each `PlanAction` is dispatched to its driver; (ii) at PlanResolution time, surfacing gate failure as a plan-level error. Decide at Phase 3a implementation start. Either is fine; (i) is more straightforward.
- O3: workflow-plugin-infra `infra.dns_record` step handler migration (caller-list miss from cycle-1 adversarial). After Phase 3b, the step handler needs to dispatch DNS record mutation via the engine's resource driver path. Two options: (i) the step handler resolves a resource driver via the engine context at step execution time (similar pattern in other typed steps that touch IaC); (ii) the step is repointed at a higher-level construct like `wfctl infra apply` for individual record changes. Choose at Phase 3b implementation start; default to (i) unless precedent supports (ii).

## Top doubts (self-challenge)

1. **Phase 3 is genuinely heavier than Phases 1+2.** Phases 1+2 are 5 small, parallelizable PRs. Phase 3 is 2 sequential PRs touching workflow's command surface + workflow-plugin-infra's plugin surface. Sequencing risk: if Phase 3a's wfctl additions miss a corner case in the apply-gate wiring (Open Question O2), Phase 3b can't ship and workflow-plugin-infra is stuck holding both old + new code paths. Mitigation: Phase 3a's PR must include the gate-hook validation tests; if those don't pass, Phase 3b waits.

2. **The `infra.dns_record` step handler migration is structurally similar to the cycle-1 caller-list miss.** I need to grep ALL of workflow-plugin-infra for callers of `dnsprovider.*` and `dnspolicy.*` before Phase 3b's PR description claims completeness. Cycle 1's design said "admincli is the only caller" — false. Cycle 2 must not repeat that. Mitigation: Phase 3b's PR description block-lists every grep'd caller upfront.

3. **Provider EnumerateAll vs Import shape parity** (Open Question O1). Each provider plugin's Import is a different code path than the (new) EnumerateAll. They share the same return type but were written at different times by different authors. Phase 4's import-export-roundtrip scenario surfaces parity bugs; if any provider returns inconsistent shapes, that provider's plugin needs a fix before Phase 4 can declare done. This is a real risk but it's the right test — it ensures we don't ship a state that can't be applied back.

## Change Log

| Date | Author | Change |
|---|---|---|
| 2026-05-26 | codingsloth@pm.me | cycle 1 — peer-contract architecture (DNSProvider gRPC service in workflow-plugin-infra w/ peer-dispatch via EngineCallbackService). Adversarial review FAIL: 3 Criticals (peer-dispatch RPC absent; missed caller in `infra.dns_record` step handler; reserved-command name collision). |
| 2026-05-26 | codingsloth@pm.me | cycle 2 — full architectural pivot. Drops new contract entirely. Uses engine-native `wfctl infra import` + `IaCProviderEnumerator.EnumerateAll` strict-contract path. Restructures as 5 phases: (1) EnumerateAll across 4 providers, (2) wfctl import-all wrapper, (3) policy code relocated workflow-plugin-infra → wfctl, (4) workflow-scenarios DNS orchestration, (5) pointer to separate gocodealone-dns design. |
