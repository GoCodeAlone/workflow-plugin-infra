# Retro: DNS import + provider decoupling cascade

**PRs:** 9 across 6 repos
**Merged:** 2026-05-27
**Branch (this repo):** `refactor/strip-dns-libdns-2026-05-26T1900` (PR 8 = workflow-plugin-infra#25)
**Design:** `docs/plans/2026-05-26-dns-provider-contract-design.md`
**Plan:** `docs/plans/2026-05-26-dns-provider-contract.md`
**Related ADRs:** none recorded (design backports sufficed; no manifest amendments)

## Cascade scope

- workflow-plugin-digitalocean#177 — EnumerateAll(infra.dns); v2.1.6
- workflow-plugin-cloudflare#9 — EnumerateAll(infra.dns); v0.1.1
- workflow-plugin-namecheap#14 — EnumerateAll(infra.dns); v0.1.4
- workflow-plugin-hover#27 — ListDomains + EnumerateAll; v0.4.0
- workflow-registry#165 — pin-bump 4 DNS providers + cloudflare CREATE
- workflow#786 — wfctl infra import-all bulk wrapper (Phase 2)
- workflow#787 — wfctl dns-policy + relocate dns/{policy,gate,audit} + OnBeforeAction (Phase 3a)
- workflow-plugin-infra#25 — strip libdns + admincli + dns packages + remove infra.dns_record step; v1.0.0 (Phase 3b)
- workflow-scenarios#27 — DNS orchestration scenarios 89/90/91 + stub IaCProvider gRPC plugin (Phase 4)

Plus backport: workflow-plugin-namecheap#15 + workflow-plugin-cloudflare#10 (NC+CF ManifestProvider gap that surfaced post-tag; unblocked PR 5).

Plus post-merge auto-sync: workflow-registry#166 (workflow-plugin-infra v1.0.0 pin-bump).

## Adversarial-review findings, scored

### Design phase (cycles 1 → 2 → 3 → 3.5)

| Cycle | Finding | Severity | Outcome |
|---|---|---|---|
| 1 | Peer-dispatch RPC (`EngineCallbackService.InvokeService`) does not exist; entire DNSProvider-contract architecture has no SDK path | Critical | Prescient — cycle 2 pivoted to engine-native primitives (`wfctl infra import` already covered the use case) |
| 1 | `internal/plugin.go:150` also calls `dnsprovider.NewAdapter`; original "admincli is sole caller" claim falsified | Critical | Prescient — Phase 3b had to handle the step handler; cycle 3.5 chose to remove `infra.dns_record` step entirely rather than rewrite |
| 1 | `plugin` + `infra` are reserved wfctl command names; proposed CLI surface impossible | Critical | Prescient — cycle 2 used `wfctl infra import-all` (subcommand) + `wfctl dns-policy` (new top-level not in reserved list) |
| 2 | Hover `ListDomains` method missing on pkg/hoverclient | Critical | Prescient — PR 4 explicitly added `ListDomains` |
| 2 | `ApplyPlanHooks` has no pre-action gate slot | Critical | Prescient — PR 7 Task 19 added `OnBeforeAction` as first-class structural change |
| 2 | `DNSRecordStepConfig.provider_creds` proto field stranded by step-handler migration; `upsert` semantic mismatch | Critical | Prescient — cycle 3.5 chose step removal entirely (per cycle-3 I-NEW-1 finding that `engine.ResolveProvider` doesn't exist for plugin step handlers) |
| 3 | `wfctl dns-policy` builtin conflicts with design-guidance §CLI ("CLI surface attaches via plugins, not by editing wfctl") | Important | Resolved upfront — design-guidance rev 3 clarified the cross-cutting-orchestrator vs capability-scoped placement nuance in the same commit |
| 3 | Scenario lossiness charter is record-type-unaware; DO SRV/CAA extras would falsely apply globally | Important | Resolved upfront — cycle 3.5 restructured charter to `(provider, record_type, field)` triples |
| 3 | NS records included in cross-provider transfer matrix would always fail (apex NS is provider-managed) | Important | Resolved upfront — cycle 3.5 explicitly excluded NS from transfer matrix |
| 3 | `engine.ResolveProvider` for `infra.dns_record` step handler architecturally impossible (no SDK primitive in plugin-process step handler context) | Important | Resolved upfront — cycle 3.5 elevated to explicit design constraint; step removed instead of migrated |
| 3.5 | `OnBeforeAction` error tier (fatal vs best-effort) unspecified | Important | Resolved upfront — specified FATAL semantics; plan + impl honored |

### Plan phase (cycles 1 → 2 → 3 → 4 → 5 → 6 → 7)

The plan loop ran 7 cycles. Each cycle caught new concrete SDK-fact / repo-state errors that would have been compile-time or runtime failures.

| Cycle | Findings (count) | Outcome |
|---|---|---|
| 1 | 5 Criticals (CF/NC/DO SDK type lookups + Hover path + scenario `-o` flag) + 6 Importants (interface assertion, state-store type, var name, runtime-launch, workflow-registry omission, scenario directory layout) | All Prescient — would have caused compile errors or implementer rework |
| 2 | 3 new Criticals (wrong runtime interface assertion, nonexistent `NewArrayAutoPagerFromSlice` constructor, nonexistent `wfctl plugin registry-validate` command) + 2 Importants (--state-file flag, dumpStateToFile unimplemented) | All Prescient — distinct issue classes vs cycle 1 |
| 3 | 1 Critical (`wfctl infra apply --provider=stub-A` invalid flag) + 3 Importants (manifest path/format, sleep-5 race, translate-state-to-config.py underspecified) | All Prescient |
| 4 | 2 Criticals (manifest paths still wrong w/ `workflow-plugin-` prefix; cloudflare manifest missing; `IaCProvider.Import` return type `*ResourceState` not `*ResourceOutput`) + 2 Importants (jq `.applied_config.records`, --provider semantic) | All Prescient — return type fix particularly load-bearing for stub plugin |
| 5 | 2 Criticals (`resolveProviderModuleByName` wrong return value; `loadConfig` nonexistent function) + 1 Important (`--plugin-dir` flag position) | All Prescient — helper code rewrite |
| 6 | 3 Criticals (compile errors in cycle-5 helper: `ResolveForEnv` returns bool not error; `ExpandEnvInMapPreservingKeys` single-value; pointer-receiver range-by-value) | All Prescient — cycle 7 mirrored existing precedent line-for-line |
| 7 | 0 Critical/Important; 1 Minor (redundant flag) | Converged |

## Gate misses

| Issue | Gate that missed | Why it slipped | Fix idea |
|---|---|---|---|
| PR 9 (workflow-scenarios) round 1 C1: scenarios used `backend: memory` which `infra_state_store.go` doesn't recognize | spec-reviewer | Spec-reviewer ran `bash -n` (syntax check) instead of actually executing scenario; design referenced "filesystem" generically; plan didn't pin the backend value | spec-reviewer for scenario PRs should run at least one scenario end-to-end before approving; add to spec-review checklist for IaC test scenarios |
| PR 9 round 1 C2: stub plugin layout `/tmp/dns-stub` (single file) doesn't match wfctl's `findIaCPluginDir` expectation (`$dir/<name>/<name>` + `plugin.json`) | adversarial-design-review (plan) | Plan code block showed `go build -o /tmp/dns-stub ../../lib/dns-stub-plugin/` without addressing wfctl's plugin discovery layout; adversarial reviewer focused on SDK signature correctness, not on the runtime plugin-loader contract | Add "plugin-loader layout" to plan-phase bug-class checklist for any PR that spawns external plugin processes |
| PR 9 schema validation: scenarios 89/91 lacked an entry-point module (e.g., `http.server`) which workflow/schema/validate.go::checkEntryPoints requires | adversarial-design-review (plan) | Same blind spot — plan didn't note workflow's entry-point requirement | Add "workflow config validation rules" to scenario-plan bug-class checklist |
| PR 6 (wfctl import-all) Lint failure: `fmt.Sprintf` w/ no format directives (staticcheck S1039) | local pre-commit / verification-before-completion | Implementer didn't run `golangci-lint run` locally before pushing | implementer prompt should explicitly include `golangci-lint run` as a pre-push verification step for any workflow-repo PR |
| PR 7 (workflow dns-policy) Lint failure: `nilerr` in `dns/audit/audit.go` migration helper swallowed errors | local pre-commit / verification-before-completion | Same as above + the nilerr lint rule wasn't on the implementer's radar | Same fix as above; the lint configuration has been stable for months, this is a routine pre-push check |
| Backport: workflow-plugin-{namecheap,cloudflare} Release pipelines missing ManifestProvider — verify-capabilities fails at tag-push time | adversarial-design-review (design) | Design assumed all 4 provider plugins had working release pipelines; v1+v2 cascade memory note mentioned a ManifestProvider fix for hover but didn't generalize | Pre-cascade gate: verify each touched provider plugin's most-recent Release workflow run was green; flag any failing as a prerequisite to the cascade |
| Locked plan was lost from workflow-plugin-infra/master because the design branch never merged; only PR 25 (with the plan markdown but not the `.scope-lock` sidecar) landed | finishing-a-development-branch | The plan + design + adversarial-review reports lived on a separate branch from the per-PR feature branches; PR 25 carried the plan but not the lock file | scope-lock helper should also generate a `chore/scope-lock-init` PR against the orchestrator repo at lock time, OR the locked plan should always live on master from cycle-2 of writing-plans onward |

## Missed skill activations

| Gate | Fired? | Notes |
|---|---|---|
| project-design-guidance | ✓ (pre-design) | Loaded /Users/jon/workspace/docs/design-guidance.md; updated rev 2 + rev 3 in same change set |
| brainstorming | ✓ | Single batch (4 questions); user volunteered subsequent direction |
| adversarial-design-review (design) | ✓ (4 cycles: 1, 2, 3, 3.5) | Convergence took 4 cycles due to architectural pivot from peer-contract to engine-native |
| writing-plans | ✓ | Initial draft + 6 revisions |
| adversarial-design-review (plan) | ✓ (7 cycles: 1-7) | Plan loop ran longer than design loop because each cycle revealed new SDK-fact mismatches; cycles 4-7 were verified-against-actual-code fixes |
| recording-decisions | did not fire | Design backports + manifest unchanged → no ADRs needed; this is correct per skill |
| alignment-check | ✓ (2 cycles) | Cycle 1 flagged 4 drift items (design PR count, --dry-run exit semantics, capability advertisement task, DO zone_id); cycle 2 PASS |
| scope-lock | ✓ | Applied at lock time; later marked Complete via `scope-lock-complete` helper (after re-creating lock file on master since original sat on unmerged design branch) |
| subagent-driven-development | ✓ | Team `dns-provider-cascade` w/ 2 implementers + spec-reviewer + code-reviewer |
| pr-monitoring | partial | No `autodev:pr-monitoring` agent type available in env; manual CI polling via watchdog wakes |
| finishing-a-development-branch | not directly | PR creation happened inline by implementers; cascade-level finishing handled by team-lead's manual close-out + scope-lock-complete |
| post-merge-retrospective | ✓ (this doc) | |

## What worked

- **Architectural pivot caught at design cycle 1 saved weeks.** The original "DNSProvider gRPC contract + peer-dispatch" architecture would have required workflow SDK extensions that don't exist. Adversarial reviewer named the gap (`EngineCallbackService.InvokeService` is plugin→host, not host→peer-plugin) on first read. Cycle 2's full pivot to `wfctl infra import` + `IaCProviderEnumerator` reused entirely existing strict-contract surface — zero new RPCs, zero SDK changes.
- **Plan-phase adversarial loop sharpened code blocks to compile-ready precision.** 7 cycles caught SDK-fact errors at every layer: wrong type names (NC `DomainsGetListResult` → `DomainsGetListCommandResponse`), wrong return arities (godo `Links.CurrentPage`), nonexistent functions (`loadConfig`, `pagination.NewArrayAutoPagerFromSlice`, `wfctl plugin registry-validate`), wrong flag positions (`--plugin-dir` before subcommand), pointer-receiver range pitfalls. None of these would have been caught structurally; only by reading actual source.
- **Stub IaCProvider gRPC plugin (vs HTTP mock).** Adversarial reviewer cycle 2 flagged that workflow-scenarios's existing HTTP mocks would be insufficient for the IaC strict-contract path. PR 9 built a real `sdk.ServeIaCPlugin`-backed stub. Result: scenarios actually exercise the wire contract, catching the layout bug + the entry-point-module schema requirement on first end-to-end run.
- **Backport path (no manifest amendment) for NC+CF ManifestProvider fix.** When tag-push surfaced a pre-existing infrastructure gap, scope-lock §Backport Path correctly applied: design assumption (all 4 plugins have working Release pipelines) was disproved at execution time; manifest scope didn't change; no amendment ADR needed. Cascade unblocked in ~15 minutes.

## What didn't

- **spec-reviewer for scenarios PR ran `bash -n` not real execution.** Code-reviewer caught 2 Criticals (state backend + plugin layout) by actually running the scenarios. The spec-reviewer's checklist for IaC test PRs needs to include "run the scenario end-to-end before approving" not just "syntax check the scripts."
- **Lint failures recurred twice (PR 6 + PR 7).** Both repos had stable golangci-lint configurations; both implementer pushes skipped local lint. Implementer prompt should make `golangci-lint run` an explicit pre-push step for any workflow-repo PR, not a discretionary one.
- **Adversarial-design-review (plan) blind spot on runtime plugin-loader layout.** Plan code for the stub plugin built it as `go build -o /tmp/dns-stub` (single binary, no subdir). The cycle-1 reviewer focused on Go SDK signatures + caught many bugs there, but didn't think to verify the runtime plugin-discovery contract (`$WFCTL_PLUGIN_DIR/<name>/<name>` + `plugin.json` sidecar). Adding "plugin-loader runtime layout" to the plan-phase bug-class checklist would have caught this before round-2 review.
- **Lock file lived on unmerged design branch.** The scope-lock + design + plan + all adversarial-review reports were committed to a feature branch that was never merged to master in this repo (only the plan markdown shipped, via PR 25). Future autonomous runs should land the locked design+plan on master at lock time, OR the scope-lock helper should generate a separate `chore/scope-lock-init` PR.

## Plugin-level follow-ups

1. **Add "plugin-loader runtime layout" + "workflow config validation rules" to `adversarial-design-review` plan-phase bug-class checklist.** Any plan that spawns or loads an external plugin process should be checked against the host's discovery layout. Any plan with workflow config files should be checked against `workflow/schema/validate.go::checkEntryPoints` requirements. First retro signal; would become a trend if it recurs in another scenario PR.

2. **Add `golangci-lint run` to implementer prompt's pre-push verification list for workflow-repo PRs.** Twice in one cascade is signal; if this recurs in the next cross-workflow cascade, promote to a formal pre-push gate in `verification-before-completion`.

3. **Make scope-lock helper publish a `chore/scope-lock-init` PR.** Current behavior: lock file lives on the design branch + has to be re-created post-merge if the design branch isn't merged. Better behavior: lock file lands on master at scope-lock time via auto-merged PR.

4. **Add "spec-reviewer runs at least one scenario end-to-end" to subagent-driven-development prompt for IaC-test-scenario PRs.** First retro signal; promote to checklist if recurs.

5. **Pre-cascade gate: verify each touched plugin repo's last Release workflow was green.** When a cascade depends on tags being publishable, ManifestProvider-style gaps surface at the most expensive point (tag-push time). Cheap pre-flight: `gh run list --workflow Release --limit 1 --branch main` per repo, fail-fast if not SUCCESS.

## Project guidance updates

| Guidance file | Change | Reason |
|---|---|---|
| `/Users/jon/workspace/docs/design-guidance.md` | Already updated rev 2 + rev 3 during this cascade (added "Plugin Contracts & Extensibility" section + clarified CLI surface placement: capability-scoped → plugin cliCommands vs cross-cutting orchestrator → wfctl builtin) | Durable cross-design lesson: contracts live in their orchestrator plugin; ContractRegistry + EngineCallbackService.GetService + InvokeService is the peer-discovery surface; wfctl hosts cross-cutting orchestrator commands |
| `workflow-plugin-infra/CLAUDE.md` | no change | This repo-local file did not need updates beyond what the workspace-level guidance already covers |
