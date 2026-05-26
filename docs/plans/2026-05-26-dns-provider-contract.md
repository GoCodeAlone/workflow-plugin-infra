# DNS import + provider decoupling Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Ship cross-provider DNS state import via the engine-native `wfctl infra import` path (4 provider plugin EnumerateAll PRs + 1 bulk-import wrapper), relocate DNS policy code out of workflow-plugin-infra into wfctl (2 PRs), and add cross-provider DNS orchestration scenarios (1 PR). 8 PRs across 6 repos.

**Architecture:** Engine-native via strict-contract `IaCProvider.Import` + `IaCProviderEnumerator.EnumerateAll` (`workflow/plugin/external/proto/iac.proto`). No new gRPC contract. No peer-dispatch SDK extension. Policy code moves to wfctl where it has direct provider-driver access; workflow-plugin-infra strips libdns/* deps + the `infra.dns_record` step (deprecated due to step-handler peer-dispatch impossibility per cycle-3 adversarial I-NEW-1).

**Tech Stack:** Go 1.23+; workflow SDK (`workflow/plugin/external/sdk`); per-provider native SDKs (godo, cloudflare-go/v7, go-namecheap-sdk, pkg/hoverclient); `workflow/iac/wfctlhelpers/ApplyPlanHooks`; `workflow/cmd/wfctl/`.

**Base branch:** main (each repo's primary branch)

**Design:** `docs/plans/2026-05-26-dns-provider-contract-design.md`

---

## Scope Manifest

**PR Count:** 9
**Tasks:** 33
**Estimated Lines of Change:** ~3600 (informational; not enforced)

**Out of scope:**
- aws/azure/gcp/godaddy/route53 EnumerateAll implementations (follow-up plans per provider; same pattern as PR 1-4).
- Workflow SDK extensions for peer-plugin dispatch (`InvokeService` on `EngineCallbackService`, `AdditionalServices` hook on `IaCServeOptions`). The design uses engine-native primitives that do not need them.
- Cryptographic plugin-identity attestation (workflow-plugin-supply-chain owns).
- gocodealone-dns catalog refresh (Phase 5 — separate design in gocodealone-dns repo; deferred per user direction).
- New gRPC contract for DNS provider operations. Existing strict contract suffices.
- Per-record-mutation step type replacement. `infra.dns_record` step deprecated; per-record workflows route through `wfctl infra apply` or `wfctl dns-policy *`. A future per-step DNS surface (if needed) is a separate design.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(do): EnumerateAll for infra.dns | Task 1, Task 2, Task 3 | `feat/dns-enumerate-all` (workflow-plugin-digitalocean) |
| 2 | feat(cf): EnumerateAll for infra.dns | Task 4, Task 5, Task 6 | `feat/dns-enumerate-all` (workflow-plugin-cloudflare) |
| 3 | feat(nc): EnumerateAll for infra.dns | Task 7, Task 8, Task 9 | `feat/dns-enumerate-all` (workflow-plugin-namecheap) |
| 4 | feat(hover): ListDomains + EnumerateAll for infra.dns | Task 10, Task 11, Task 12, Task 13 | `feat/dns-enumerate-all` (workflow-plugin-hover) |
| 4.5 | chore(registry): pin-bump DO/CF/NC/Hover for EnumerateAll | Task 13.5 | `chore/dns-providers-pin-bump` (workflow-registry) |
| 5 | feat(wfctl): infra import-all bulk wrapper | Task 14, Task 15, Task 16, Task 17 | `feat/wfctl-infra-import-all` (workflow) |
| 6 | feat(wfctl): relocate dns policy/gate/audit + dns-policy commands + OnBeforeAction hook | Task 18, Task 19, Task 20, Task 21, Task 22, Task 23, Task 24 | `feat/wfctl-dns-policy` (workflow) |
| 7 | refactor(infra): strip libdns + admincli + dns packages + remove dns_record step | Task 25, Task 26, Task 27, Task 28 | `refactor/strip-dns-libdns` (workflow-plugin-infra) |
| 8 | feat(scenarios): DNS orchestration tests + stub provider plugin harness | Task 29, Task 30, Task 31, Task 32 | `feat/dns-orchestration` (workflow-scenarios) |

**Dependencies:**
- PRs 1, 2, 3, 4 parallel (no inter-dep).
- PR 4.5 (workflow-registry manifest sweep) follows PRs 1-4 — batched manifest version pin updates for all 4 providers in one workflow-registry PR per `feedback_version_bump_immediate_merge.md` pattern.
- PR 5 needs PRs 1-4 merged + tagged + PR 4.5 merged (for its e2e smoke against a real provider). Recommend after all 4 land + tags pushed + registry refreshed.
- PR 6 needs PRs 1-4 merged + PR 5 merged (its tests exercise EnumerateAll-backed driver paths via `wfctl infra import-all`).
- PR 7 needs PR 6 merged (relocates pkg destinations must exist first).
- PR 8 needs PRs 1-5 merged (scenarios consume import-all + EnumerateAll). PR 8's stub-plugin scaffolding can begin in parallel with PR 6/7; full scenario suite blocks until PR 7 merge to confirm system shape.

**Status:** Draft

---

## Global Design Guidance Mapping

| Guidance | Plan response |
|---|---|
| wfctl is user-facing CLI; cross-cutting orchestrators → wfctl builtin | PR 5 + PR 6 add wfctl builtins; PR 7 strips plugin cliCommand |
| Strict contracts; no structpb/Any | Reuses existing `IaCProviderEnumerator.EnumerateAll` + `IaCProvider.Import` strict contracts |
| libdns/cloud-sdks isolated in `internal/<provider>/` | PR 7 drops libdns from workflow-plugin-infra; remaining libdns lives in each provider plugin's `internal/drivers/` |
| Cross-driver parity (≥2 drivers) | 4 drivers in PR 1-4; Phase 4 scenarios validate parity |
| No mock-first development | PR 8 builds real stub IaCProvider gRPC plugin (not HTTP mock) + live-cred env-gated tests in PR 1-4 |
| Secrets never logged | PR 6 drops `--token` flag from admincli; creds source from infra config file (one location) |
| Audit trail for state-mutating ops | PR 6 relocates JSONL trail to `${XDG_STATE_HOME}/wfctl/plugins/wfctl/dns-audit.jsonl` |
| Self-hosted runner for IaC CI | PR 1-4 live tests env-gated; CI workflow uses self-hosted runner already |
| Plugin minEngineVersion + capabilities populated | PR 7 bumps workflow-plugin-infra to v1.0.0 (capability surface shrink) |
| Cross-cutting orchestrator commands → wfctl builtin | PR 6 `wfctl dns-policy *` builtin; explicitly justified by rev-3 design-guidance §CLI |

---

## Verification per change class

| Task | Class | Verification |
|---|---|---|
| Tasks 1-13 (provider EnumerateAll) | Plugin/extension + multi-component | unit tests pass; live test env-gated; representative call returns expected `[]ResourceOutput` |
| Tasks 14-17 (import-all CLI) | CLI command + multi-component | `wfctl infra import-all --help` exits 0; e2e against ≥1 real provider plugin loaded |
| Tasks 18-24 (dns-policy + relocation) | CLI command + internal logic + path migration | unit tests; `wfctl dns-policy show --help` correct; gate hook tests pass; audit JSONL migration smoke run |
| Tasks 25-28 (infra strip) | Plugin/extension + proto break + version pin update | `go build ./...` exits 0 post-strip; version-skew audit clean; `wfctl plugin verify-capabilities workflow-plugin-infra` passes |
| Tasks 29-32 (scenarios) | Multi-component boundary + integration | scenarios run with stub plugin; cross-provider transfer asserts (type,name,data,ttl) equality; delegation scenario walks two providers |

Runtime-launch-validation triggers (build/deployment/version pins/plugin loading) apply to: Task 28 (workflow-plugin-infra major bump + capability change); Task 24 (wfctl version bump). Both carry rollback notes in their task bodies.

---

## PR 1 — workflow-plugin-digitalocean: EnumerateAll for infra.dns

**Repo:** `/Users/jon/workspace/workflow-plugin-digitalocean`
**Branch:** `feat/dns-enumerate-all-2026-05-26T1900`

### Task 1: Add unit test for EnumerateAll("infra.dns")

**Files:**
- Test: `internal/provider_enumerator_test.go` (modify; existing test file for spaces_key enum sits here)

**Step 1: Write failing test**

```go
func TestDOProvider_EnumerateAll_DNS_paginates(t *testing.T) {
    ctx := context.Background()
    mockDomains := &mockDomainsClient{
        pages: [][]godo.Domain{
            {{Name: "alpha.test", TTL: 1800}, {Name: "beta.test", TTL: 3600}},
            {{Name: "gamma.test", TTL: 1800}},
        },
    }
    p := &DOProvider{client: &godo.Client{Domains: mockDomains}}
    out, err := p.EnumerateAll(ctx, "infra.dns")
    if err != nil { t.Fatalf("EnumerateAll: %v", err) }
    if len(out) != 3 { t.Fatalf("want 3 zones; got %d", len(out)) }
    if out[0].Outputs["zone"] != "alpha.test" { t.Errorf("zone[0] = %v", out[0].Outputs["zone"]) }
    if out[0].ProviderID != "alpha.test" { t.Errorf("providerID[0] = %v", out[0].ProviderID) }
    if out[0].Outputs["ttl"].(int) != 1800 { t.Errorf("ttl[0] = %v", out[0].Outputs["ttl"]) }
}

func TestDOProvider_EnumerateAll_DNS_uninitialized(t *testing.T) {
    p := &DOProvider{client: nil}
    _, err := p.EnumerateAll(context.Background(), "infra.dns")
    if err == nil || !strings.Contains(err.Error(), "not initialized") {
        t.Fatalf("want not-initialized error; got %v", err)
    }
}
```

`mockDomainsClient` stub mirrors existing `mockSpacesKeysClient` pattern; emits paginated responses, supports `Links.Pages.Next` to terminate.

**Step 2: Run test to verify failure**

Run: `GOWORK=off go test -run 'TestDOProvider_EnumerateAll_DNS' ./internal/...`
Expected: FAIL — `EnumerateAll: resource type "infra.dns" not supported`

**Step 3: Add compile-time interface assertion**

In `internal/provider_enumerator_test.go` (or `provider.go` near the existing `Enumerator` assertion):

```go
var _ interfaces.EnumeratorAll = (*DOProvider)(nil)
```

`interfaces.EnumeratorAll` is the new (account-scoped) interface; `interfaces.Enumerator` is the tag-scoped one (separate method). Verify the exact interface name via `go doc github.com/GoCodeAlone/workflow/interfaces.EnumeratorAll` — if the canonical name differs, use the canonical name.

**Step 4: Commit failing test**

```bash
git add internal/provider_enumerator_test.go
git commit -m "test(provider): add failing EnumerateAll infra.dns coverage"
```

### Task 2: Implement enumerateAllDNS

**Files:**
- Modify: `internal/provider.go:670-760` (EnumerateAll switch + add private helper)

**Step 1: Add case to switch**

In `EnumerateAll` body (around line 687):

```go
case "infra.dns":
    return p.enumerateAllDNS(ctx)
```

**Step 2: Implement helper**

Append to `internal/provider.go` (mirror the existing `enumerateAllSpacesKeys` pagination pattern at lines 703-760):

```go
// enumerateAllDNS paginates GET /v2/domains via godo's Domains.List using
// ListOptions{Page,PerPage:200}; loop terminates via godo's IsLastPage signal.
// Each *ResourceOutput carries the zone name + ttl + zone_file for the
// import-all path to feed into IaCProvider.Import.
func (p *DOProvider) enumerateAllDNS(ctx context.Context) ([]*interfaces.ResourceOutput, error) {
    var all []*interfaces.ResourceOutput
    opt := &godo.ListOptions{Page: 1, PerPage: 200}
    for {
        domains, resp, err := p.client.Domains.List(ctx, opt)
        if err != nil {
            return nil, fmt.Errorf("digitalocean: EnumerateAll infra.dns: list page=%d: %w", opt.Page, err)
        }
        for _, d := range domains {
            outputs := map[string]any{
                "zone":      d.Name,
                "ttl":       d.TTL,
                "zone_file": d.ZoneFile,
            }
            all = append(all, &interfaces.ResourceOutput{
                ProviderID: d.Name,
                Type:       "infra.dns",
                Outputs:    outputs,
            })
        }
        if resp == nil || resp.Links == nil || resp.Links.IsLastPage() {
            break
        }
        page, err := resp.Links.CurrentPage() // godo returns (int, error) — single int
        if err != nil {
            return nil, fmt.Errorf("digitalocean: EnumerateAll infra.dns: parse current page: %w", err)
        }
        opt.Page = page + 1
    }
    return all, nil
}
```

**Step 3: Run unit tests**

Run: `GOWORK=off go test -run 'TestDOProvider_EnumerateAll_DNS' ./internal/...`
Expected: PASS — both tests green.

**Step 4: Run broader provider tests** (no regression)

Run: `GOWORK=off go test ./internal/...`
Expected: PASS — all provider tests including existing spaces_key enumerator tests still green.

**Step 5: Commit**

```bash
git add internal/provider.go
git commit -m "feat(provider): implement EnumerateAll for infra.dns via godo Domains.List"
```

### Task 3: Live integration test (env-gated)

**Files:**
- Test: `internal/provider_enumerator_live_test.go` (new)

**Step 1: Write env-gated test**

```go
//go:build live_dns

package internal

import (
    "context"
    "os"
    "testing"
)

func TestDOProvider_EnumerateAll_DNS_live(t *testing.T) {
    if os.Getenv("INFRA_DNS_ENUMERATE_LIVE") != "1" {
        t.Skip("set INFRA_DNS_ENUMERATE_LIVE=1 + DIGITALOCEAN_TOKEN to run")
    }
    p := newRealProvider(t) // helper from existing live test file pattern
    out, err := p.EnumerateAll(context.Background(), "infra.dns")
    if err != nil { t.Fatalf("live EnumerateAll: %v", err) }
    if len(out) == 0 { t.Skip("account has zero zones; cannot validate") }
    for _, o := range out {
        if o.ProviderID == "" { t.Errorf("empty ProviderID for %+v", o.Outputs) }
        if o.Type != "infra.dns" { t.Errorf("wrong Type %q", o.Type) }
    }
    t.Logf("enumerated %d zones from live account", len(out))
}
```

`newRealProvider(t)` helper exists per prior live-test patterns; if missing in this repo, copy from spaces_key live test scaffolding.

**Step 2: Smoke run locally if creds available**

Run: `INFRA_DNS_ENUMERATE_LIVE=1 DIGITALOCEAN_TOKEN=$TOKEN GOWORK=off go test -tags live_dns -run TestDOProvider_EnumerateAll_DNS_live ./internal/...`
Expected: PASS (or SKIP if zero zones); log shows enumerated count.

**Step 3: Commit + push branch**

```bash
git add internal/provider_enumerator_live_test.go
git commit -m "test(provider): add env-gated live EnumerateAll infra.dns test"
git push -u origin feat/dns-enumerate-all-2026-05-26T1900
```

**Step 4: Open PR**

```bash
gh pr create --title "feat(provider): EnumerateAll for infra.dns" \
  --body "Implements IaCProviderEnumerator.EnumerateAll(\"infra.dns\") via godo Domains.List paginated. Returns one *ResourceOutput per zone with ProviderID=zone-name + Outputs={zone, ttl, zone_file}.

Part of cross-repo cascade docs/plans/2026-05-26-dns-provider-contract.md (workflow-plugin-infra). Unblocks wfctl infra import-all path for DO zones.

Live integration test env-gated on INFRA_DNS_ENUMERATE_LIVE=1." \
  --base main
```

Add Copilot reviewer per memory feedback (skip per memory `feedback_copilot_review_broken_2026_05.md` if service still broken; CI green + admin-merge suffices).

---

## PR 2 — workflow-plugin-cloudflare: EnumerateAll for infra.dns

**Repo:** `/Users/jon/workspace/workflow-plugin-cloudflare`
**Branch:** `feat/dns-enumerate-all-2026-05-26T1900`

### Task 4: Add unit test for EnumerateAll("infra.dns")

**Files:**
- Test: `internal/iacserver_test.go` (modify; existing test file has IaC server scaffolding)

**Step 0: Inspect actual cfProvider shape** (cycle-2 finding)

Before writing code, the implementer runs `grep -n 'type cfProvider' internal/iacserver.go` to confirm the struct's exact fields. As of v0.x cfProvider has `dnsDriver *drivers.DNSDriver` + `domainDriver *drivers.DomainDriver` — there is NO `client` field. The new EnumerateAll path needs an account-level zone client; add it via dependency injection.

**Step 1: Define minimal zonePager interface + zoneListerCF**

cloudflare-go/v7's AutoPager is a concrete type that's hard to construct from a slice in tests. Define a small interface in `internal/iacserver.go` that captures only the methods cfProvider uses:

```go
// zonePager is the minimal iterator surface EnumerateAll needs.
// Both cloudflare-go's *pagination.V4PagePaginationArrayAutoPager[zones.Zone]
// and the test fake satisfy this interface via standard method names.
type zonePager interface {
    Next() bool
    Current() zones.Zone
    Err() error
}

type zoneListerCF interface {
    // ListZones returns a zonePager iterating the account's zones.
    ListZones(ctx context.Context, query zones.ZoneListParams) zonePager
}

type cfProvider struct {
    dnsDriver    *drivers.DNSDriver
    domainDriver *drivers.DomainDriver
    zones        zoneListerCF  // injected; concrete adapter wraps SDK AutoPager
}
```

`Initialize()` (existing) constructs a concrete `zoneListerCF` whose `ListZones` calls `sdkClient.Zones.ListAutoPaging(ctx, query)` and returns the AutoPager (which already implements `Next()/Current()/Err()`):

```go
type cfRealZoneLister struct{ client *cloudflare.Client }
func (l *cfRealZoneLister) ListZones(ctx context.Context, q zones.ZoneListParams) zonePager {
    return l.client.Zones.ListAutoPaging(ctx, q) // returns *V4PagePaginationArrayAutoPager which satisfies zonePager
}
```

The exact return-type satisfaction is verified at compile time by Go's structural typing; if the AutoPager's `Current()` returns `Zone` (not `*Zone`), the interface matches directly. If signature differs (e.g., `Current() *Zone`), adjust the interface OR wrap in a thin adapter.

**Step 2: Write failing test (slice-backed stub)**

```go
type slicePager struct {
    items []zones.Zone
    i     int
    cur   zones.Zone
    err   error
}
func (p *slicePager) Next() bool {
    if p.i >= len(p.items) { return false }
    p.cur = p.items[p.i]
    p.i++
    return true
}
func (p *slicePager) Current() zones.Zone { return p.cur }
func (p *slicePager) Err() error { return p.err }

type fakeZoneLister struct{ items []zones.Zone }
func (f *fakeZoneLister) ListZones(_ context.Context, _ zones.ZoneListParams) zonePager {
    return &slicePager{items: f.items}
}

func TestCfProvider_EnumerateAll_DNS(t *testing.T) {
    ctx := context.Background()
    p := &cfProvider{zones: &fakeZoneLister{items: []zones.Zone{
        {ID: "zid-1", Name: "alpha.test", Account: zones.ZoneAccount{ID: "acct-1"}},
        {ID: "zid-2", Name: "beta.test", Account: zones.ZoneAccount{ID: "acct-1"}},
    }}}
    out, err := p.EnumerateAll(ctx, "infra.dns")
    if err != nil { t.Fatalf("EnumerateAll: %v", err) }
    if len(out) != 2 { t.Fatalf("want 2; got %d", len(out)) }
    if out[0].ProviderID != "zid-1" { t.Errorf("providerID[0]") }
    if out[0].Outputs["zone"] != "alpha.test" { t.Errorf("zone[0]") }
    if out[0].Outputs["account_id"] != "acct-1" { t.Errorf("account_id[0]") }
}
```

No reliance on `pagination.NewArrayAutoPagerFromSlice` (which doesn't exist). The 3-method `zonePager` interface is the entire test surface.

**Step 2: Run failing test**

Run: `GOWORK=off go test -run 'TestCfProvider_EnumerateAll_DNS' ./internal/...`
Expected: FAIL — `EnumerateAll: resource type "infra.dns" not supported` or method-missing if cfProvider doesn't yet implement EnumerateAll.

**Step 3: Commit failing test**

```bash
git add internal/iacserver_test.go
git commit -m "test(provider): add failing EnumerateAll infra.dns coverage"
```

### Task 5: Implement EnumerateAll on cfProvider

**Files:**
- Modify: `internal/iacserver.go` (add EnumerateAll method to cfProvider)
- Modify: `internal/drivers/dns.go` (sdkClient ListZones method if not present)

**Step 3: Add EnumerateAll to cfProvider using zonePager iterator**

In `internal/iacserver.go`:

```go
// EnumerateAll implements interfaces.EnumeratorAll for infra.dns. Pages via
// the injected zoneListerCF (production wraps cloudflare-go/v7
// Zones.ListAutoPaging). Per-zone account_id captured for downstream Import.
func (p *cfProvider) EnumerateAll(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
    if p.zones == nil {
        return nil, fmt.Errorf("cloudflare: EnumerateAll called on uninitialized provider")
    }
    if resourceType != "infra.dns" {
        return nil, fmt.Errorf("cloudflare: EnumerateAll: resource type %q not supported", resourceType)
    }
    var out []*interfaces.ResourceOutput
    pager := p.zones.ListZones(ctx, zones.ZoneListParams{}) // value, not pointer
    for pager.Next() {
        zone := pager.Current()
        out = append(out, &interfaces.ResourceOutput{
            ProviderID: zone.ID,
            Type:       "infra.dns",
            Outputs: map[string]any{
                "zone":       zone.Name,
                "account_id": zone.Account.ID,
                "zone_id":    zone.ID,
            },
        })
    }
    if err := pager.Err(); err != nil {
        return nil, fmt.Errorf("cloudflare: EnumerateAll infra.dns: %w", err)
    }
    return out, nil
}
```

Verify exact AutoPager method-set at implementation start via `go doc github.com/cloudflare/cloudflare-go/v7/zones` — if `Current()` returns `*Zone` not `Zone`, adjust the `zonePager` interface accordingly.

**Step 3: Run unit tests**

Run: `GOWORK=off go test -run 'TestCfProvider_EnumerateAll_DNS' ./internal/...`
Expected: PASS.

**Step 4: Broader test run**

Run: `GOWORK=off go test ./internal/...`
Expected: PASS — no regression in existing CF tests.

**Step 5: Commit**

```bash
git add internal/iacserver.go internal/drivers/dns.go
git commit -m "feat(provider): implement EnumerateAll for infra.dns via cloudflare-go ListAutoPaging"
```

### Task 6: Live integration test + open PR

**Files:**
- Test: `internal/iacserver_live_test.go` (new)

**Step 1: Env-gated live test**

```go
//go:build live_dns

package internal

import (
    "context"
    "os"
    "testing"
)

func TestCfProvider_EnumerateAll_DNS_live(t *testing.T) {
    if os.Getenv("INFRA_DNS_ENUMERATE_LIVE") != "1" {
        t.Skip("set INFRA_DNS_ENUMERATE_LIVE=1 + CLOUDFLARE_API_TOKEN to run")
    }
    p := newLiveCfProvider(t) // helper using CLOUDFLARE_API_TOKEN
    out, err := p.EnumerateAll(context.Background(), "infra.dns")
    if err != nil { t.Fatalf("live EnumerateAll: %v", err) }
    if len(out) == 0 { t.Skip("account has zero zones") }
    for _, o := range out {
        if o.ProviderID == "" { t.Errorf("empty zone ID for %+v", o.Outputs) }
    }
    t.Logf("enumerated %d zones", len(out))
}
```

**Step 2: Smoke + commit + push**

```bash
git add internal/iacserver_live_test.go
git commit -m "test(provider): env-gated live EnumerateAll infra.dns test"
git push -u origin feat/dns-enumerate-all-2026-05-26T1900
```

**Step 3: Open PR**

```bash
gh pr create --title "feat(provider): EnumerateAll for infra.dns" \
  --body "Implements IaCProviderEnumerator.EnumerateAll for cloudflare via cloudflare-go ListAutoPaging. Per-zone Outputs include account_id (required for downstream Import operations). Live test env-gated on INFRA_DNS_ENUMERATE_LIVE=1. Part of cross-repo cascade docs/plans/2026-05-26-dns-provider-contract.md (workflow-plugin-infra)." \
  --base main
```

---

## PR 3 — workflow-plugin-namecheap: EnumerateAll for infra.dns

**Repo:** `/Users/jon/workspace/workflow-plugin-namecheap`
**Branch:** `feat/dns-enumerate-all-2026-05-26T1900`

### Task 7: Add unit test

**Files:**
- Test: `internal/iacserver_test.go` (modify)

**Step 0: Inspect actual go-namecheap-sdk types** (cycle-2 finding)

Run `go doc github.com/namecheap/go-namecheap-sdk/v2/namecheap` and verify:
- Response type is `DomainsGetListCommandResponse` (NOT `DomainsGetListResult`)
- `DomainsService` has `GetList(args *DomainsGetListArgs) (*DomainsGetListCommandResponse, error)` — note: caller invokes via `client.Domains.GetList(...)` (subservice)
- `DomainsGetListArgs` fields: `Page *int`, `PageSize *int` (pointers)
- `Domain` struct fields: `Name string`, `IsOurDNS *bool` (pointer), `Expires *DateTime` (pointer)

The test stub must mirror this shape exactly.

**Step 1: Failing test**

```go
func ptrBool(b bool) *bool { return &b }

type stubNCDomains struct{ resp *namecheap.DomainsGetListCommandResponse }
func (s *stubNCDomains) GetList(_ *namecheap.DomainsGetListArgs) (*namecheap.DomainsGetListCommandResponse, error) {
    return s.resp, nil
}

func TestNcProvider_EnumerateAll_DNS(t *testing.T) {
    ctx := context.Background()
    domains := []namecheap.Domain{
        {Name: "alpha.test", IsOurDNS: ptrBool(true)},
        {Name: "beta.test",  IsOurDNS: ptrBool(false)},
    }
    p := &ncProvider{client: &stubNCClient{domains: &stubNCDomains{resp: &namecheap.DomainsGetListCommandResponse{Domains: &domains}}}}
    out, err := p.EnumerateAll(ctx, "infra.dns")
    if err != nil { t.Fatalf("EnumerateAll: %v", err) }
    if len(out) != 2 { t.Fatalf("want 2; got %d", len(out)) }
    if out[0].ProviderID != "alpha.test" { t.Errorf("providerID[0]") }
    if out[0].Outputs["is_our_dns"] != true { t.Errorf("is_our_dns[0]") }
    if out[1].Outputs["is_our_dns"] != false { t.Errorf("is_our_dns[1]") }
}
```

`stubNCClient` mirrors the SDK's nested-subservice shape: `Domains` field returning a `*stubNCDomains`. ncProvider's `client` field is whatever interface today permits calls of the form `p.client.Domains.GetList(...)` — verify at implementation time and adapt the test scaffolding accordingly.

**Step 2: Verify failure + commit**

```bash
GOWORK=off go test -run TestNcProvider_EnumerateAll_DNS ./internal/...
# Expect: FAIL — method not implemented or unsupported type
git add internal/iacserver_test.go
git commit -m "test(provider): failing EnumerateAll infra.dns coverage"
```

### Task 8: Implement EnumerateAll on ncProvider

**Files:**
- Modify: `internal/iacserver.go`

**Step 2: Implementation**

```go
// EnumerateAll implements interfaces.EnumeratorAll for infra.dns. Uses
// namecheap-go-sdk client.Domains.GetList; paginates via PageSize/Page args
// (both *int). is_our_dns surfaced so operators can identify zones
// registered at NC but with authority pointed elsewhere.
func (p *ncProvider) EnumerateAll(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
    if p.client == nil {
        return nil, fmt.Errorf("namecheap: EnumerateAll called on uninitialized provider")
    }
    if resourceType != "infra.dns" {
        return nil, fmt.Errorf("namecheap: EnumerateAll: resource type %q not supported", resourceType)
    }
    var out []*interfaces.ResourceOutput
    page := 1
    pageSize := 100
    for {
        resp, err := p.client.Domains.GetList(&namecheap.DomainsGetListArgs{Page: &page, PageSize: &pageSize})
        if err != nil {
            return nil, fmt.Errorf("namecheap: EnumerateAll infra.dns: page=%d: %w", page, err)
        }
        if resp == nil || resp.Domains == nil || len(*resp.Domains) == 0 {
            break
        }
        for _, d := range *resp.Domains {
            outputs := map[string]any{"zone": d.Name}
            if d.IsOurDNS != nil { outputs["is_our_dns"] = *d.IsOurDNS }
            if d.Expires != nil  { outputs["expires"] = d.Expires.Format(time.RFC3339) }
            out = append(out, &interfaces.ResourceOutput{
                ProviderID: d.Name,
                Type:       "infra.dns",
                Outputs:    outputs,
            })
        }
        if len(*resp.Domains) < pageSize { break }
        page++
    }
    return out, nil
}
```

`namecheap.DateTime` is the SDK's wrapper around `time.Time` (verify the formatter method via `go doc`). Use whatever the SDK exposes (`.Format`, `.Time()`, or direct field) — fall back to `fmt.Sprintf("%v", d.Expires)` if needed but prefer the typed accessor.

**Step 2: Tests + commit**

```bash
GOWORK=off go test ./internal/...
git add internal/iacserver.go
git commit -m "feat(provider): implement EnumerateAll for infra.dns via go-namecheap-sdk GetList"
```

### Task 9: Live integration test + open PR

**Files:**
- Test: `internal/iacserver_live_test.go` (new)

**Step 1: Live test (NC needs IP allowlist + self-hosted runner)**

```go
//go:build live_dns

func TestNcProvider_EnumerateAll_DNS_live(t *testing.T) {
    if os.Getenv("INFRA_DNS_ENUMERATE_LIVE") != "1" {
        t.Skip("set INFRA_DNS_ENUMERATE_LIVE=1 + NAMECHEAP_API_USER + NAMECHEAP_API_KEY + NAMECHEAP_CLIENT_IP")
    }
    p := newLiveNcProvider(t)
    out, err := p.EnumerateAll(context.Background(), "infra.dns")
    if err != nil { t.Fatalf("live: %v", err) }
    t.Logf("enumerated %d zones", len(out))
}
```

**Step 2: Commit + push + PR**

```bash
git add internal/iacserver_live_test.go
git commit -m "test(provider): env-gated live EnumerateAll infra.dns test"
git push -u origin feat/dns-enumerate-all-2026-05-26T1900
gh pr create --title "feat(provider): EnumerateAll for infra.dns" --body "Implements IaCProviderEnumerator.EnumerateAll via go-namecheap-sdk Domains.GetList. Per-zone Outputs include is_using_our_dns (NC authority flag) + expires. Live test env-gated; requires NC client_ip allowlist + self-hosted runner. Part of cross-repo cascade." --base main
```

---

## PR 4 — workflow-plugin-hover: ListDomains + EnumerateAll for infra.dns

**Repo:** `/Users/jon/workspace/workflow-plugin-hover`
**Branch:** `feat/dns-enumerate-all-2026-05-26T1900`

**Prerequisite (cycle-2 finding C4)**: local working tree must be synced to origin/main before starting. PR #26 (pkg/hoverclient extraction; tag v0.3.0) is merged remotely but local checkout may be on the pre-extraction `master` branch.

```bash
cd /Users/jon/workspace/workflow-plugin-hover
git fetch origin
git checkout main
git pull --ff-only origin main
git tag | grep v0.3.0    # confirm tag present locally
ls pkg/hoverclient/      # confirm package directory present
```

Both must succeed before Task 10 proceeds.

**Module-path note (cycle-2 finding C4)**: `pkg/hoverclient` is a SUBPATH inside the single `github.com/GoCodeAlone/workflow-plugin-hover` Go module — NOT a separate Go module with its own `go.mod`. Consumers import it as `github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient` and resolve to the PARENT module's tag (e.g. `@v0.4.0` after Task 11 tags the parent module). Task 11 below tags the parent module at `v0.4.0`, NOT a subpath tag.

### Task 10: Add ListDomains method to pkg/hoverclient

**Files:**
- Modify: `pkg/hoverclient/client.go`
- Test: `pkg/hoverclient/client_test.go`

**Step 1: Failing test**

```go
func TestClient_ListDomains(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/api/domains" { t.Errorf("path = %q", r.URL.Path) }
        fmt.Fprintln(w, `{"succeeded":true,"domains":[{"id":"d-1","domain_name":"alpha.test"},{"id":"d-2","domain_name":"beta.test"}]}`)
    }))
    defer srv.Close()
    c := newTestClient(srv)
    domains, err := c.ListDomains(context.Background())
    if err != nil { t.Fatalf("ListDomains: %v", err) }
    if len(domains) != 2 { t.Fatalf("want 2; got %d", len(domains)) }
    if domains[0].DomainName != "alpha.test" { t.Errorf("name[0] = %q", domains[0].DomainName) }
}
```

**Step 2: Verify fail + implement**

```bash
GOWORK=off go test -run TestClient_ListDomains ./pkg/hoverclient/
# FAIL: method not defined
```

In `pkg/hoverclient/client.go` (sibling to `GetDomain` at line 367):

```go
// ListDomains fetches all domains in the authenticated account from
// GET /api/domains. Returns the deserialized []Domain. Login is ensured
// before the request; CSRF not required for GET /api/domains.
func (c *Client) ListDomains(ctx context.Context) ([]Domain, error) {
    if err := c.ensureLogin(ctx); err != nil { return nil, fmt.Errorf("hover: login: %w", err) }
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/domains", nil)
    if err != nil { return nil, fmt.Errorf("hover: ListDomains: build request: %w", err) }
    req.Header.Set("Accept", "application/json")
    resp, err := c.httpClient.Do(req)
    if err != nil { return nil, fmt.Errorf("hover: ListDomains: %w", err) }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("hover: ListDomains: status %d", resp.StatusCode)
    }
    var body struct {
        Succeeded bool     `json:"succeeded"`
        Domains   []Domain `json:"domains"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
        return nil, fmt.Errorf("hover: ListDomains: decode: %w", err)
    }
    if !body.Succeeded { return nil, fmt.Errorf("hover: ListDomains: API returned succeeded=false") }
    return body.Domains, nil
}
```

**Step 3: Run test + commit**

```bash
GOWORK=off go test ./pkg/hoverclient/
git add pkg/hoverclient/client.go pkg/hoverclient/client_test.go
git commit -m "feat(client): add ListDomains for account-level enumeration"
```

### Task 11: Tag parent module v0.4.0

**Files:**
- (none directly; tag operation against the PARENT `workflow-plugin-hover` Go module — not a subpath tag)

**Step 1: Merge Task 10 PR first**

Task 10 added `ListDomains` to `pkg/hoverclient/client.go`. That PR must be MERGED to main with CI green before tagging. Task 12 cannot proceed until v0.4.0 is published.

This task is operationally separate from Tasks 10 + 12 — it happens BETWEEN them, after Task 10 merges and before Task 12's pin bump. In PR terms, Task 11 is a tag operation, not a PR. The plan's PR 4 row in the Scope Manifest covers Tasks 10 + 12 (one PR per Go module change); Task 11 is the inter-task tag step.

**Step 2: Tag + push**

```bash
git fetch --tags
git checkout main && git pull --ff-only
git tag v0.4.0
git push origin v0.4.0
```

**Step 3: Confirm tag visible to consumers**

```bash
git ls-remote --tags origin | grep v0.4.0
# Expect: ref/tags/v0.4.0 hash present
```

Consumers will import as `github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient` and resolve to module tag v0.4.0.

### Task 12: Implement EnumerateAll on hoverProvider

**Files:**
- Modify: `internal/iacserver.go`
- Modify: `go.mod` (bump pkg/hoverclient pin to v0.4.0)
- Test: `internal/iacserver_test.go`

**Step 1: Failing test**

```go
func TestHoverProvider_EnumerateAll_DNS(t *testing.T) {
    stub := &fakeHoverClient{
        domains: []hoverclient.Domain{
            // hoverclient.Domain field is `Name string` (tagged json:"domain_name"),
            // NOT a separate DomainName field. Verified pkg/hoverclient/client.go.
            {ID: "d-1", Name: "alpha.test"},
            {ID: "d-2", Name: "beta.test"},
        },
    }
    p := &hoverProvider{client: stub}
    out, err := p.EnumerateAll(context.Background(), "infra.dns")
    if err != nil { t.Fatalf("EnumerateAll: %v", err) }
    if len(out) != 2 { t.Fatalf("want 2; got %d", len(out)) }
    if out[0].ProviderID != "alpha.test" { t.Errorf("providerID[0] = %v", out[0].ProviderID) }
    if out[0].Outputs["zone"] != "alpha.test" { t.Errorf("zone[0]") }
}
```

Extend `fakeHoverClient` with `ListDomains(ctx context.Context) ([]hoverclient.Domain, error)` method. If pkg/hoverclient also exposes `ExpiresAt` or similar on Domain, add it to outputs after verifying the actual field via `go doc github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient.Domain`.

**Step 2: Verify fail; bump pin; implement**

```bash
# Bump pin
GOWORK=off go get github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient@v0.4.0
GOWORK=off go mod tidy
```

In `internal/iacserver.go`:

```go
func (p *hoverProvider) EnumerateAll(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
    if p.client == nil {
        return nil, fmt.Errorf("hover: EnumerateAll on uninitialized provider")
    }
    if resourceType != "infra.dns" {
        return nil, fmt.Errorf("hover: EnumerateAll: resource type %q not supported", resourceType)
    }
    domains, err := p.client.ListDomains(ctx)
    if err != nil { return nil, fmt.Errorf("hover: EnumerateAll infra.dns: %w", err) }
    out := make([]*interfaces.ResourceOutput, 0, len(domains))
    for _, d := range domains {
        outputs := map[string]any{
            "zone":      d.Name,       // hoverclient.Domain field is Name (json:"domain_name")
            "domain_id": d.ID,
        }
        // ExpiresAt / other fields: surface them ONLY after confirming via `go doc`
        // that the hoverclient.Domain struct exposes them. Cycle-2 adversarial showed
        // memory references that didn't match the actual struct.
        out = append(out, &interfaces.ResourceOutput{
            ProviderID: d.Name,
            Type:       "infra.dns",
            Outputs:    outputs,
        })
    }
    return out, nil
}
```

**Step 3: Tests + commit**

```bash
GOWORK=off go test ./internal/...
git add internal/iacserver.go internal/iacserver_test.go go.mod go.sum
git commit -m "feat(provider): implement EnumerateAll for infra.dns via pkg/hoverclient ListDomains"
```

### Task 13: Live integration test + open PR

**Files:**
- Test: `internal/iacserver_live_test.go` (new)

**Step 1: Live test + commit + push + PR**

```go
//go:build live_dns

func TestHoverProvider_EnumerateAll_DNS_live(t *testing.T) {
    if os.Getenv("INFRA_DNS_ENUMERATE_LIVE") != "1" {
        t.Skip("set INFRA_DNS_ENUMERATE_LIVE=1 + HOVER_USERNAME + HOVER_PASSWORD")
    }
    p := newLiveHoverProvider(t)
    out, err := p.EnumerateAll(context.Background(), "infra.dns")
    if err != nil { t.Fatalf("live: %v", err) }
    t.Logf("enumerated %d hover domains", len(out))
}
```

```bash
git add internal/iacserver_live_test.go
git commit -m "test(provider): env-gated live EnumerateAll infra.dns test"
git push -u origin feat/dns-enumerate-all-2026-05-26T1900
gh pr create --title "feat(provider): ListDomains + EnumerateAll for infra.dns" \
  --body "Adds pkg/hoverclient.ListDomains calling GET /api/domains; implements IaCProviderEnumerator.EnumerateAll(\"infra.dns\") via the new method. Bumps pkg/hoverclient to v0.4.0. Live test env-gated. Part of cross-repo cascade docs/plans/2026-05-26-dns-provider-contract.md (workflow-plugin-infra)." \
  --base main
```

---

## PR 4.5 — workflow-registry: pin-bump manifests for DO/CF/NC/Hover

**Repo:** `/Users/jon/workspace/workflow-registry`
**Branch:** `chore/dns-providers-pin-bump-2026-05-26T1900`

**Wait for:** PRs 1, 2, 3, 4 merged + tags published in respective repos.

### Task 13.5: Pin-bump 4 provider manifests + admin-merge

Per `feedback_version_bump_immediate_merge.md`: version-pin manifest changes auto-merge in same turn. Batch all 4 provider bumps into one PR.

**Files (cycle-4 corrected — registry uses short-name dirs WITHOUT `workflow-plugin-` prefix; verified via `ls /Users/jon/workspace/workflow-registry/plugins/`):**
- Modify: `plugins/digitalocean/manifest.json` (version pin)
- Create: `plugins/cloudflare/manifest.json` — **NEW FILE; cloudflare has no existing manifest entry in workflow-registry today.** Mirror the shape used in `plugins/digitalocean/manifest.json`. Required keys: `name`, `version`, `description`, `repository`, `downloads`, `requires`, `provides`, `capabilities`. Confirm exact schema via `cat plugins/digitalocean/manifest.json` at task start.
- Modify: `plugins/namecheap/manifest.json`
- Modify: `plugins/hover/manifest.json`

**Step 1: Bump pins to each provider's new tagged version**

For each provider manifest, update the `version:` field to the tag published after its respective PR (1-4) landed. Exact version numbers determined at PR-landing time; no hardcoded values here.

**Step 2: Validate manifests via the registry's own validation script**

```bash
./scripts/validate-manifests.sh
# Expect: each updated/created manifest reports OK
```

(Script path is `scripts/validate-manifests.sh` — verified at repo root listing.) CI runs the same script on PR open; local validation is belt-and-suspenders.

**Step 3: Commit + push + auto-merge PR**

```bash
git add plugins/digitalocean/manifest.json \
        plugins/cloudflare/manifest.json \
        plugins/namecheap/manifest.json \
        plugins/hover/manifest.json
git commit -m "chore(manifest): pin-bump DO/CF/NC/Hover providers for EnumerateAll infra.dns"
git push -u origin chore/dns-providers-pin-bump-2026-05-26T1900

# Capture PR URL+number atomically from gh pr create — no sleep, no race
PR_URL=$(gh pr create \
  --title "chore(manifest): pin-bump 4 DNS providers for EnumerateAll cascade" \
  --body "Pin-bumps four manifests after the EnumerateAll(\"infra.dns\") cascade lands. Part of docs/plans/2026-05-26-dns-provider-contract.md in workflow-plugin-infra." \
  --base main)
PR_NUM=$(printf '%s' "$PR_URL" | grep -oE '[0-9]+$')

# Per feedback_version_bump_immediate_merge: admin-merge in same turn
gh pr merge "$PR_NUM" --squash --admin --delete-branch
```

PR number captured from `gh pr create` stdout (URL ends with `/pull/<NUM>`); avoids the brittle `sleep 5` pattern (flagged by cycle-3 adversarial + harness-prohibited per `feedback_no_speculative_remote_ci.md`).

---

## PR 5 — workflow: wfctl infra import-all bulk wrapper

**Repo:** `/Users/jon/workspace/workflow`
**Branch:** `feat/wfctl-infra-import-all-2026-05-26T1900`

**Wait for**: PRs 1-4 merged + tagged + workflow-registry manifests refreshed.

### Task 14: Add command dispatch + flag parsing

**Files:**
- Modify: `cmd/wfctl/infra.go` (add `case "import-all"` to `runInfra` switch + new `runInfraImportAll` function)
- Modify: `cmd/wfctl/infra.go` (infraUsage block)

**Step 1: Failing test**

`cmd/wfctl/infra_import_all_test.go` (new):

```go
package main

import (
    "context"
    "testing"
)

func TestRunInfraImportAll_requiresProvider(t *testing.T) {
    err := runInfraImportAll([]string{})
    if err == nil || !strings.Contains(err.Error(), "--provider") {
        t.Fatalf("want --provider required error; got %v", err)
    }
}

func TestRunInfraImportAll_requiresType(t *testing.T) {
    err := runInfraImportAll([]string{"--provider", "digitalocean"})
    if err == nil || !strings.Contains(err.Error(), "--type") {
        t.Fatalf("want --type required error; got %v", err)
    }
}
```

**Step 2: Verify failure + scaffold function**

```bash
GOWORK=off go test -run TestRunInfraImportAll ./cmd/wfctl/...
# FAIL: function not defined
```

In `cmd/wfctl/infra.go` (next to `runInfraImport` at line 1021):

```go
func runInfraImportAll(args []string) error {
    fs := flag.NewFlagSet("infra import-all", flag.ContinueOnError)
    var configFile, envName, providerName, resourceType, pluginDirFlag, outputPath string
    var dryRun bool
    fs.StringVar(&configFile, "config", "", "Config file")
    fs.StringVar(&configFile, "c", "", "Config file (short)")
    fs.StringVar(&envName, "env", "", "Environment name")
    fs.StringVar(&providerName, "provider", "", "Provider name (required)")
    fs.StringVar(&resourceType, "type", "", "Resource type (required), e.g. infra.dns")
    fs.BoolVar(&dryRun, "dry-run", false, "List zones without persisting")
    fs.StringVar(&pluginDirFlag, "plugin-dir", "", "Plugin directory")
    fs.StringVar(&outputPath, "output", "", "Optional: write imported state to this file (in addition to state backend)")
    fs.StringVar(&outputPath, "o", "", "Output path (short)")
    if err := fs.Parse(args); err != nil { return err }
    if providerName == "" { return fmt.Errorf("import-all requires --provider") }
    if resourceType == "" { return fmt.Errorf("import-all requires --type (e.g. infra.dns)") }
    // TODO: dispatch via runInfraImportAllWithDeps (Task 15)
    return nil
}
```

`--output`/`-o` is consumed by the scenarios in Tasks 31-32 to capture per-run state for cross-provider diffing without re-reading the state backend.

Add `case "import-all"` to runInfra switch (around line 76):

```go
case "import-all":
    return runInfraImportAll(args[1:])
```

Update `infraUsage()` body to document the new subcommand.

**Step 3: Run unit tests + commit**

```bash
GOWORK=off go test -run TestRunInfraImportAll ./cmd/wfctl/...
# PASS
git add cmd/wfctl/infra.go cmd/wfctl/infra_import_all_test.go
git commit -m "feat(wfctl): add infra import-all subcommand scaffold + flag parsing"
```

### Task 15: Implement enumerate + iterate dispatch

**Files:**
- Modify: `cmd/wfctl/infra.go` (runInfraImportAll body)

**Step 1: Failing test**

```go
func TestRunInfraImportAll_dispatch(t *testing.T) {
    // Use a stubbed provider that implements EnumerateAll
    stub := &stubProvider{
        enumerateAll: func(ctx context.Context, rt string) ([]*interfaces.ResourceOutput, error) {
            return []*interfaces.ResourceOutput{
                {ProviderID: "alpha.test", Type: "infra.dns", Outputs: map[string]any{"zone": "alpha.test"}},
                {ProviderID: "beta.test", Type: "infra.dns", Outputs: map[string]any{"zone": "beta.test"}},
            }, nil
        },
        importFn: func(ctx context.Context, cloudID, rt string) (*interfaces.ResourceState, error) {
            // IaCProvider.Import returns *ResourceState (verified iac_provider.go:30)
            return &interfaces.ResourceState{ProviderID: cloudID, Type: rt, Name: cloudID}, nil
        },
    }
    store := newInMemoryStateStore()
    n, err := runInfraImportAllWithDeps(context.Background(), stub, store, "infra.dns", false)
    if err != nil { t.Fatalf("import-all: %v", err) }
    if n != 2 { t.Errorf("imported = %d; want 2", n) }
    if got := store.Count(); got != 2 { t.Errorf("state store count = %d; want 2", got) }
}
```

Factor the dispatch core into `runInfraImportAllWithDeps(ctx, provider, store, resourceType, dryRun) (int, error)` for testability; `runInfraImportAll` is the CLI wrapper that resolves provider + store from config.

Use the package-private `infraStateStore` interface (the same type returned by `resolveStateStore` — verify via `grep -n 'type infraStateStore' cmd/wfctl/*.go`). DO NOT use `interfaces.IaCStateStore` — that's a different (broader) interface defined in `workflow/interfaces/iac_state.go` and the package-private wfctl helper does not satisfy it.

**Step 2: Implement core**

```go
func runInfraImportAllWithDeps(ctx context.Context, provider interfaces.IaCProvider, store infraStateStore, resourceType string, dryRun bool) (int, error) {
    enumerator, ok := provider.(interfaces.EnumeratorAll)
    if !ok { return 0, fmt.Errorf("provider does not implement EnumerateAll (interfaces.EnumeratorAll)") }
    outputs, err := enumerator.EnumerateAll(ctx, resourceType)
    if err != nil { return 0, fmt.Errorf("enumerate: %w", err) }
    imported := 0
    var failures []string
    for _, o := range outputs {
        zoneName, _ := o.Outputs["zone"].(string)
        if zoneName == "" { zoneName = o.ProviderID }
        if dryRun {
            fmt.Printf("would import: provider=%s zone=%s id=%s\n", "<provider>", zoneName, o.ProviderID)
            imported++
            continue
        }
        state, ierr := provider.Import(ctx, o.ProviderID, resourceType)
        if ierr != nil {
            failures = append(failures, fmt.Sprintf("%s: %v", zoneName, ierr))
            continue
        }
        synth := buildResourceStateFromImport(zoneName, o.ProviderID, resourceType, state)
        if serr := store.SaveResource(ctx, synth); serr != nil {
            failures = append(failures, fmt.Sprintf("%s: save: %v", zoneName, serr))
            continue
        }
        imported++
    }
    if len(failures) > 0 {
        return imported, fmt.Errorf("import-all completed with %d failures:\n  %s", len(failures), strings.Join(failures, "\n  "))
    }
    return imported, nil
}
```

`buildResourceStateFromImport` synthesizes a `ResourceSpec` Name from zoneName (sanitized) + uses existing helpers from `runInfraImport`.

**Step 3: Wire CLI wrapper to deps + test + commit**

`--provider` semantic (cycle-4 finding I2): `providerName` is the `iac.provider` MODULE name declared in the config file (e.g., `"stub-A"`), NOT the plugin TYPE (e.g., `"stub"`). Resolution walks `cfg.Modules` to find the matching module, then extracts the plugin type from `modCfg["provider"]` field. Helper mirrors the existing `resolveProviderForSpec` (`workflow/cmd/wfctl/infra.go:1150`):

```go
// resolveProviderModuleByName resolves an iac.provider module by name,
// returning the plugin discriminator (from modCfg["provider"]) and the
// fully-resolved module config. Mirrors resolveProviderForSpec line-for-line
// (workflow/cmd/wfctl/infra.go:1150-1180) but indexes by module name
// instead of by spec's provider reference.
func resolveProviderModuleByName(cfgFile, envName, name string) (string, map[string]any, error) {
    cfg, err := config.LoadFromFile(cfgFile)
    if err != nil {
        return "", nil, fmt.Errorf("load %s: %w", cfgFile, err)
    }
    for i := range cfg.Modules {                 // index-range to allow pointer-to-element
        m := &cfg.Modules[i]                     // pointer needed for m.ResolveForEnv (pointer receiver)
        if m.Type != "iac.provider" || m.Name != name {
            continue
        }
        var modCfg map[string]any
        if envName != "" {
            resolved, ok := m.ResolveForEnv(envName) // returns (*ResolvedModule, bool)
            if !ok {
                return "", nil, fmt.Errorf("provider module %q is disabled for environment %q", name, envName)
            }
            modCfg = config.ExpandEnvInMapPreservingKeys(resolved.Config, infraPreserveKeys)
        } else {
            modCfg = config.ExpandEnvInMapPreservingKeys(m.Config, infraPreserveKeys)
        }
        providerType, _ := modCfg["provider"].(string)
        if providerType == "" {
            return "", nil, fmt.Errorf("provider module %q has no 'provider' type configured", name)
        }
        return providerType, modCfg, nil
    }
    return "", nil, fmt.Errorf("no iac.provider module named %q in config", name)
}
```

Three precise corrections vs. cycle-6 draft (each verified by reading `resolveProviderForSpec` source):
1. **Range over index** (`for i := range cfg.Modules` + `m := &cfg.Modules[i]`) — `ResolveForEnv` has pointer receiver `*ModuleConfig`; ranging by value would copy + can't address.
2. **`ResolveForEnv` returns `(*ResolvedModule, bool)`** — not `(*ResolvedModule, error)`. Guard via `if !ok`.
3. **`ExpandEnvInMapPreservingKeys` returns single `map[string]any`** — not `(map, error)`. Single-value assignment.

Plus `envName == ""` branch handles the no-env case using `m.Config` directly (same pattern as `resolveProviderForSpec:1171-1172`).

In `runInfraImportAll`:

```go
ctx := context.Background()
providerType, providerCfg, err := resolveProviderModuleByName(configFile, envName, providerName)
if err != nil { return err }
provider, closer, err := resolveIaCProvider(ctx, providerType, providerCfg)
if err != nil { return err }
defer closer.Close()
store, err := resolveStateStore(configFile, envName)
if err != nil { return err }
n, err := runInfraImportAllWithDeps(ctx, provider, store, resourceType, dryRun)
if outputPath != "" {
    if werr := dumpStateToFile(ctx, store, outputPath); werr != nil {
        fmt.Fprintf(os.Stderr, "warning: --output dump failed: %v\n", werr)
    }
}
fmt.Printf("imported %d zones\n", n)
return err
```

`dumpStateToFile` is a new helper (implementation below). The variable name is `configFile` (matches Task 14's flag binding), not `cfgFile`.

```go
// dumpStateToFile snapshots the current state-store contents to outputPath
// as a JSON array of ResourceState. Intended for scenario test harnesses
// that diff state across runs without re-reading the live state backend.
func dumpStateToFile(ctx context.Context, store infraStateStore, path string) error {
    resources, err := store.ListResources(ctx)
    if err != nil { return fmt.Errorf("list resources: %w", err) }
    data, err := json.MarshalIndent(map[string]any{"resources": resources}, "", "  ")
    if err != nil { return fmt.Errorf("marshal: %w", err) }
    if err := os.WriteFile(path, data, 0o600); err != nil {
        return fmt.Errorf("write %s: %w", path, err)
    }
    return nil
}
```

`infraStateStore.ListResources(ctx)` is expected to exist on the package-private interface. Verify via `grep -n 'ListResources' cmd/wfctl/infra_state_store.go` at implementation start; if absent, add it to the interface + each implementor (in-memory + on-disk) in this same task — small extension, ~30 LOC.

```bash
GOWORK=off go test ./cmd/wfctl/...
git add cmd/wfctl/infra.go cmd/wfctl/infra_import_all_test.go
git commit -m "feat(wfctl): implement infra import-all dispatch via EnumerateAll + Import"
```

### Task 16: End-to-end smoke test against real plugin

**Files:**
- Test: `cmd/wfctl/infra_import_all_e2e_test.go` (new)

**Step 1: E2E test using locally-built workflow-plugin-digitalocean v0.5.x**

```go
//go:build e2e_dns_import

package main

func TestInfraImportAll_e2e_DO(t *testing.T) {
    if os.Getenv("WFCTL_E2E_DNS_IMPORT") != "1" {
        t.Skip("set WFCTL_E2E_DNS_IMPORT=1 + DIGITALOCEAN_TOKEN")
    }
    cfg := writeTempConfig(t, /* with iac.provider.digitalocean + iac.state.local + one infra.dns spec */)
    err := runInfraImportAll([]string{"--config", cfg, "--provider", "digitalocean", "--type", "infra.dns"})
    if err != nil { t.Fatalf("e2e import-all: %v", err) }
    // Inspect state store; assert at least one resource state present
}
```

**Step 2: Smoke + commit + push**

```bash
git add cmd/wfctl/infra_import_all_e2e_test.go
git commit -m "test(wfctl): env-gated e2e for infra import-all against real plugin"
```

### Task 17: Open PR

```bash
git push -u origin feat/wfctl-infra-import-all-2026-05-26T1900
gh pr create --title "feat(wfctl): infra import-all bulk wrapper" \
  --body "Adds wfctl infra import-all subcommand. Resolves a provider plugin, calls IaCProviderEnumerator.EnumerateAll(resourceType), and iterates IaCProvider.Import for each enumerated cloud ID, persisting via the resolved state store. Per-zone failure isolated; exit nonzero only when all zones failed or no provider data returned.

Verification: unit tests cover flag parsing + dispatch; env-gated e2e exercises real workflow-plugin-digitalocean plugin loading.

Part of cross-repo cascade docs/plans/2026-05-26-dns-provider-contract.md (workflow-plugin-infra). Depends on PRs 1-4 in respective provider plugin repos." \
  --base main
```

---

## PR 6 — workflow: relocate dns policy/gate/audit + dns-policy commands + OnBeforeAction hook

**Repo:** `/Users/jon/workspace/workflow`
**Branch:** `feat/wfctl-dns-policy-2026-05-26T1900`

**Wait for**: PR 5 merged.

### Task 18: Add OnBeforeAction hook to ApplyPlanHooks

**Files:**
- Modify: `iac/wfctlhelpers/apply.go:91-110` (ApplyPlanHooks struct + callers)
- Modify: `iac/wfctlhelpers/apply.go:270-440` (per-action loop dispatch)
- Test: `iac/wfctlhelpers/apply_test.go` (extend)

**Step 1: Failing test**

```go
func TestApplyPlan_OnBeforeAction_abortsFatal(t *testing.T) {
    actions := []interfaces.PlanAction{
        {Action: "create", Resource: interfaces.ResourceSpec{Name: "rec-1", Type: "infra.dns"}},
        {Action: "create", Resource: interfaces.ResourceSpec{Name: "rec-2", Type: "infra.dns"}},
    }
    var beforeCalls int
    hooks := ApplyPlanHooks{
        OnBeforeAction: func(ctx context.Context, a interfaces.PlanAction) error {
            beforeCalls++
            if a.Resource.Name == "rec-1" { return fmt.Errorf("policy denied") }
            return nil
        },
    }
    err := applyPlanWithEnvProviderAndHooks(ctx, fakeProvider, plan, store, hooks)
    if err == nil || !strings.Contains(err.Error(), "policy denied") {
        t.Fatalf("want policy-denied fatal; got %v", err)
    }
    if beforeCalls != 1 { t.Errorf("OnBeforeAction called %d times; want 1 (abort on first)", beforeCalls) }
}
```

**Step 2: Verify fail + implement**

```bash
GOWORK=off go test -run TestApplyPlan_OnBeforeAction ./iac/wfctlhelpers/
# FAIL: field OnBeforeAction not present
```

In `iac/wfctlhelpers/apply.go:91`:

```go
type ApplyPlanHooks struct {
    OnBeforeAction    func(ctx context.Context, action interfaces.PlanAction) error // FATAL on non-nil; aborts apply
    OnResourceApplied func(...)
    OnResourceDeleted func(...)
    OnPlanComplete    func(...)
}
```

In per-action loop (line ~280):

```go
for _, action := range plan.Actions {
    if hooks.OnBeforeAction != nil {
        if err := hooks.OnBeforeAction(ctx, action); err != nil {
            return fmt.Errorf("apply aborted by pre-action hook for %s/%s: %w", action.Resource.Type, action.Resource.Name, err)
        }
    }
    // ... existing dispatch
}
```

**Step 3: Update existing callers** (those constructing `ApplyPlanHooks` literals — pass `nil` for new field; no behavior change).

Grep for `ApplyPlanHooks{` and audit each call site.

**Step 4: Run tests + commit**

```bash
GOWORK=off go test ./iac/wfctlhelpers/
git add iac/wfctlhelpers/apply.go iac/wfctlhelpers/apply_test.go
git commit -m "feat(iac): add OnBeforeAction fatal hook to ApplyPlanHooks"
```

### Task 19: Relocate dnspolicy package

**Files:**
- Create: `dns/policy/parse.go`, `dns/policy/policy.go`, `dns/policy/match.go`, `dns/policy/reader.go`, `dns/policy/writer.go`, `dns/policy/serialize.go` (copy from `workflow-plugin-infra/internal/dnspolicy/`)
- Create: `dns/policy/*_test.go` (copy tests)

**Step 1: Copy + rename package** (use perl for portability — BSD vs GNU sed differ on `-i` syntax)

```bash
mkdir -p dns/policy
cp /Users/jon/workspace/workflow-plugin-infra/internal/dnspolicy/*.go dns/policy/
perl -pi -e 's|^package dnspolicy|package policy|' dns/policy/*.go
perl -pi -e 's|github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy|github.com/GoCodeAlone/workflow/dns/policy|g' dns/policy/*.go
```

Apply same `perl -pi -e` form to Tasks 20 + 21 (dnsgate/dnsaudit relocation).

**Step 2: Verify package compiles + tests pass**

```bash
GOWORK=off go test ./dns/policy/
# Expect: all parser/serializer tests green
```

**Step 3: Commit**

```bash
git add dns/policy/
git commit -m "feat(dns/policy): relocate parser from workflow-plugin-infra/internal/dnspolicy"
```

### Task 20: Relocate dnsgate package + adapt to driver-based dispatch

**Files:**
- Create: `dns/gate/gate.go` (new; mirrors `workflow-plugin-infra/internal/dnsgate/gate.go` but dispatches via `interfaces.ResourceDriver` not `dnspolicy.Adapter`)
- Test: `dns/gate/gate_test.go`

**Step 1: Failing test**

```go
func TestGate_AllowsOwner(t *testing.T) {
    g := &Gate{
        Reader: stubReader{policyTXT: []string{"v=1; owner=ratchet; delegate=multisite:www,admin"}},
    }
    err := g.CheckAllowed(context.Background(), "example.test", "bandname", "CNAME", "multisite")
    if !errors.Is(err, ErrPolicyDenied) && err != nil {
        t.Errorf("want allow or denied error; got %v", err)
    }
    // multisite is not delegated for "bandname"; expect ErrPolicyDenied
    if err == nil { t.Error("want ErrPolicyDenied for bandname under multisite delegate; got allow") }
}
```

**Step 2: Implement** (adapts existing dnsgate logic to read TXT via `ResourceDriver.Read(zoneRef)` then parse via `dns/policy`)

```go
package gate

type Gate struct {
    Reader interface {
        GetTXT(ctx context.Context, name string) ([]string, error)
    }
}

func (g *Gate) CheckAllowed(ctx context.Context, zone, recordName, recordType, owner string) error {
    txt, err := g.Reader.GetTXT(ctx, "_workflow-dns-policy."+zone)
    if err != nil { return fmt.Errorf("read policy TXT: %w", err) }
    p, err := policy.Parse(zone, txt)
    if err != nil { return fmt.Errorf("parse policy: %w", err) }
    return p.CheckAllowed(recordName, recordType, owner)
}
```

Adapter wiring (the `Reader` interface) connects to `ResourceDriver.Read` + scanning returned record list for `TXT _workflow-dns-policy.<zone>` records. Provided in a sibling `dns/gate/driver_reader.go`:

```go
type DriverReader struct {
    Driver interfaces.ResourceDriver
    Zone   string
}

func (r *DriverReader) GetTXT(ctx context.Context, name string) ([]string, error) {
    out, err := r.Driver.Read(ctx, interfaces.ResourceRef{Type: "infra.dns", ProviderID: r.Zone})
    if err != nil { return nil, err }
    records, _ := out.Outputs["records"].([]map[string]any)
    var values []string
    for _, rec := range records {
        if rec["type"] == "TXT" && rec["name"] == name {
            if v, ok := rec["data"].(string); ok { values = append(values, v) }
        }
    }
    return values, nil
}
```

**Step 3: Tests + commit**

```bash
GOWORK=off go test ./dns/gate/
git add dns/gate/
git commit -m "feat(dns/gate): relocate gate; adapt to ResourceDriver.Read TXT scanning"
```

### Task 21: Relocate dnsaudit package

**Files:**
- Create: `dns/audit/audit.go`, `dns/audit/audit_test.go` (copy + rename from `workflow-plugin-infra/internal/dnsaudit/`)
- Audit path: now `${XDG_STATE_HOME:-$HOME/.local/state}/wfctl/plugins/wfctl/dns-audit.jsonl`

**Step 1: Copy + rename**

```bash
mkdir -p dns/audit
cp /Users/jon/workspace/workflow-plugin-infra/internal/dnsaudit/*.go dns/audit/
perl -pi -e 's|^package dnsaudit|package audit|' dns/audit/*.go
perl -pi -e 's|plugins/infra/dns-audit.jsonl|plugins/wfctl/dns-audit.jsonl|' dns/audit/audit.go
```

**Step 2: Add one-time migration**

In `dns/audit/audit.go`, on initialization (idempotent): if old path exists, append its contents to new path + leave old file in place (for follow-up cleanup). Migration logged at INFO.

**Step 3: Tests + commit**

```bash
GOWORK=off go test ./dns/audit/
git add dns/audit/
git commit -m "feat(dns/audit): relocate trail to wfctl-builtin path; one-time migration"
```

### Task 22: Add wfctl dns-policy commands (show, set, transfer-ownership, drift)

**Files:**
- Create: `cmd/wfctl/dns_policy.go`
- Test: `cmd/wfctl/dns_policy_test.go`
- Modify: `cmd/wfctl/main.go` (register "dns-policy" in `commands` map)
- Modify: `cmd/wfctl/plugin_cli_commands.go` (add "dns-policy" to `reservedCLICommands`)

**Step 1: Failing test for usage**

```go
func TestRunDNSPolicy_help(t *testing.T) {
    err := runDNSPolicy([]string{"--help"})
    // expect non-nil but with helpful text; assert each subcommand listed
    // OR runDNSPolicy returns nil with usage on stdout
}

func TestRunDNSPolicyShow_requiresZone(t *testing.T) {
    err := runDNSPolicy([]string{"show", "--provider", "digitalocean"})
    if err == nil || !strings.Contains(err.Error(), "--zone") {
        t.Fatalf("want --zone required; got %v", err)
    }
}
```

**Step 2: Scaffold + implement**

```go
package main

import (
    // ...
    "github.com/GoCodeAlone/workflow/dns/policy"
    "github.com/GoCodeAlone/workflow/dns/audit"
)

func runDNSPolicy(args []string) error {
    if len(args) < 1 { return dnsPolicyUsage() }
    switch args[0] {
    case "show":             return runDNSPolicyShow(args[1:])
    case "set":              return runDNSPolicySet(args[1:])
    case "transfer-ownership": return runDNSPolicyTransfer(args[1:])
    case "drift":            return runDNSPolicyDrift(args[1:])
    default: return dnsPolicyUsage()
    }
}

func runDNSPolicyShow(args []string) error {
    fs := flag.NewFlagSet("dns-policy show", flag.ContinueOnError)
    var configFile, envName, zone, providerName string
    fs.StringVar(&configFile, "config", "", "Config file")
    fs.StringVar(&envName, "env", "", "Environment name")
    fs.StringVar(&zone, "zone", "", "Zone (required)")
    fs.StringVar(&providerName, "provider", "", "Provider (required)")
    if err := fs.Parse(args); err != nil { return err }
    if zone == "" { return fmt.Errorf("dns-policy show requires --zone") }
    if providerName == "" { return fmt.Errorf("dns-policy show requires --provider") }
    ctx := context.Background()
    provider, closer, err := resolveIaCProvider(ctx, providerName, /* providerCfg from infra config */)
    if err != nil { return err }
    defer closer.Close()
    driver, err := provider.ResourceDriver("infra.dns")
    if err != nil { return err }
    out, err := driver.Read(ctx, interfaces.ResourceRef{Type: "infra.dns", ProviderID: zone})
    if err != nil { return fmt.Errorf("read zone: %w", err) }
    txt := extractPolicyTXT(out, zone)
    pol, err := policy.Parse(zone, txt)
    if err != nil { return fmt.Errorf("parse policy: %w", err) }
    return policy.PrintPolicy(os.Stdout, pol)
}

// Similar for Set / Transfer / Drift — mirrors existing admincli logic but reads/writes via ResourceDriver, not libdns adapter.
```

Each mutating command (`set`, `transfer-ownership`) appends to `dns/audit` JSONL trail.

**Step 3: Register builtin**

In `cmd/wfctl/main.go:74`:
```go
"dns-policy": runDNSPolicy,
```

In `cmd/wfctl/plugin_cli_commands.go:16`:
```go
var reservedCLICommands = map[string]struct{}{
    "plugin":     {},
    // ...
    "dns-policy": {}, // new
}
```

**Step 4: Tests + commit**

```bash
GOWORK=off go test ./cmd/wfctl/
git add cmd/wfctl/dns_policy.go cmd/wfctl/dns_policy_test.go cmd/wfctl/main.go cmd/wfctl/plugin_cli_commands.go
git commit -m "feat(wfctl): add dns-policy command (show/set/transfer-ownership/drift)"
```

### Task 23: Wire dns-gate as OnBeforeAction for infra.dns resources

**Files:**
- Modify: `cmd/wfctl/infra.go` (`runInfraApply` constructs `ApplyPlanHooks` — wire `OnBeforeAction` to dns/gate when `infra.dns` resources present)

**Step 1: Failing test (integration-style)**

```go
func TestRunInfraApply_dnsGateAborts(t *testing.T) {
    // Setup: plan with infra.dns resource; provider stubbed; gate Reader returns TXT denying the owner
    // Expect: applyPlan returns error with "policy denied" wrapped
}
```

**Step 2: Implement wiring**

```go
// In runInfraApply, after plan is resolved:
hooks := ApplyPlanHooks{}
hooks.OnBeforeAction = func(ctx context.Context, action interfaces.PlanAction) error {
    if action.Resource.Type != "infra.dns" { return nil }
    // Resolve driver for the zone
    driver, err := provider.ResourceDriver("infra.dns")
    if err != nil { return err }
    g := &gate.Gate{Reader: &gate.DriverReader{Driver: driver, Zone: action.Resource.ProviderID}}
    return g.CheckAllowed(ctx, action.Resource.ProviderID, /* record name from action */, /* record type */, currentOwner)
}
```

**Step 3: Tests + commit**

```bash
GOWORK=off go test ./cmd/wfctl/
git add cmd/wfctl/infra.go cmd/wfctl/infra_apply_test.go
git commit -m "feat(wfctl): wire dns-gate as OnBeforeAction for infra.dns resources during apply"
```

### Task 24: Runtime-launch validation + push branch + open PR

**Files:**
- (none directly)

**Step 1: Final test sweep**

```bash
GOWORK=off go test ./...
```

**Step 2: Build wfctl + smoke-test new commands (runtime-launch-validation)**

```bash
GOWORK=off go build -o /tmp/wfctl-smoke ./cmd/wfctl
/tmp/wfctl-smoke dns-policy --help
# Expect: exit 0; help text lists show/set/transfer-ownership/drift subcommands
/tmp/wfctl-smoke infra import-all --help
# Expect: exit 0; help text shows --provider --type --output flags
/tmp/wfctl-smoke dns-policy show
# Expect: exit nonzero with "requires --zone" error message
```

If `docker compose` integration smoke is available locally (workflow's existing `make smoke` target or similar), run it. Otherwise the binary-level help+error smoke above satisfies the gate for a CLI-class change.

**Rollback**: revert this PR; pre-Phase-3b workflow-plugin-infra still has its infra-dns plugin cliCommand intact, so admincli flows continue working via the old surface. No state migration to undo on rollback because the audit-trail migration is additive (old path retained for one release cycle).

**Step 3: Push + PR**

```bash
git push -u origin feat/wfctl-dns-policy-2026-05-26T1900
gh pr create --title "feat(wfctl): dns-policy + OnBeforeAction hook + relocate dns packages" \
  --body "Phase 3a of cross-repo DNS cascade. Highlights:

- Adds OnBeforeAction (fatal) hook to ApplyPlanHooks
- Adds workflow/dns/{policy,gate,audit} packages (relocated from workflow-plugin-infra)
- Adds wfctl dns-policy {show,set,transfer-ownership,drift} commands
- Wires dns-gate as OnBeforeAction during wfctl infra apply for infra.dns resources
- Audit trail at \${XDG_STATE_HOME}/wfctl/plugins/wfctl/dns-audit.jsonl (one-time migration from old plugins/infra/ path)

Pairs with workflow-plugin-infra strip PR (Phase 3b).

Rollback: revert commit; the workflow-plugin-infra plugin still has its admincli intact pre-Phase-3b — system continues working via the old plugin-cliCommands path.

Design: workflow-plugin-infra/docs/plans/2026-05-26-dns-provider-contract-design.md" \
  --base main
```

---

## PR 7 — workflow-plugin-infra: strip libdns + admincli + dns packages + remove dns_record step

**Repo:** `/Users/jon/workspace/workflow-plugin-infra`
**Branch:** `refactor/strip-dns-libdns-2026-05-26T1900`

**Wait for**: PR 6 merged + tagged.

### Task 25: Delete admincli + dnsprovider + dnspolicy + dnsgate + dnsaudit packages

**Files:**
- Delete: `internal/admincli/` (entire directory)
- Delete: `internal/dnsprovider/` (entire directory)
- Delete: `internal/dnspolicy/` (entire directory)
- Delete: `internal/dnsgate/` (entire directory)
- Delete: `internal/dnsaudit/` (entire directory)

**Step 1: Delete + verify no in-repo imports remain**

```bash
git rm -r internal/admincli internal/dnsprovider internal/dnspolicy internal/dnsgate internal/dnsaudit
grep -rn 'workflow-plugin-infra/internal/dns\|workflow-plugin-infra/internal/admincli' . | grep -v _worktrees
# Expect: zero hits (plugin.go callers handled in Task 26)
```

**Step 2: Commit**

```bash
git commit -m "refactor: delete admincli + dnspolicy/gate/audit/provider packages (relocated to workflow)"
```

### Task 26: Remove DNSRecordStepConfig from proto + drop step handler + drop infra.dns_record step type

**Files:**
- Modify: `internal/contracts/infra.proto` (delete `DNSRecordStepConfig` message)
- Modify: `internal/plugin.go` (delete step handler lines 130-170 + step factory registration)
- Modify: `plugin.json` (remove `infra.dns_record` from `capabilities.stepTypes`)
- Modify: any generated `*.pb.go` files (re-generate after proto change)

**Step 1: Drop proto message**

In `internal/contracts/infra.proto`:
```diff
-message DNSRecordStepConfig {
-    string provider = 1;
-    map<string, string> provider_creds = 2;
-    string zone = 3;
-    // ...
-}
```

Regenerate stubs:
```bash
make proto  # or buf generate, per repo's makefile
```

**Step 2: Drop step handler from plugin.go**

Delete the `DNSRecordStepFactory` registration + handler implementation. Verify no remaining `dnsprovider.*` references compile.

**Step 3: Drop step type from plugin.json**

```diff
-"stepTypes": ["infra.dns_record"],
+"stepTypes": [],
```

**Step 4: Compile + commit**

```bash
GOWORK=off go build ./...
git add internal/contracts/infra.proto internal/plugin.go internal/contracts/*.pb.go plugin.json
git commit -m "refactor: remove infra.dns_record step + DNSRecordStepConfig proto (peer-dispatch infeasible)"
```

### Task 27: Drop libdns deps from go.mod + remove infra-dns cliCommand

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `plugin.json` (remove `cliCommands` entry for `infra-dns`)

**Step 1: Drop deps**

```bash
GOWORK=off go mod edit -droprequire=github.com/libdns/libdns
GOWORK=off go mod edit -droprequire=github.com/libdns/cloudflare
GOWORK=off go mod edit -droprequire=github.com/libdns/digitalocean
GOWORK=off go mod edit -droprequire=github.com/libdns/namecheap
GOWORK=off go mod edit -droprequire=github.com/libdns/route53
GOWORK=off go mod edit -droprequire=github.com/libdns/googleclouddns
GOWORK=off go mod edit -droprequire=github.com/libdns/azure
# Drop any libdns/godaddy etc. shipped in v2
GOWORK=off go mod tidy
```

**Step 2: Remove infra-dns cliCommand from plugin.json**

```diff
-"cliCommands": [{ "name": "infra-dns", "description": "..." }],
+"cliCommands": [],
```

**Step 3: Build + commit**

```bash
GOWORK=off go build ./...
GOWORK=off go test ./...
git add go.mod go.sum plugin.json
git commit -m "refactor: drop libdns/* deps + infra-dns cliCommand (Phase 3b)"
```

### Task 28: Major version bump + push + PR

**Files:**
- Modify: `plugin.json` (version field)
- Modify: `internal/version.go` (if Version constant present)

**Step 1: Bump version**

```diff
-"version": "0.X.Y",
+"version": "1.0.0",
```

Capabilities surface has shrunk + proto contract broke → major bump per semver.

**Step 2: Run full test suite + version-skew audit**

```bash
GOWORK=off go test ./...
# Also run wfctl plugin verify-capabilities against the locally built binary
wfctl plugin verify-capabilities ./bin/workflow-plugin-infra
# Expect: capabilities reported match plugin.json
```

**Step 3: Commit + push + PR**

```bash
git add plugin.json internal/version.go
git commit -m "chore: bump to v1.0.0 (capability surface shrink + proto break)"
git push -u origin refactor/strip-dns-libdns-2026-05-26T1900

gh pr create --title "refactor: strip libdns + admincli + dns packages + remove dns_record step" \
  --body "Phase 3b of cross-repo DNS cascade. workflow-plugin-infra becomes a thin abstract-module-types-only plugin.

Changes:
- DELETE internal/admincli, internal/dnsprovider, internal/dnspolicy, internal/dnsgate, internal/dnsaudit
- DROP libdns/* deps from go.mod
- REMOVE infra.dns_record step type + DNSRecordStepConfig proto (peer-dispatch infeasible from step handler context; per-record workflows route through wfctl infra apply or wfctl dns-policy *)
- REMOVE infra-dns cliCommand (commands moved to wfctl dns-policy builtin in PR 6)
- BUMP to v1.0.0 (capability surface shrink + breaking proto change)

Rollback: revert commit + ensure PR 6 is also reverted (revert PR 7 first, then PR 6 — see design doc Rollback section). After revert, libdns + admincli code restored from git history.

Runtime-launch-validation: post-merge, verify wfctl loads workflow-plugin-infra v1.0.0 without dns-related capability errors + wfctl plugin verify-capabilities passes.

Design: docs/plans/2026-05-26-dns-provider-contract-design.md" \
  --base main
```

---

## PR 8 — workflow-scenarios: DNS orchestration scenarios + stub provider plugin

**Repo:** `/Users/jon/workspace/workflow-scenarios`
**Branch:** `feat/dns-orchestration-2026-05-26T1900`

**Wait for**: PR 5 + PR 6 + PR 7 merged.

**Cycle-2 finding I6**: scenarios use canonical `scenarios/<id>-<name>/` layout with `config/` + `test/` subdirs, NOT a flat `dns/` directory at repo root. Highest existing scenario ID is 88 (`88-iac-dns-replay-migration`). New DNS scenarios use IDs 89/90/91. Stub plugin (shared by all three) lives at `scenarios/lib/dns-stub-plugin/`.

### Task 29: Build stub IaCProvider gRPC plugin

**Files:**
- Create: `scenarios/lib/dns-stub-plugin/main.go`
- Create: `scenarios/lib/dns-stub-plugin/fixtures/example.yaml`
- Create: `scenarios/lib/dns-stub-plugin/go.mod` (or use parent go.work if applicable)

**Step 1: Scaffold stub plugin**

```go
package main

import (
    "context"
    "os"
    "github.com/GoCodeAlone/workflow/plugin/external/sdk"
    "github.com/GoCodeAlone/workflow/interfaces"
    "gopkg.in/yaml.v3"
)

type stubServer struct {
    fixturePath string
}

func (s *stubServer) Initialize(ctx context.Context, _ map[string]string) error { return nil }
func (s *stubServer) Name() string { return "dns-stub" }
func (s *stubServer) Version() string { return "0.0.1" }
// ... required IaCProvider methods (stub impls returning canned data from fixture)

func (s *stubServer) EnumerateAll(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
    data, _ := os.ReadFile(s.fixturePath)
    var fixture struct{ Zones []map[string]any }
    yaml.Unmarshal(data, &fixture)
    var out []*interfaces.ResourceOutput
    for _, z := range fixture.Zones {
        out = append(out, &interfaces.ResourceOutput{
            ProviderID: z["id"].(string),
            Type:       resourceType,
            Outputs:    z,
        })
    }
    return out, nil
}

func (s *stubServer) Import(ctx context.Context, cloudID, resourceType string) (*interfaces.ResourceState, error) {
    // interfaces.IaCProvider.Import returns *ResourceState (verified iac_provider.go:30)
    data, err := os.ReadFile(s.fixturePath)
    if err != nil { return nil, fmt.Errorf("stub: read fixture: %w", err) }
    var fixture struct {
        Zones []map[string]any `yaml:"zones"`
    }
    if err := yaml.Unmarshal(data, &fixture); err != nil {
        return nil, fmt.Errorf("stub: parse fixture: %w", err)
    }
    for _, z := range fixture.Zones {
        if id, _ := z["id"].(string); id == cloudID {
            return &interfaces.ResourceState{
                ProviderID:    cloudID,
                Type:          resourceType,
                Name:          cloudID,
                AppliedConfig: z, // includes records array from fixture; AppliedConfig is map[string]any per iac_state.go:37
            }, nil
        }
    }
    return nil, fmt.Errorf("stub: zone %q not found in fixture", cloudID)
}

func main() {
    fixturePath := os.Getenv("DNS_STUB_FIXTURE")
    if fixturePath == "" { fixturePath = "fixtures/example.yaml" }
    sdk.ServeIaCPlugin(&stubServer{fixturePath: fixturePath}, sdk.IaCServeOptions{BuildVersion: "0.0.1"})
}
```

**Step 2: Example fixture**

```yaml
# scenarios/lib/dns-stub-plugin/fixtures/example.yaml
zones:
  - id: alpha.test
    zone: alpha.test
    records:
      - {type: A,     name: "@",   data: "1.2.3.4", ttl: 300}
      - {type: CNAME, name: www,   data: alpha.test, ttl: 300}
      - {type: MX,    name: "@",   data: mail.alpha.test, ttl: 3600, priority: 10}
```

**Step 3: Build + commit**

```bash
cd scenarios/lib/dns-stub-plugin && GOWORK=off go build .
cd ../../..
git add scenarios/lib/dns-stub-plugin/
git commit -m "feat(scenarios): stub IaCProvider gRPC plugin for DNS orchestration tests"
```

### Task 30: Scenario 89 — dns-import-export-roundtrip

**Files:**
- Create: `scenarios/89-dns-import-export-roundtrip/scenario.yaml`
- Create: `scenarios/89-dns-import-export-roundtrip/config/app.yaml`
- Create: `scenarios/89-dns-import-export-roundtrip/test/run.sh`

**Step 1: scenario.yaml + config**

```yaml
# scenarios/89-dns-import-export-roundtrip/scenario.yaml
id: 89-dns-import-export-roundtrip
title: DNS import-export roundtrip
description: Import zone via wfctl infra import-all then plan; assert NoOp
componentsRequired:
  - workflow >= 0.65.0
  - workflow-plugin-infra >= 1.0.0
runtime: local
```

```yaml
# scenarios/89-dns-import-export-roundtrip/config/app.yaml
modules:
  - name: stub
    type: iac.provider.stub
resources:
  - name: alpha-test
    type: infra.dns
    config:
      provider: stub
      domain: alpha.test
```

**Step 2: test/run.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail
PASS=0; FAIL=0; SKIP=0
# Build stub plugin from shared lib
go build -o /tmp/dns-stub ../../lib/dns-stub-plugin/
# Point wfctl plugin discovery at /tmp (env var alternative to --plugin-dir
# per-subcommand flag — verified at workflow/cmd/wfctl/infra.go:259).
export WFCTL_PLUGIN_DIR=/tmp
# wfctl invocation: import-all then plan, assert plan is no-op
if wfctl infra import-all --config=../config/app.yaml --provider=stub --type=infra.dns; then PASS=$((PASS+1)); else FAIL=$((FAIL+1)); fi
if wfctl --plugin-dir=/tmp infra plan --config=../config/app.yaml > /tmp/plan.txt; then PASS=$((PASS+1)); else FAIL=$((FAIL+1)); fi
if grep -q "No changes" /tmp/plan.txt; then PASS=$((PASS+1)); else FAIL=$((FAIL+1)); fi
echo "PASS=$PASS FAIL=$FAIL SKIP=$SKIP"
[ $FAIL -eq 0 ]
```

PASS/FAIL/SKIP counter pattern matches existing scenarios (e.g., 64, 72).

**Step 3: Commit**

```bash
chmod +x scenarios/89-dns-import-export-roundtrip/test/run.sh
git add scenarios/89-dns-import-export-roundtrip/
git commit -m "feat(scenarios): 89 dns-import-export-roundtrip — import then plan NoOp"
```

### Task 31: Scenario 90 — dns-cross-provider-transfer

**Files:**
- Create: `scenarios/90-dns-cross-provider-transfer/scenario.yaml`
- Create: `scenarios/90-dns-cross-provider-transfer/config/source.yaml` (provider: stub-A)
- Create: `scenarios/90-dns-cross-provider-transfer/config/target.yaml` (provider: stub-B)
- Create: `scenarios/90-dns-cross-provider-transfer/config/lossiness.yaml`
- Create: `scenarios/90-dns-cross-provider-transfer/test/run.sh`
- Create: `scenarios/90-dns-cross-provider-transfer/test/verify-transfer.py`

**Step 1: Two stub providers**

Configure two stub provider instances (stub-A as DO equivalent, stub-B as CF equivalent). Each loads same record set via fixture.

**Step 2: test/run.sh — paired fixture+config pattern (no translate script, no --provider flag)**

`wfctl infra apply` derives the provider from the config file's `iac.provider.*` module declaration — there is no `--provider` flag (verified `workflow/cmd/wfctl/infra.go:1244-1295`). Use paired source/target fixtures + configs: each side has its own fully-formed `config/*.yaml` declaring the same resource set against its respective provider module.

```bash
#!/usr/bin/env bash
set -euo pipefail
PASS=0; FAIL=0; SKIP=0

# Build stub plugin + point wfctl discovery at /tmp
go build -o /tmp/dns-stub ../../lib/dns-stub-plugin/
export WFCTL_PLUGIN_DIR=/tmp

# 1. Apply source config → populate stub-A state
wfctl infra apply --config=../config/source.yaml && PASS=$((PASS+1)) || FAIL=$((FAIL+1))

# 2. Import-all from source provider; capture state
wfctl infra import-all --config=../config/source.yaml --provider=stub-A --type=infra.dns --output=/tmp/source-state.json && PASS=$((PASS+1)) || FAIL=$((FAIL+1))

# 3. Apply target config → populate stub-B state (same resource set, different provider module)
wfctl infra apply --config=../config/target.yaml && PASS=$((PASS+1)) || FAIL=$((FAIL+1))

# 4. Import-all from target provider; capture state
wfctl infra import-all --config=../config/target.yaml --provider=stub-B --type=infra.dns --output=/tmp/roundtrip-state.json && PASS=$((PASS+1)) || FAIL=$((FAIL+1))

# 5. Diff source vs roundtrip with per-(provider, record_type, field) exclusions
python3 ./verify-transfer.py /tmp/source-state.json /tmp/roundtrip-state.json ../config/lossiness.yaml && PASS=$((PASS+1)) || FAIL=$((FAIL+1))

echo "PASS=$PASS FAIL=$FAIL SKIP=$SKIP"
[ $FAIL -eq 0 ]
```

`config/source.yaml` declares `iac.provider.stub` module named `stub-A` + N `infra.dns` resources targeting it. `config/target.yaml` declares same module type named `stub-B` + same N `infra.dns` resources targeting it. Both configs share the same record set (DNS records identical) so the cross-provider parity check is meaningful. The fixtures backing each stub provider serve canned `Import` responses with matching record content.

No `translate-state-to-config.py` helper needed (cycle-3 reviewer's Option 2). The state→config schema gap was real; paired fixtures avoid it entirely.

**Step 3: Lossiness charter**

```yaml
# scenarios/90-dns-cross-provider-transfer/config/lossiness.yaml
exclude:
  - {provider: cloudflare, record_type: "*",   field: proxied}
  - {provider: namecheap,  record_type: "*",   field: email_type}
  - {provider: namecheap,  record_type: "*",   field: is_using_our_dns}
  - {provider: digitalocean, record_type: SRV, field: weight}
  - {provider: digitalocean, record_type: SRV, field: port}
  - {provider: digitalocean, record_type: CAA, field: flags}
  - {provider: digitalocean, record_type: CAA, field: tag}
matrix: [A, AAAA, CNAME, MX, TXT, SRV, CAA]   # NS excluded (apex provider-managed)
```

`verify-transfer.py` (small Python script) loads both state files + lossiness.yaml + asserts equality with the field masks applied.

**Step 4: Commit**

```bash
chmod +x scenarios/90-dns-cross-provider-transfer/test/run.sh
git add scenarios/90-dns-cross-provider-transfer/
git commit -m "feat(scenarios): 90 dns-cross-provider-transfer with lossiness charter (per-record-type exclusions)"
```

### Task 32: Scenario 91 — dns-delegation + scenarios.json registration + open PR

**Files:**
- Create: `scenarios/91-dns-delegation/scenario.yaml`
- Create: `scenarios/91-dns-delegation/config/app.yaml` (two providers, parent + child)
- Create: `scenarios/91-dns-delegation/test/run.sh`
- Modify: `scenarios.json` (register all three new scenarios: 89, 90, 91)

**Step 1: Two-zone config**

```yaml
# scenarios/91-dns-delegation/config/app.yaml — parent zone at stub-A with NS delegation; child zone at stub-B
modules:
  - {name: stub-A, type: iac.provider.stub}
  - {name: stub-B, type: iac.provider.stub}
resources:
  - name: parent-zone
    type: infra.dns
    config:
      provider: stub-A
      domain: example.test
      records:
        - {type: A,  name: "@",            data: "10.0.0.1", ttl: 300}
        - {type: NS, name: child.example.test, data: ns1.stub-b.test, ttl: 300}
        - {type: NS, name: child.example.test, data: ns2.stub-b.test, ttl: 300}
  - name: child-zone
    type: infra.dns
    config:
      provider: stub-B
      domain: child.example.test
      records:
        - {type: A,     name: "@",  data: "10.0.0.2", ttl: 300}
        - {type: CNAME, name: www,  data: child.example.test, ttl: 300}
```

**Step 2: test/run.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail
PASS=0; FAIL=0; SKIP=0

# Build stub plugin + point wfctl discovery at /tmp
go build -o /tmp/dns-stub ../../lib/dns-stub-plugin/
export WFCTL_PLUGIN_DIR=/tmp

# Single apply: both providers, both zones
wfctl infra apply --config=../config/app.yaml && PASS=$((PASS+1)) || FAIL=$((FAIL+1))
# Roundtrip: import-all each provider, assert delegation NS at parent + child records intact
wfctl infra import-all --config=../config/app.yaml --provider=stub-A --type=infra.dns --output=/tmp/parent-state.json && PASS=$((PASS+1)) || FAIL=$((FAIL+1))
wfctl infra import-all --config=../config/app.yaml --provider=stub-B --type=infra.dns --output=/tmp/child-state.json && PASS=$((PASS+1)) || FAIL=$((FAIL+1))
jq -e '.resources[] | select(.name=="parent-zone") | .applied_config.records[] | select(.type=="NS" and .name=="child.example.test")' /tmp/parent-state.json && PASS=$((PASS+1)) || FAIL=$((FAIL+1))
jq -e '.resources[] | select(.name=="child-zone") | (.applied_config.records | length) >= 2' /tmp/child-state.json && PASS=$((PASS+1)) || FAIL=$((FAIL+1))
echo "PASS=$PASS FAIL=$FAIL SKIP=$SKIP"
[ $FAIL -eq 0 ]
```

**Step 3: Register in scenarios.json**

Add entries for IDs 89, 90, 91 to the `scenarios` map in `scenarios.json` (mirror the existing entry shape used by scenarios 80-88; check structure at `cat scenarios.json | jq '.scenarios | to_entries[-3:]'`).

**Step 4: Push + open PR**

```bash
chmod +x scenarios/91-dns-delegation/test/run.sh
git add scenarios/91-dns-delegation/ scenarios.json
git commit -m "feat(scenarios): 91 dns-delegation + register 89/90/91 in scenarios.json"
git push -u origin feat/dns-orchestration-2026-05-26T1900

gh pr create --title "feat(scenarios): 3 DNS orchestration scenarios (89/90/91) + stub provider plugin" \
  --body "Phase 4 of cross-repo DNS cascade. Three new scenarios:

- 89-dns-import-export-roundtrip: import zone then plan; assert NoOp
- 90-dns-cross-provider-transfer: import from source provider; apply to target; assert (type, name, data, ttl) parity per the lossiness charter (per-record-type field exclusions)
- 91-dns-delegation: parent zone at one provider with NS records pointing to second provider's nameservers; child zone at second provider; single wfctl infra apply manages both

Stub IaCProvider gRPC plugin at scenarios/lib/dns-stub-plugin/ serves canned EnumerateAll/Import from YAML fixtures — runs locally without cloud creds.

Multi-component validation: scenarios exercise real wfctl + real plugin loading via stub plugin processes. HTTP mocks are insufficient for the IaC strict-contract path.

Pattern precedent for multi-provider single-config: scenario 66-iac-multi-cloud.

Design: workflow-plugin-infra/docs/plans/2026-05-26-dns-provider-contract-design.md" \
  --base main
```

---

## Post-merge follow-ups (not in this plan)

- Phase 5: gocodealone-dns catalog refresh design + plan (separate). Trigger: after PRs 1-8 merged. Initial catalog activation via DO + Hover (creds available); CF + NC activation pending operator-provided creds.
- aws/azure/gcp/godaddy/route53 EnumerateAll for infra.dns (follow-up plans per provider, same shape as PR 1-4).
- Workflow SDK extension for `engine.ResolveProvider` from plugin step handlers — if a future per-record DNS step type is required, this SDK gap (cycle-3 I-NEW-1) must be closed first.

---

## Change Log

| Date | Author | Change |
|---|---|---|
| 2026-05-26 | codingsloth@pm.me | Initial plan draft (cycle 1). Mirrors design cycle 3.5. 8 PRs, 32 tasks, 6 repos. |
| 2026-05-26 | codingsloth@pm.me | Plan cycle 7 — addresses 3 compile errors in cycle-6 helper. (C1-C3-CYCLE6) `resolveProviderModuleByName` helper rewritten as exact line-for-line mirror of existing `resolveProviderForSpec` (`workflow/cmd/wfctl/infra.go:1150-1180`) after reading the actual source. Fixes: (a) `for i := range cfg.Modules` + `m := &cfg.Modules[i]` (pointer receiver requires addressable element, range-by-value can't satisfy); (b) `resolved, ok := m.ResolveForEnv(envName)` with `if !ok` guard (returns `(*ResolvedModule, bool)` not `error`); (c) `modCfg := config.ExpandEnvInMapPreservingKeys(...)` single-value assignment (returns `map[string]any`, not `(map, error)`); (d) added `envName == ""` branch using `m.Config` directly. |
| 2026-05-26 | codingsloth@pm.me | Plan cycle 6 — addresses adversarial cycle 5 findings (all 3 fact-verified before applying). (C1-CYCLE5) `resolveProviderModuleByName` was returning `m.Type` (always literal `"iac.provider"`) but the caller needs the plugin discriminator from `modCfg["provider"].(string)` — verified by reading `resolveProviderForSpec` at `infra.go:1174`. Helper rewritten to mirror the existing function's resolution pattern including `m.ResolveForEnv(envName)` + `config.ExpandEnvInMapPreservingKeys(...)`. (C2-CYCLE5) `loadConfig(cfgFile, envName)` doesn't exist; corrected to `config.LoadFromFile(cfgFile)` (single arg) — verified `infra.go:221`. (I1-CYCLE5) `--plugin-dir` is per-subcommand flag not global — verified at `infra.go:258-259`. Switched all scenario run.sh scripts to use `export WFCTL_PLUGIN_DIR=/tmp` env var at top (verified env var support same line). Cleaner than per-invocation flag. |
| 2026-05-26 | codingsloth@pm.me | Plan cycle 5 — addresses adversarial cycle 4 findings (verified facts against actual repos before applying). (C1-CYCLE4) workflow-registry layout: `plugins/<short-name>/manifest.json` (NO `workflow-plugin-` prefix; verified by `ls plugins/`). Cloudflare DOES NOT exist in registry today — Task 13.5 reframed as `plugins/cloudflare/manifest.json` CREATE + 3 existing modifies. `validate-manifests.sh` path is `scripts/validate-manifests.sh` (verified). (C2-CYCLE4) `interfaces.IaCProvider.Import` returns `(*ResourceState, error)` not `(*ResourceOutput, error)` (verified iac_provider.go:30). Task 15 importFn stub + Task 29 stub plugin Import method both corrected. (I1-CYCLE4) Task 32 jq paths corrected: `.applied_config.records` not `.config.records` (ResourceState field tag is `json:"applied_config"` per iac_state.go:37); operator position fixed for length check. (I2-CYCLE4) `--provider` semantic explicitly specified: module name, not plugin type. `resolveProviderModuleByName` helper specified inline (~12 LOC). (M3) `--plugin-dir=/tmp` added to all `wfctl infra apply` + `wfctl infra import-all` invocations in scenario run.sh scripts. |
| 2026-05-26 | codingsloth@pm.me | Plan cycle 4 — addresses adversarial cycle 3 findings. (C1-CYCLE3) `wfctl infra apply` has NO `--provider` flag — provider derives from config's `iac.provider` module. Task 31 reworked to paired source/target config pattern (no translate script). (I1-CYCLE3) workflow-registry stores manifests at `plugins/<name>/manifest.json`, NOT `manifests/*.yaml`. Task 13.5 paths + format corrected; uses repo's own `validate-manifests.sh` for preflight. (I2-CYCLE3) `sleep 5` dropped; PR number captured atomically from `gh pr create` stdout. (I3-CYCLE3) `translate-state-to-config.py` helper deleted entirely (reviewer Option 2 — paired fixture+config pair eliminates state→config schema gap). (M1) `sed -i ''` BSD-only → `perl -pi -e` for portability across Tasks 19/20/21. (M2) stub plugin Import method now has explicit YAML fixture lookup implementation, not a TODO placeholder. |
| 2026-05-26 | codingsloth@pm.me | Plan cycle 3 — addresses adversarial cycle 2 findings. (C2-NEW) Task 15 runtime dispatch now asserts `interfaces.EnumeratorAll` not `interfaces.Enumerator` (the original I1 fix was applied to the compile-time assertion only, missed the runtime dispatch). (C3-NEW) CF test stub dropped reference to nonexistent `pagination.NewArrayAutoPagerFromSlice`; instead define minimal `zonePager` interface (`Next() bool / Current() Zone / Err() error`) that both the real AutoPager and a `slicePager` test fake satisfy. (C4-NEW) `wfctl plugin registry-validate` doesn't exist; replaced with explicit Go-test or `wfctl plugin validate <path>` per-manifest fallback. (I1-CYCLE2) Task 31 reworked to use config-driven cross-provider apply path (no `--state-file` flag; added `translate-state-to-config.py` helper). (I2-CYCLE2) `dumpStateToFile` now has explicit implementation block w/ `infraStateStore.ListResources` extension note. |
| 2026-05-26 | codingsloth@pm.me | Plan cycle 2 — addresses adversarial cycle 1 findings. (C1) CF cfProvider injected `zones zoneListerCF` interface field; iterator pattern uses cloudflare-go/v7 AutoPager Next()/Current()/Err() not iter.Seq2. (C2) NC types corrected: `DomainsGetListCommandResponse`, `IsOurDNS *bool`, `Expires *DateTime`, subservice `client.Domains.GetList`, `*int` pointer args. (C3) DO `Links.CurrentPage()` single int return; loop terminates via `IsLastPage()`. (C4) Hover prerequisite git pull added; module-path note clarifies pkg/hoverclient is subpath of parent module (tag parent at v0.4.0); fixed `Domain.Name` field (was `DomainName`). (C5) `--output`/`-o` flag added to import-all; scenarios use it. (I1) interface assertion `interfaces.EnumeratorAll` not `Enumerator`. (I2) `infraStateStore` (package-private) not `interfaces.IaCStateStore`. (I3) variable `configFile` not `cfgFile`. (I4) Task 24 runtime-launch-validation step added: build wfctl + invoke --help on new commands + verify --required error paths. (I5) PR 4.5 + Task 13.5 added for workflow-registry pin-bump batched per `feedback_version_bump_immediate_merge.md`. (I6) scenario layout normalized to canonical `scenarios/<id>-<name>/{scenario.yaml,config/,test/}` with IDs 89/90/91; stub plugin moved to `scenarios/lib/dns-stub-plugin/`; PASS/FAIL/SKIP counter pattern adopted; scenarios.json registration tasked. Plan now 9 PRs, 33 tasks, 7 repos. |
