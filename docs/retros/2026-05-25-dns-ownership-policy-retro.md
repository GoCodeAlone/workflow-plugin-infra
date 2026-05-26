# Retro: DNS ownership policy gate

**PR:** #13 — feat(infra): DNS ownership policy gate + infra.dns_record step + wfctl infra-dns CLI
**Merged:** 2026-05-25 (885a8cc23f9fe95dfb2009af24c0b31d76d99f5a)
**Branch:** feat/dns-ownership-policy (deleted post-merge)
**Design:** docs/plans/2026-05-25-dns-ownership-policy-design.md
**Plan:** docs/plans/2026-05-25-dns-ownership-policy.md
**Related ADRs:** none filed (decisions captured inline in design body)

## Adversarial-review findings, scored

Aggregate: **7 design + 3 plan adversarial cycles** surfaced 22 Criticals + 31 Importants + 12+ Minors. All resolved pre-merge. Sample of high-signal findings below.

| Phase | Finding | Severity | Outcome |
|---|---|---|---|
| design c1 | bootstrap circular dependency (gate blocks own first write) | Critical | Resolved upfront — bootstrap bypasses Gate via set-policy CLI |
| design c1 | DNSConfig proto lacks `owner` field | Critical | Resolved upfront — typed owner in new DNSRecordStepInput |
| design c1 | IaCProvider gRPC chain unimplemented | Critical | Resolved upfront — use libdns directly, isolated in dnsprovider/ |
| design c2 | STRICT_PROTO rejects root-level YAML keys | Critical | Resolved upfront — typed step input proto field |
| design c2 | `infra.dns_record` step type doesn't exist | Critical | Resolved upfront — explicit registration in plugin.go |
| design c3 | DNSRecordStepInput conflates Config+Input (TypedStepFactory needs 3 messages) | Critical | Resolved upfront — split into Config/Input/Output |
| design c3 | wfctl `plugin <name> <sub>` dispatch doesn't exist | Critical | Resolved upfront — single-binary --wfctl-cli sentinel via ServePluginFull |
| design c4 | plugin.contracts.json invented field names (`config_descriptor`) silently register as untyped | Critical | Resolved upfront — corrected to `config`/`input`/`output` |
| design c4 | wfctl plugin-binary dispatch does NOT scan `wfctl-<name>` (reads `plugin.json.capabilities.cliCommands[]`) | Critical | Resolved upfront — manifest-driven declaration |
| design c5 | `binary` field invented in CLICommandDeclaration (doesn't exist) | Critical | Resolved upfront — single-binary main.go handles both roles |
| design c5 | `dnsprovider.Apply` undefined; post-gate DNS write path missing | Critical | Resolved upfront — DNSRecordWriter interface + Apply signature |
| design c6 | Manual os.Args + sdk.ServeIaCPlugin pattern swallows exit codes | Critical | Resolved upfront — sdk.ServePluginFull + admincli.CLIProvider precedent |
| design c6 | Stale `provider_token_ref`/`ProviderTokenRef` after `provider_creds` rename | Critical | Resolved upfront — global replace |
| plan c1 | Task 8 `m.typeName` undefined (struct field is `m.infraType`) | Critical | Resolved upfront |
| plan c1 | Task 10 introduces `main.Version` but goreleaser injects into `internal.Version` | Critical | Resolved upfront — main.go uses internal.Version |
| plan c1 | CheckAllowed default-owner steals explicitly delegated records | Critical | Resolved upfront — two-phase logic |
| plan c1 | DO SetRecords requires existing ID (UpsertTXT broken for new records) | Critical | Resolved upfront — GET + SetRecords(ID) for updates + AppendRecords for new |
| plan c1 | DO DeleteRecords without ID fails | Critical | Resolved upfront — GET first then DeleteRecords with full record |
| plan c2 | plugin_test.go hard t.Fatalf on non-module contracts breaks Task 6 | Critical | Resolved upfront — kind-guard patches added to Task 6 |
| plan c2 | Task 9 audit test in `package admincli` calls undefined LogAttempt (lives in dnsaudit) | Critical | Resolved upfront — test moved into dnsaudit package |
| design c1 | Heritage sentinel collision risk (`_dns-mgmt` too generic) | Important | Resolved upfront — renamed to `_workflow-dns-policy` (tool-scoped) |
| design c2 | libdns/hover doesn't exist (provider matrix lied) | Important | Resolved upfront — Hover deferred to v2 |
| design c2 | libdns module dependency burden not acknowledged | Important | Resolved upfront — isolated in internal/dnsprovider/ |
| design c4 | Hover client 508 lines (not ~80); adapter requires multi-step logic | Important | Resolved upfront — Hover deferred to v2 entirely |
| design c5 | Route53/GCP/Azure also break single-token NewAdapter signature | Important | Resolved upfront — `creds map[string]string` signature + v1 = DO+CF only |
| design c5 | Bootstrap UX confusion (`--bootstrap` vs routine `set-policy`) | Important | Resolved upfront — 6-case behavior table |
| design c6 | ExpandEnvInMapPreservingVars not on step-config path | Important | Resolved upfront — template + bare-shell paths documented |
| design c7 | Pattern-within-Entry sort unspecified (sha256 non-deterministic) | Minor | Resolved upfront — Serialize sorts patterns + types within Entry |

## Gate misses

**None this PR.** Every issue downstream of the design phase was caught by the gate it was assigned to:

- Design adversarial review caught all design-level architectural bugs (proto split, dispatch mechanism, libdns API quirks) before the plan was written.
- Plan adversarial review caught all plan-level mechanical bugs (struct field names, test kind-guards, ldflag targets) before any code was written.
- Code review caught all post-implementation bugs (DO upsertTXTRRset early-break, unguarded type-assert, transfer-ownership self-delete, doc/code drift, uncached gate) before PR was opened — all addressed in commit `420b46f` before opening PR.
- Zero Copilot review comments + zero post-merge inline comments + zero CI failures on merge commit.

The autonomous pipeline caught everything pre-merge. No reviewer outside the pipeline had to flag anything.

| Issue | Gate that missed | Why it slipped | Fix idea |
|---|---|---|---|
| (none) | — | — | — |

## Missed skill activations

| Gate | Fired? | Notes |
|---|---|---|
| brainstorming | yes (informally) | Initial design discussion was conversational — no formal brainstorming skill invocation, but design doc was authored from informal exploration. Acceptable. |
| adversarial-design-review (design) | yes | 7 cycles |
| writing-plans | yes | 1 invocation |
| adversarial-design-review (plan) | yes | 3 cycles |
| alignment-check | yes | 2 cycles |
| scope-lock | yes | Locked 2026-05-25T20:24:21Z |
| subagent-driven-development | yes | Single implementer sequential mode (10 tasks split into 11 commits) |
| requesting-code-review | yes | Found 1 Critical + 4 Important + 2 Minor → fixed pre-PR |
| finishing-a-development-branch | yes | Autonomous mode → PR created |
| pr-monitoring | yes | Background agent ran; merge happened externally before agent finished polling (Copilot must have approved + admin-merge fired) |
| post-merge-retrospective | yes (this doc) | |

## What worked

- **7+3 adversarial cycles converged on a clean design.** Each cycle surfaced real architectural bugs (not nits). Cycle-over-cycle the design shrank in surface area (Hover deferred, single-binary dispatch, narrow v1 provider scope) and gained precision (sha256 formula, 6-case bootstrap table, two-phase CheckAllowed).
- **Plan-phase adversarial review caught 5 Criticals that would have broken at first compile.** Struct field names, ldflag targets, plugin_test.go kind-guards, DO libdns API quirks — all surfaced from grepping the actual code before the implementer touched anything.
- **Code review caught a real ownership-correctness bug** (DO upsertTXTRRset early-break leaving stale TXT entries — would have silently undone set-policy edits at runtime). Fixed pre-PR.
- **Single-binary --wfctl-cli dispatch via sdk.ServePluginFull** matched existing workflow-plugin-supply-chain precedent — no engine changes needed.

## What didn't

- **Adversarial cycles introduced new Criticals by their own fixes.** Cycle 2 introduced C-1 (STRICT_PROTO) + C-2 (step type doesn't exist) by mis-applying cycle-1 fixes. Cycle 3 introduced C-1 (3-message split) + C-2 (wfctl dispatch fiction) by mis-applying cycle-2 fixes. Each subsequent cycle had to fix the previous cycle's introduction. Net: 13 Criticals total across 7 design cycles, ~half of which were self-inflicted by partial fixes.
- **No formal brainstorming skill invocation** at the start — the design doc was authored from conversational context. Worked here because the user had clear direction, but a `brainstorming` invocation would have made the load-bearing assumption list explicit earlier (TXT-only marker, gate placement, owner identity trust model).
- **Implementer's reported "main_smoke_test.go gitignore catch"** was a non-issue (gitignore only matches the compiled binary, not the test file). The implementer's misdiagnosis didn't cause harm but indicates a watchdog opportunity: spec-reviewer flagged it as "moot" rather than ignoring it — good catch.

## Plugin-level follow-ups

**Pattern: adversarial cycles introduce new Criticals via their own fixes.** Observed here across 6 cycles. If this recurs in 2+ more retros, propose:
- A "cycle stability" subcheck in `adversarial-design-review`: after each revision, before re-running, diff the prior plan/design and ask "what new architectural surface did this revision introduce?" — surface that to the next reviewer cycle as a focused attack target.

For now: single retro, single observation. **No plugin change warranted yet.** Watch for recurrence.

## Project guidance updates

| Guidance file | Change | Reason |
|---|---|---|
| `docs/design-guidance.md` | no change | No cross-design lesson surfaced. The patterns identified (TXT-policy zone-root, libdns isolation, single-binary --wfctl-cli dispatch, `creds map[string]string` for multi-cred forward-compat) are infra-plugin-specific implementation choices, not durable workflow-wide design principles. |
