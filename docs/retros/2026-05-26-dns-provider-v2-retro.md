# Retro: DNS provider v2 cascade

**PRs:** #15 (Route53 + registry) → #17 (Namecheap) → #18 (GCP) → #21 (Azure) → #22 (GoDaddy)
**Merged:** 2026-05-26 (c7c1ac8 → 6d9982b → 2fbf8dd → f0509ad → 1900a55)
**Tag:** v0.3.0
**Branches:** all deleted post-merge
**Design:** docs/plans/2026-05-26-dns-provider-v2-design.md
**Plan:** docs/plans/2026-05-26-dns-provider-v2.md (scope-lock af0d875e/b835b5f6)
**Related ADRs:** none filed (decisions captured inline)
**Hover (PR 6) deferred:** workflow-plugin-hover#25 pkg/hoverclient extraction prerequisite

## Process metrics

| Phase | Cycles | Findings caught pre-merge |
|---|---|---|
| Design adversarial | 3 (cycle 3 PASS) | 4 Crit cred-key mismatches + 8 Imp + 4 Min |
| Plan adversarial | 4 (cycle 4 PASS) | 3 Crit (worktree path, PR contention, missing compile-check) + 6 Imp self-introduced via own fixes + 4 Min |
| Alignment-check | PASS first pass | 1 minor drift (Namecheap RRset test) — non-blocking |
| Scope-lock | 2026-05-26T06:34:58Z | hash b835b5f6 |
| Spec review | APPROVED 5/5 | 0 deviations from plan |
| Code review (caveman + general) | APPROVED 5/5 | 1 false Crit (type-assert in same-package test — correct) + 2 cosmetic |
| Copilot review | SKIPPED | per `feedback-copilot-review-broken-2026-05` |

## Adversarial-review findings, scored

### Design phase (3 cycles)

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| design c1 | Route53 cred keys `access_key`/`secret_key` invented; upstream uses `access_key_id`/`secret_access_key` | Critical | Resolved cycle 2 — verified via `go doc` against proxy.golang.org |
| design c1 | GCP `ServiceAccountJSON` = file path, NOT inline JSON | Critical | Resolved cycle 2 — defer inline-JSON to v3; `service_account_path` + ADC only |
| design c1 | GoDaddy two-key `api_key`+`api_secret` invented; actual single `api_token` (`<sso-key>:<sso-secret>`) | Critical | Resolved cycle 2 — single key + 50-domain restriction warning |
| design c1 | Namecheap `api_user` + `sandbox=true` invented; actual `user` + `api_endpoint` | Critical | Resolved cycle 2 |
| design c2 | Adapter pseudo-code returns `error`; actual `(recordID string, err error)` | Critical | Resolved cycle 3 |
| design c2 | Namecheap whole-zone-replace gating deferred to PR time | Critical | Resolved cycle 3 via upstream source spike — libdns/namecheap.SetRecords already Get-merge-Set per (name,type) |
| design c2 | Azure managed-identity path absent | Important | Resolved cycle 3 — godoc-cited triple-empty MI semantics |
| design c2 | Dual aliasing (`aws`+`route53` etc.) YAGNI | Important | Resolved cycle 2 — single canonical key |
| design c3 | A4 overclaim re per-provider scoping | Important | Resolved cycle 3 — weakened to honest framing (GoDaddy/Hover/Namecheap = account-wide) |

### Plan phase (4 cycles — heavier cycling due to self-introduced bugs)

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| plan c1 | Worktree path mismatch (`wf-infra-dns-provider-v2-route53` doesn't exist) | Critical | Resolved cycle 2 — single shared worktree |
| plan c1 | 6 PRs all touch `adapter.go` switch + `go.mod` (claimed parallel; actually serial) | Critical | Resolved cycle 2 — registry-map refactor via init() |
| plan c1 | No compile-time `var _ dnspolicy.Adapter = (*<adapter>)(nil)` check | Critical | Resolved cycle 2 |
| plan c2 | Helper-in-prod with anonymous iface arg doesn't lock libdns boundary | Critical | Resolved cycle 3 — moved helper to _test.go (still half-fix) |
| plan c3 | Helper-in-test STILL doesn't exercise adapter method (test calls helper, not `a.UpsertTXT(...)`) | Critical | Resolved cycle 4 — iface-typed `.provider` field; tests `&<prov>Adapter{provider: stub}.UpsertTXT(...)` |
| plan c3 | Hover speculative skeleton against unreleased package | Critical | Resolved cycle 4 — Step 0 godoc gate + TBD-pending markers |
| plan c4 | route53_test.go missing `libdnsr53` import | Minor | Resolved inline before lock |

## Gate misses

**One real miss + ecosystem mismatch.**

| Issue | Gate that missed | Why it slipped | Fix idea |
|---|---|---|---|
| Stacked branches couldn't merge against post-#15 master | `writing-plans` (sequencing column) | Plan said PRs 2-6 branch-stack from PR 1 branch OR rebase from master post-merge. Implementers branched off pre-merge feat/dns-provider-v2; after PR 15 squash-merge collapsed that branch, stacked branches couldn't auto-rebase (docs conflicts on plan files). Required manual cherry-pick to fresh -v2/-v3 branches per PR. | Plan should say: implementers branch from `master` AFTER PR 1 merges, NOT branch-stack pre-merge. Squash-merge collapses the stack base; rebase is messy. |
| go.mod/go.sum parallel-dep conflicts on PRs 19/20 | `pr-monitoring` | Two PRs adding different libdns deps simultaneously hit go.sum conflicts on second-merge attempt. Auto-rebase would have worked but force-push was hook-blocked. | Use new -v3 branch + close stale PR; non-destructive. Document as expected for parallel-dep PRs in the plan. |

Zero post-merge bugs surfaced. Zero CI failures on merge commits.

## Missed skill activations

| Gate | Fired? | Notes |
|---|---|---|
| project-design-guidance | yes | New workspace `docs/design-guidance.md` written from Q&A |
| brainstorming | yes | Full pipeline including Q&A batches |
| adversarial-design-review (design) | yes | 3 cycles |
| writing-plans | yes | Single invocation |
| adversarial-design-review (plan) | yes | 4 cycles — heavier than usual due to architectural fixes self-introduced |
| alignment-check | yes | First pass PASS |
| scope-lock | yes | Locked 2026-05-26T06:34:58Z (re-locked after Status: edit) |
| subagent-driven-development | yes | Sequential mode; 1 implementer per task in per-task worktree (per `feedback-per-agent-worktree-per-task-pr`) |
| spec-review + code-review | yes | 5 tasks × 2 reviews each = 10 reviews; 1 false-Crit caught |
| finishing-a-development-branch | implicit | PRs opened directly via `gh pr create` after reviews PASS |
| pr-monitoring | yes (3 monitors total) | PR 15 monitor (cleanly merged), PRs 17-20 monitor (bailed on hook misfire), PRs 21-22 monitor (returned incomplete) |
| post-merge-retrospective | yes (this doc) | |
| Copilot review | SKIPPED | per `feedback-copilot-review-broken-2026-05` |

## What worked

- **Adversarial cycles caught all upstream cred-key mismatches** before any code was written. Cycle 1 design alone identified 4 critical libdns field-tag invented names. Without these gates, 4 of 5 v2 adapters would have shipped with non-functional cred passing.
- **Registry-map refactor (plan-cycle-2 fix)** turned 6 serially-conflicting PRs into 1 blocking + 4 parallel. Each subsequent PR added 1 file via `init() Register(...)`, zero `adapter.go` conflicts across PRs.
- **Iface-typed `.provider` field (plan-cycle-4 fix)** is architecturally correct (matches v1 DO test precedent). Tests exercise REAL adapter methods against stubs — boundary lock + algorithm verification both real.
- **Source-spike resolution (Namecheap whole-zone risk)** turned a "gating verification" Important into a "no adapter logic needed" RESOLVED via single 30-min upstream source read. Saved adapter-side Get-merge-Set complexity.
- **Per-task worktrees** isolated parallel implementer work cleanly. No shared-branch coordination issues. All 4 implementers ran in parallel ~3 minutes each.
- **Workspace `docs/design-guidance.md`** authored from human Q&A established workflow-ecosystem-dogfood mandate as cross-design constraint. First design (v2) inherited it cleanly via citation.

## What didn't

- **4 plan-adversarial cycles** is high — each cycle introduced new Critical findings via its own fixes (cycle 2 introduced helper-in-prod; cycle 3 introduced helper-in-test-still-doesn't-exercise-adapter; cycle 4 introduced 1 missing import). Matches the pattern from v1 retro ("adversarial cycles introduced new Criticals by their own fixes"). **Pattern recurrence**: 2nd retro showing this. Time to propose a plugin gate: "after each revision, before re-running, diff the prior plan/design and ask 'what new architectural surface did this revision introduce?'"
- **Stacked-branch coordination broke post-merge** — branch-stacking from feat/dns-provider-v2 was foreseeably wrong because squash-merge always collapses the stack base. Plan should have specified "branch from master AFTER PR 1 merges" instead of offering branch-stack as alternative.
- **Background pr-monitoring agents returned incomplete/empty results twice** (accff76 returned stale state; af7a0a60 returned 3-word "Waiting for monitor notification"). Suggests `subagent-driven-development` reliability gap when long-running monitor tasks combine with hook interventions. Orchestrator had to verify manually via `gh pr view` and admin-merge directly. Not a pipeline bug per se but a monitor-reliability gap to track.
- **Two PR replacements (#19→#21, #20→#22)** were needed because hook blocked force-push of rebased branches. Cherry-pick + new branch + close-old-PR works but adds operator-visible PR churn. The hook rule (no force-push autonomously) is correct in spirit but expensive for routine rebase-after-conflict.

## Plugin-level follow-ups

**Pattern: adversarial cycles introduce new Criticals via their own fixes.** Observed TWICE now (v1 DNS ownership policy + v2 DNS providers). Proposal: add a "revision-introspection" subcheck in `adversarial-design-review`:
- After each revision, before re-running adversarial review, diff the prior plan/design and ask "what new architectural surface did this revision introduce?"
- Surface that diff to the next reviewer cycle as a focused attack target.
- Would have caught cycle-2→3 helper-in-prod regression + cycle-3→4 missing-import + cycle-3→4 helper-doesn't-exercise-adapter in ONE cycle each instead of two.

**Pattern: long-running pr-monitoring subagents return empty/stale.** Single retro observation. Watch for recurrence; if 1 more retro shows it, propose monitor-cadence reliability fix.

## Project guidance updates

| Guidance file | Change | Reason |
|---|---|---|
| `/Users/jon/workspace/docs/design-guidance.md` | no change | Established THIS session; cycle-4 design cited it correctly; no retro-discovered drift |
| (none repo-local) | n/a | DNS-provider-v2 is one of many extension classes; no cross-design v2-specific principle worth promoting to repo-local guidance |
