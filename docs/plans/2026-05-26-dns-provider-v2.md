# DNS provider v2 Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Extend `internal/dnsprovider/NewAdapter` from 2 → 8 providers (Route53, GCP Cloud DNS, Azure DNS, Namecheap, GoDaddy, Hover) with per-provider unit tests + cred-key docs.

**Architecture:** Refactor `adapter.go` switch into an `init()`-based registry-map so each provider file self-registers — eliminates per-PR merge contention on the central switch. Per-provider adapter file under `internal/dnsprovider/`. Each implements `dnspolicy.Adapter` (= `DNSPolicyReader + DNSRecordWriter`). 5 providers wrap libdns directly; Hover wraps `pkg/hoverclient` (custom HTTP, extracted via workflow-plugin-hover#25). One file per provider; one docs file per provider — zero adapter.go conflicts across PRs.

**Tech Stack:** Go 1.23, `github.com/libdns/{route53 v1.6.2, googleclouddns v1.2.0, azure v0.5.0, namecheap v1.0.0, godaddy v1.1.0}`, `github.com/libdns/libdns v1.1.1`, `github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient` (post-extraction).

**Base branch:** master (each PR branches from master at HEAD of plan-creation commit `edfb376`; `go mod tidy` after rebase if go.sum diverges).

**Worktree:** `/Users/jon/workspace/_worktrees/wf-infra-dns-provider-v2` (single shared worktree; per-PR sub-worktree NOT used — implementer switches branches in this worktree between PRs).

---

## Scope Manifest

**PR Count:** 6
**Tasks:** 6
**Estimated Lines of Change:** ~1900 (informational)

**Out of scope:**
- Live cloud integration tests (deferred to v3)
- AWS assume-role chain (`assume_role_arn`) — v3
- GCP inline JSON cred form — v3
- Provider aliases (`aws`/`gcp`/`azure` shorthand) — v3
- Cloudflare migration to multi-cred — v1 single-token preserved
- gocodealone-dns mirror extension — separate work
- workflow#779 cross-driver ownership tagging beyond DNS — separate work
- `priority` arg for non-MX/SRV record types (silently dropped per v1 precedent; v3 followup)
- Engine-startup smoke test (`docker compose up + curl /healthz`) — pure-Go-library extension does not change plugin gRPC surface; build + unit + plugin-binary-builds is the agreed verification class. Workspace guidance §"Multi-Component Validation" deviation explicitly acknowledged.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(dnsprovider): registry-map + Route53 adapter + docs index | Task 1 | feat/dns-provider-v2-route53 |
| 2 | feat(dnsprovider): GCP Cloud DNS adapter | Task 2 | feat/dns-provider-v2-gcp |
| 3 | feat(dnsprovider): Azure DNS adapter | Task 3 | feat/dns-provider-v2-azure |
| 4 | feat(dnsprovider): Namecheap adapter | Task 4 | feat/dns-provider-v2-namecheap |
| 5 | feat(dnsprovider): GoDaddy adapter | Task 5 | feat/dns-provider-v2-godaddy |
| 6 | feat(dnsprovider): Hover adapter | Task 6 | feat/dns-provider-v2-hover |

**Status:** Draft

**Parallelism note**: after PR 1 lands (registry refactor + Route53), PRs 2-6 each add ONE provider file + ONE docs file. Zero conflicts on `adapter.go`. Only conflict surface is `go.mod`/`go.sum` (additive only; `go mod tidy` after rebase resolves). PRs 2-6 genuinely parallelizable post-PR-1.

---

## Global Design Guidance

Source: `/Users/jon/workspace/docs/design-guidance.md`

| guidance | plan response |
|---|---|
| Go stdlib-first | All adapters Go; only new deps are libdns adapters + pkg/hoverclient |
| Dogfood workflow ecosystem | All work within existing `internal/dnsprovider/` package; no new binaries |
| Reuse over rebuild | Hover via `pkg/hoverclient` (extract issue filed) |
| libdns isolated in `internal/<provider>/` | Per v1 precedent: single file under `internal/dnsprovider/<provider>.go` (extension of established v1 layout) |
| Secrets never logged | Each adapter wraps upstream errors with `(creds redacted)` suffix; missing-cred errors name only the key |
| Cross-driver parity | All 6 implement `dnspolicy.Adapter` interface exactly (compile-checked via `var _ dnspolicy.Adapter = (*<prov>Adapter)(nil)` per file) |
| No mock-first | Per user choice: stub libdns providers for v2 (3rd-party API = legitimate stub boundary per guidance) |
| Cost discipline | No live cloud calls in CI |
| Plugin minEngineVersion | Unchanged (no engine ABI change) |
| Multi-component validation (`docker compose up`) | EXPLICITLY DEFERRED per Out-of-scope. Justification: v2 = pure Go library extension; no gRPC surface change, no startup config change, no engine ABI change. Build + unit + plugin-binary-compile is class-appropriate for this change. End-to-end exercise when first multisite tenant adopts a v2 provider. |

---

## Shared implementation patterns (read once; tasks reference)

### Adapter shape (canonical: `internal/dnsprovider/digitalocean.go` + `cloudflare.go`)

```go
package dnsprovider

import (
    "context"
    "fmt"
    "time"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
    libdns<prov> "github.com/libdns/<provider>"
    "github.com/libdns/libdns"
)

// Compile-time interface check (closes plan-cycle-1 C-3).
var _ dnspolicy.Adapter = (*<prov>Adapter)(nil)

// Self-registration via init (closes plan-cycle-1 C-2 — eliminates adapter.go switch contention).
func init() { Register("<provider-key>", new<Prov>Adapter) }

type <prov>Adapter struct {
    provider *libdns<prov>.Provider
}

func new<Prov>Adapter(creds map[string]string) (dnspolicy.Adapter, error) {
    c := ExpandCredsMap(creds)
    // validate required cred keys; per missing → "<prov>: missing creds.<key>"
    return &<prov>Adapter{provider: &libdns<prov>.Provider{...}}, nil
}

// Interface impls — GetTXT/UpsertTXT/UpsertRecord/DeleteRecord — match v1 doAdapter shape.
```

### Test pattern (canonical: `digitalocean_test.go` lines 1-77)

```go
// Per-provider stub iface that the libdns Provider satisfies.
type <prov>ProviderIface interface {
    GetRecords(context.Context, string) ([]libdns.Record, error)
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}

type stub<Prov>Provider struct {
    existing  []libdns.Record
    setCalls  [][]libdns.Record
    getCalls  int
}
// (impl iface methods; record calls; libdns.Provider satisfies same iface.)

// Sanity assertion: libdns Provider satisfies our stub iface.
var _ <prov>ProviderIface = (*libdns<prov>.Provider)(nil)

// Round-trip helper: mirrors adapter's UpsertTXT but accepts the stub iface.
func upsertTXTViaProvider<Prov>(ctx context.Context, p <prov>ProviderIface, zone, relName string, values []string, ttl int) error {
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
    }
    _, err := p.SetRecords(ctx, zone, recs)
    return err
}
```

### Cred-validation pattern

Each adapter constructor: read keys via `ExpandCredsMap`, validate required, return `<prov>: missing creds.<key>` per missing key. `ExpandCredsMap` applies `os.ExpandEnv` (unset → empty string, ambient fallback works).

---

### Task 1: Registry refactor + Route53 adapter + docs index

**Files:**
- Modify: `internal/dnsprovider/adapter.go` (refactor switch → registry-map)
- Modify: `internal/dnsprovider/digitalocean.go` (add `init() { Register("digitalocean", ...) }`)
- Modify: `internal/dnsprovider/cloudflare.go` (add `init() { Register("cloudflare", ...) }`)
- Create: `internal/dnsprovider/registry_test.go` (test registry semantics)
- Create: `internal/dnsprovider/route53.go`
- Create: `internal/dnsprovider/route53_test.go`
- Create: `docs/providers/README.md`
- Create: `docs/providers/route53.md`
- Modify: `go.mod`, `go.sum` (add `github.com/libdns/route53 v1.6.2`)

**Cred mapping (verified 2026-05-26 via `go doc github.com/libdns/route53.Provider`):**

| YAML cred key | upstream field | required? |
|---|---|---|
| `region` | `Region` (`region`) | yes |
| `access_key_id` | `AccessKeyId` (`access_key_id`) | yes unless ambient |
| `secret_access_key` | `SecretAccessKey` (`secret_access_key`) | yes unless ambient |
| `session_token` | `SessionToken` (`session_token`) | optional |
| `profile` | `Profile` (`profile`) | optional (alternative to access_key) |

**Step 1: Refactor adapter.go to registry-map**

Replace entire file with:

```go
package dnsprovider

import (
    "errors"
    "fmt"
    "sort"
    "strings"
    "sync"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
)

// ErrUnknownProvider is returned when NewAdapter receives a provider name
// that has no registered adapter.
var ErrUnknownProvider = errors.New("dnsprovider: unknown provider")

// AdapterFactory constructs a dnspolicy.Adapter from a creds map.
type AdapterFactory func(creds map[string]string) (dnspolicy.Adapter, error)

var (
    factoriesMu sync.RWMutex
    factories   = map[string]AdapterFactory{}
)

// Register registers an adapter factory for a provider key (case-folded).
// Called from each provider file's init().
func Register(provider string, f AdapterFactory) {
    factoriesMu.Lock()
    defer factoriesMu.Unlock()
    factories[strings.ToLower(strings.TrimSpace(provider))] = f
}

// NewAdapter dispatches on provider name (case-folded) + creds map.
// Providers self-register via init(). Unknown providers return
// ErrUnknownProvider with the supported list.
func NewAdapter(provider string, creds map[string]string) (dnspolicy.Adapter, error) {
    key := strings.ToLower(strings.TrimSpace(provider))
    factoriesMu.RLock()
    f, ok := factories[key]
    factoriesMu.RUnlock()
    if !ok {
        return nil, fmt.Errorf("%w %q (supported: %s)", ErrUnknownProvider, provider, supportedList())
    }
    return f(creds)
}

func supportedList() string {
    factoriesMu.RLock()
    defer factoriesMu.RUnlock()
    keys := make([]string, 0, len(factories))
    for k := range factories {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    return strings.Join(keys, ", ")
}
```

**Step 2: Wire DO + CF into registry via init()**

Edit `internal/dnsprovider/digitalocean.go` — add after package/import block:

```go
func init() { Register("digitalocean", newDigitalOceanAdapter) }

// Compile-time interface check.
var _ dnspolicy.Adapter = (*doAdapter)(nil)
```

Edit `internal/dnsprovider/cloudflare.go` — add equivalent:

```go
func init() { Register("cloudflare", newCloudflareAdapter) }

// Compile-time interface check.
var _ dnspolicy.Adapter = (*cfAdapter)(nil)
```

**Step 3: Write failing registry tests (registry_test.go)**

```go
package dnsprovider

import (
    "errors"
    "strings"
    "testing"
)

func TestNewAdapter_UnknownProviderListsSupported(t *testing.T) {
    _, err := NewAdapter("nonexistent", map[string]string{})
    if !errors.Is(err, ErrUnknownProvider) {
        t.Fatalf("want ErrUnknownProvider, got %v", err)
    }
    // After PR 1 the supported list must include the v1 providers we re-registered.
    msg := err.Error()
    for _, want := range []string{"digitalocean", "cloudflare", "route53"} {
        if !strings.Contains(msg, want) {
            t.Errorf("supported list missing %q in error: %s", want, msg)
        }
    }
}

func TestNewAdapter_RegistryIsSorted(t *testing.T) {
    // supportedList sorts keys for deterministic error messages.
    list := supportedList()
    parts := strings.Split(list, ", ")
    for i := 1; i < len(parts); i++ {
        if parts[i-1] >= parts[i] {
            t.Errorf("supported list not sorted: %v", parts)
            break
        }
    }
}
```

**Step 4: Write failing Route53 tests (route53_test.go)**

```go
package dnsprovider

import (
    "context"
    "strings"
    "testing"

    libdnsr53 "github.com/libdns/route53"
    "github.com/libdns/libdns"
)

func TestNewRoute53Adapter_RequiresRegion(t *testing.T) {
    _, err := newRoute53Adapter(map[string]string{
        "access_key_id":     "AKIA",
        "secret_access_key": "secret",
    })
    if err == nil || !strings.Contains(err.Error(), "creds.region") {
        t.Errorf("want missing-region error, got %v", err)
    }
}

func TestNewRoute53Adapter_AmbientCredsOK(t *testing.T) {
    a, err := newRoute53Adapter(map[string]string{"region": "us-east-1"})
    if err != nil { t.Fatalf("ambient mode rejected: %v", err) }
    if a == nil { t.Fatal("nil adapter") }
}

func TestNewRoute53Adapter_MapsFieldsExact(t *testing.T) {
    a, err := newRoute53Adapter(map[string]string{
        "region": "us-east-1", "access_key_id": "AKIA",
        "secret_access_key": "secret", "session_token": "tok", "profile": "p",
    })
    if err != nil { t.Fatalf("construct: %v", err) }
    r := a.(*route53Adapter)
    checks := map[string]string{
        "Region": r.provider.Region, "AccessKeyId": r.provider.AccessKeyId,
        "SecretAccessKey": r.provider.SecretAccessKey, "SessionToken": r.provider.SessionToken,
        "Profile": r.provider.Profile,
    }
    want := map[string]string{"Region": "us-east-1", "AccessKeyId": "AKIA", "SecretAccessKey": "secret", "SessionToken": "tok", "Profile": "p"}
    for k, v := range want {
        if checks[k] != v { t.Errorf("%s: got %q want %q", k, checks[k], v) }
    }
}

func TestNewAdapter_Route53Dispatch(t *testing.T) {
    a, err := NewAdapter("route53", map[string]string{"region": "us-east-1"})
    if err != nil || a == nil { t.Fatalf("dispatch route53: %v / nil=%v", err, a == nil) }
    a2, err := NewAdapter("Route53", map[string]string{"region": "us-east-1"})
    if err != nil || a2 == nil { t.Fatalf("case-fold Route53: %v / nil=%v", err, a2 == nil) }
}

// Stub iface + round-trip test (locks the libdns boundary semantics per Important I-1).
type r53ProviderIface interface {
    GetRecords(context.Context, string) ([]libdns.Record, error)
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}

// Sanity: libdns Provider satisfies our stub iface (catches upstream API churn).
var _ r53ProviderIface = (*libdnsr53.Provider)(nil)

type stubR53Provider struct {
    existing []libdns.Record
    setCalls [][]libdns.Record
    delCalls [][]libdns.Record
}

func (s *stubR53Provider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) { return s.existing, nil }
func (s *stubR53Provider) SetRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
    s.setCalls = append(s.setCalls, r); return r, nil
}
func (s *stubR53Provider) DeleteRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
    s.delCalls = append(s.delCalls, r); return r, nil
}
func (s *stubR53Provider) AppendRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
    return r, nil
}

func TestRoute53_StubRoundTrip_UpsertTXT(t *testing.T) {
    // Exercises the adapter's actual UpsertTXT path via stub-iface injection.
    // Uses package-private helper that mirrors adapter shape.
    stub := &stubR53Provider{}
    err := upsertTXTViaR53(context.Background(), stub, "example.com", "_workflow-dns-policy", []string{"v=wfinfra-v1 o=sre"}, 300)
    if err != nil { t.Fatalf("upsert: %v", err) }
    if len(stub.setCalls) != 1 { t.Errorf("SetRecords calls: %d, want 1", len(stub.setCalls)) }
    if len(stub.setCalls[0]) != 1 || stub.setCalls[0][0].RR().Type != "TXT" || stub.setCalls[0][0].RR().Data != "v=wfinfra-v1 o=sre" {
        t.Errorf("SetRecords payload wrong: %+v", stub.setCalls[0])
    }
}
```

**Step 5: Run tests to verify fail**

Run: `cd /Users/jon/workspace/_worktrees/wf-infra-dns-provider-v2 && GOWORK=off go test ./internal/dnsprovider/...`
Expected: FAIL — `undefined: Register, supportedList, newRoute53Adapter, route53Adapter, upsertTXTViaR53`.

**Step 6: Add libdns/route53 dep**

Run: `GOWORK=off go get github.com/libdns/route53@v1.6.2 && GOWORK=off go mod tidy`
Expected: go.sum updated.

**Step 7: Implement route53.go**

```go
package dnsprovider

import (
    "context"
    "fmt"
    "time"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
    libdnsr53 "github.com/libdns/route53"
    "github.com/libdns/libdns"
)

var _ dnspolicy.Adapter = (*route53Adapter)(nil)

func init() { Register("route53", newRoute53Adapter) }

type route53Adapter struct {
    provider *libdnsr53.Provider
}

func newRoute53Adapter(creds map[string]string) (dnspolicy.Adapter, error) {
    c := ExpandCredsMap(creds)
    region := c["region"]
    if region == "" {
        return nil, fmt.Errorf("route53: missing creds.region (see docs/providers/route53.md)")
    }
    return &route53Adapter{provider: &libdnsr53.Provider{
        Region:          region,
        AccessKeyId:     c["access_key_id"],
        SecretAccessKey: c["secret_access_key"],
        SessionToken:    c["session_token"],
        Profile:         c["profile"],
    }}, nil
}

// upsertTXTViaR53 is the package-private RRset-replace helper that mirrors
// route53Adapter.UpsertTXT for stub-injection round-trip testing.
func upsertTXTViaR53(ctx context.Context, p interface {
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}, zone, relName string, values []string, ttl int) error {
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
    }
    _, err := p.SetRecords(ctx, zone, recs)
    return err
}

func (a *route53Adapter) GetTXT(ctx context.Context, name string) ([]string, error) {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    recs, err := a.provider.GetRecords(ctx, zone)
    if err != nil { return nil, fmt.Errorf("route53: get records: %w (creds redacted)", err) }
    var out []string
    for _, r := range recs {
        rr := r.RR()
        if rr.Type == "TXT" && rr.Name == relName { out = append(out, rr.Data) }
    }
    return out, nil
}

func (a *route53Adapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    return upsertTXTViaR53(ctx, a.provider, zone, relName, values, ttl)
}

func (a *route53Adapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
    if priority < 0 { return "", fmt.Errorf("route53: priority must be >= 0, got %d", priority) }
    // Note: priority is currently dropped for non-MX/SRV records (matches v1 precedent).
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}}
    res, err := a.provider.SetRecords(ctx, zone, recs)
    if err != nil { return "", fmt.Errorf("route53: upsert record: %w (creds redacted)", err) }
    if len(res) > 0 { return res[0].RR().Name, nil }
    return "", nil
}

func (a *route53Adapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name}}
    if _, err := a.provider.DeleteRecords(ctx, zone, recs); err != nil {
        return fmt.Errorf("route53: delete record: %w (creds redacted)", err)
    }
    return nil
}
```

**Step 8: Add docs/providers/README.md**

```markdown
# DNS provider credentials

Per-provider cred-key documentation for `dnsprovider.NewAdapter`. Each adapter accepts a `map[string]string` of credentials. Values support `os.ExpandEnv` (`$VAR` / `${VAR}`) — unset env vars expand to empty string.

## Stability note

Adding a provider is a feature (new switch case). Removing a provider is a breaking change: per-PR revert is safe only while zero pipelines pin the removed provider key. Plugin CHANGELOG documents removal. v3 followup: emit `Deprecated` warning log from `NewAdapter` for 1 minor version before removal.

## Supported providers

- DigitalOcean — v1 (key: `digitalocean`)
- Cloudflare — v1 (key: `cloudflare`)
- [Route53 / AWS](route53.md) — v2 (key: `route53`)
- [GCP Cloud DNS](googleclouddns.md) — v2 (key: `googleclouddns`)
- [Azure DNS](azuredns.md) — v2 (key: `azuredns`)
- [Namecheap](namecheap.md) — v2 (key: `namecheap`)
- [GoDaddy](godaddy.md) — v2 (key: `godaddy`)
- [Hover](hover.md) — v2 (key: `hover`)

## `priority` argument note

`UpsertRecord` accepts a `priority int32` arg. For MX/SRV record types, libdns uses dedicated typed records; the current adapter wrappers pass `priority` through only when supported. For non-MX/SRV record types, `priority` is silently dropped (matches v1 behavior). v3 followup: typed-record dispatch for MX/SRV.
```

**Step 9: Add docs/providers/route53.md**

```markdown
# Route53 (AWS)

Provider key: `route53`

## Cred keys

| key | required | description |
|---|---|---|
| `region` | yes | AWS region (e.g. `us-east-1`) |
| `access_key_id` | optional* | AWS access key ID |
| `secret_access_key` | optional* | AWS secret access key |
| `session_token` | optional | AWS session token (for STS temp creds) |
| `profile` | optional | AWS profile name (alternative to access_key_id) |

*If `access_key_id`, `secret_access_key`, and `profile` are all empty, libdns falls back to AWS env vars (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_PROFILE`) or ambient instance/role creds.

## YAML example

```yaml
provider: route53
provider_creds:
  region: us-east-1
  access_key_id: $AWS_ACCESS_KEY_ID
  secret_access_key: $AWS_SECRET_ACCESS_KEY
```

## Notes

- AWS `assume_role_arn` deferred to v3 (requires aws-sdk-go-v2 STS chain).
- IAM policy must include `route53:ChangeResourceRecordSets`, `route53:ListResourceRecordSets`, `route53:GetChange` at minimum, scoped to the target hosted zone.
```

**Step 10: Run tests + build + vet**

```
GOWORK=off go test ./internal/dnsprovider/...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/wfinfra ./cmd/workflow-plugin-infra && ls -l /tmp/wfinfra && rm /tmp/wfinfra
```
Expected: tests PASS; vet clean; binary built (>0 bytes); removed.

**Verification (Plugin change class — agreed exception):** build + unit + plugin-binary-compile. End-to-end engine startup deferred per Out-of-scope justification.

**Rollback note:** Revert PR commit (`git revert <sha>`). `NewAdapter("route53", ...)` returns `ErrUnknownProvider`. After revert: `go mod tidy` to drop unused `libdns/route53` dep. Window: only safe while zero YAML pipelines pin `provider: route53`. Per `docs/providers/README.md` stability note, post-adoption removal needs deprecation cycle. Registry refactor itself is non-revertable in isolation (PRs 2-6 depend on it) — if PR 1 must be reverted post-PR-2-merge, revert PRs 2-6 first.

**Step 11: Commit**

```bash
git checkout -b feat/dns-provider-v2-route53
git add internal/dnsprovider/adapter.go internal/dnsprovider/registry_test.go \
        internal/dnsprovider/digitalocean.go internal/dnsprovider/cloudflare.go \
        internal/dnsprovider/route53.go internal/dnsprovider/route53_test.go \
        docs/providers/README.md docs/providers/route53.md \
        go.mod go.sum
git commit -m "feat(dnsprovider): add registry-map + Route53 adapter + docs index"
```

---

### Task 2: GCP Cloud DNS adapter

**Files (only 4 new + 2 modified):**
- Create: `internal/dnsprovider/googleclouddns.go`
- Create: `internal/dnsprovider/googleclouddns_test.go`
- Create: `docs/providers/googleclouddns.md`
- Modify: `go.mod`, `go.sum` (add `github.com/libdns/googleclouddns v1.2.0`)
- (No modification to adapter.go — registry-based self-registration.)

**Cred mapping (verified):**

| YAML cred key | upstream field | required? |
|---|---|---|
| `gcp_project` | `Project` (`gcp_project`) | yes |
| `service_account_path` | `ServiceAccountJSON` (`gcp_application_default`) | optional (omit → ADC) |

**Step 1: Write failing tests (googleclouddns_test.go)**

```go
package dnsprovider

import (
    "context"
    "strings"
    "testing"

    libdnsgcp "github.com/libdns/googleclouddns"
    "github.com/libdns/libdns"
)

func TestNewGCPAdapter_RequiresProject(t *testing.T) {
    _, err := newGoogleCloudDNSAdapter(map[string]string{"service_account_path": "/tmp/sa.json"})
    if err == nil || !strings.Contains(err.Error(), "creds.gcp_project") {
        t.Errorf("want missing-project error, got %v", err)
    }
}

func TestNewGCPAdapter_ADCMode(t *testing.T) {
    a, err := newGoogleCloudDNSAdapter(map[string]string{"gcp_project": "proj-x"})
    if err != nil { t.Fatalf("ADC mode rejected: %v", err) }
    if a == nil { t.Fatal("nil adapter") }
}

func TestNewGCPAdapter_MapsFieldsExact(t *testing.T) {
    a, err := newGoogleCloudDNSAdapter(map[string]string{
        "gcp_project": "proj-x", "service_account_path": "/etc/secrets/sa.json",
    })
    if err != nil { t.Fatalf("construct: %v", err) }
    g := a.(*gcpAdapter)
    if g.provider.Project != "proj-x" { t.Errorf("Project: %q", g.provider.Project) }
    if g.provider.ServiceAccountJSON != "/etc/secrets/sa.json" {
        t.Errorf("ServiceAccountJSON: %q", g.provider.ServiceAccountJSON)
    }
}

func TestNewAdapter_GCPDispatch(t *testing.T) {
    a, err := NewAdapter("googleclouddns", map[string]string{"gcp_project": "p"})
    if err != nil || a == nil { t.Fatalf("dispatch gcp: %v / nil=%v", err, a == nil) }
}

// Stub iface + round-trip — locks libdns boundary per I-1.
type gcpProviderIface interface {
    GetRecords(context.Context, string) ([]libdns.Record, error)
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}
var _ gcpProviderIface = (*libdnsgcp.Provider)(nil)

type stubGCPProvider struct {
    setCalls [][]libdns.Record
}
func (s *stubGCPProvider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) { return nil, nil }
func (s *stubGCPProvider) SetRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
    s.setCalls = append(s.setCalls, r); return r, nil
}
func (s *stubGCPProvider) DeleteRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) { return r, nil }
func (s *stubGCPProvider) AppendRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) { return r, nil }

func TestGCP_StubRoundTrip_UpsertTXT(t *testing.T) {
    stub := &stubGCPProvider{}
    if err := upsertTXTViaGCP(context.Background(), stub, "example.com", "_workflow-dns-policy", []string{"v=wfinfra-v1 o=sre"}, 300); err != nil {
        t.Fatalf("upsert: %v", err)
    }
    if len(stub.setCalls) != 1 { t.Errorf("SetRecords calls: %d, want 1", len(stub.setCalls)) }
}
```

**Step 2: Run tests to verify fail**

Run: `GOWORK=off go test ./internal/dnsprovider/...`
Expected: FAIL (undefined: newGoogleCloudDNSAdapter, gcpAdapter, upsertTXTViaGCP)

**Step 3: Add dep**

Run: `GOWORK=off go get github.com/libdns/googleclouddns@v1.2.0 && GOWORK=off go mod tidy`

**Step 4: Implement googleclouddns.go**

```go
package dnsprovider

import (
    "context"
    "fmt"
    "time"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
    libdnsgcp "github.com/libdns/googleclouddns"
    "github.com/libdns/libdns"
)

var _ dnspolicy.Adapter = (*gcpAdapter)(nil)

func init() { Register("googleclouddns", newGoogleCloudDNSAdapter) }

type gcpAdapter struct {
    provider *libdnsgcp.Provider
}

func newGoogleCloudDNSAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
    c := ExpandCredsMap(creds)
    project := c["gcp_project"]
    if project == "" {
        return nil, fmt.Errorf("googleclouddns: missing creds.gcp_project (see docs/providers/googleclouddns.md)")
    }
    return &gcpAdapter{provider: &libdnsgcp.Provider{
        Project:            project,
        ServiceAccountJSON: c["service_account_path"],
    }}, nil
}

func upsertTXTViaGCP(ctx context.Context, p interface {
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}, zone, relName string, values []string, ttl int) error {
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
    }
    _, err := p.SetRecords(ctx, zone, recs)
    return err
}

func (a *gcpAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    recs, err := a.provider.GetRecords(ctx, zone)
    if err != nil { return nil, fmt.Errorf("googleclouddns: get records: %w (creds redacted)", err) }
    var out []string
    for _, r := range recs {
        rr := r.RR()
        if rr.Type == "TXT" && rr.Name == relName { out = append(out, rr.Data) }
    }
    return out, nil
}

func (a *gcpAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    return upsertTXTViaGCP(ctx, a.provider, zone, relName, values, ttl)
}

func (a *gcpAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
    if priority < 0 { return "", fmt.Errorf("googleclouddns: priority must be >= 0, got %d", priority) }
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}}
    res, err := a.provider.SetRecords(ctx, zone, recs)
    if err != nil { return "", fmt.Errorf("googleclouddns: upsert record: %w (creds redacted)", err) }
    if len(res) > 0 { return res[0].RR().Name, nil }
    return "", nil
}

func (a *gcpAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name}}
    if _, err := a.provider.DeleteRecords(ctx, zone, recs); err != nil {
        return fmt.Errorf("googleclouddns: delete record: %w (creds redacted)", err)
    }
    return nil
}
```

**Step 5: Add docs/providers/googleclouddns.md**

```markdown
# GCP Cloud DNS

Provider key: `googleclouddns`

## Cred keys

| key | required | description |
|---|---|---|
| `gcp_project` | yes | GCP project ID |
| `service_account_path` | optional | Path to service-account JSON file. Omit → libdns uses Application Default Credentials (ADC) |

## YAML example

```yaml
provider: googleclouddns
provider_creds:
  gcp_project: my-gcp-project
  service_account_path: /var/secrets/sa.json
```

ADC mode (GKE workload identity, GCE metadata server, or `GOOGLE_APPLICATION_CREDENTIALS` env):
```yaml
provider: googleclouddns
provider_creds:
  gcp_project: my-gcp-project
```

## Notes

- Inline JSON cred form deferred to v3.
- IAM role: `roles/dns.admin` for the target managed zone.
```

**Step 6: Run tests + build + vet**

Same as Task 1 Step 10. Expected: PASS; vet clean; build exit 0.

**Verification:** Plugin change class — agreed exception. Same as Task 1.

**Rollback note:** Revert commit. `NewAdapter("googleclouddns", ...)` returns `ErrUnknownProvider`. `go mod tidy` after revert. Window: zero pipelines pinning `provider: googleclouddns`. Revert ordering: must revert after PR 1 (registry refactor) is still in place; do NOT revert PR 1 while PR 2 still merged.

**Step 7: Commit**

```bash
git checkout -b feat/dns-provider-v2-gcp master  # branch from master, then rebase if PR 1 still pre-merge
git add internal/dnsprovider/googleclouddns.go internal/dnsprovider/googleclouddns_test.go \
        docs/providers/googleclouddns.md go.mod go.sum
git commit -m "feat(dnsprovider): add GCP Cloud DNS adapter"
```

---

### Task 3: Azure DNS adapter

**Files:**
- Create: `internal/dnsprovider/azure.go`
- Create: `internal/dnsprovider/azure_test.go`
- Create: `docs/providers/azuredns.md`
- Modify: `go.mod`, `go.sum` (add `github.com/libdns/azure v0.5.0`)

**Cred mapping (verified — godoc confirms all-3-empty → managed identity):**

| YAML cred key | upstream field | required? |
|---|---|---|
| `subscription_id` | `SubscriptionId` (`subscription_id`) | yes |
| `resource_group_name` | `ResourceGroupName` (`resource_group_name`) | yes |
| `tenant_id` | `TenantId` (`tenant_id`) | service-principal mode |
| `client_id` | `ClientId` (`client_id`) | service-principal mode |
| `client_secret` | `ClientSecret` (`client_secret`) | service-principal mode |

Auth mode: all 3 of tenant/client/secret set → SP. All 3 empty → MI. Mixed (1 or 2 set) → reject naming missing key(s).

**Step 1: Write failing tests (azure_test.go)**

```go
package dnsprovider

import (
    "context"
    "strings"
    "testing"

    libdnsazure "github.com/libdns/azure"
    "github.com/libdns/libdns"
)

func TestNewAzureAdapter_RequiresSubscription(t *testing.T) {
    _, err := newAzureAdapter(map[string]string{"resource_group_name": "rg"})
    if err == nil || !strings.Contains(err.Error(), "creds.subscription_id") {
        t.Errorf("want missing-subscription_id, got %v", err)
    }
}

func TestNewAzureAdapter_RequiresResourceGroup(t *testing.T) {
    _, err := newAzureAdapter(map[string]string{"subscription_id": "sub"})
    if err == nil || !strings.Contains(err.Error(), "creds.resource_group_name") {
        t.Errorf("want missing-resource_group_name, got %v", err)
    }
}

func TestNewAzureAdapter_ManagedIdentityMode(t *testing.T) {
    a, err := newAzureAdapter(map[string]string{"subscription_id": "sub", "resource_group_name": "rg"})
    if err != nil { t.Fatalf("MI mode rejected: %v", err) }
    if a == nil { t.Fatal("nil adapter") }
}

func TestNewAzureAdapter_ServicePrincipalMode(t *testing.T) {
    a, err := newAzureAdapter(map[string]string{
        "subscription_id": "sub", "resource_group_name": "rg",
        "tenant_id": "t", "client_id": "c", "client_secret": "s",
    })
    if err != nil { t.Fatalf("SP mode: %v", err) }
    az := a.(*azureAdapter)
    if az.provider.TenantId != "t" || az.provider.ClientId != "c" || az.provider.ClientSecret != "s" {
        t.Errorf("SP fields wrong: %+v", az.provider)
    }
}

func TestNewAzureAdapter_PartialSPRejected(t *testing.T) {
    _, err := newAzureAdapter(map[string]string{
        "subscription_id": "sub", "resource_group_name": "rg",
        "tenant_id": "t", "client_id": "c", // client_secret missing
    })
    if err == nil || !strings.Contains(err.Error(), "client_secret") {
        t.Errorf("want partial-SP rejection naming client_secret, got %v", err)
    }
}

func TestNewAdapter_AzureDispatch(t *testing.T) {
    a, err := NewAdapter("azuredns", map[string]string{"subscription_id": "s", "resource_group_name": "rg"})
    if err != nil || a == nil { t.Fatalf("dispatch azure: %v / nil=%v", err, a == nil) }
}

// Stub round-trip per I-1.
type azProviderIface interface {
    GetRecords(context.Context, string) ([]libdns.Record, error)
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}
var _ azProviderIface = (*libdnsazure.Provider)(nil)

type stubAzProvider struct{ setCalls [][]libdns.Record }
func (s *stubAzProvider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) { return nil, nil }
func (s *stubAzProvider) SetRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
    s.setCalls = append(s.setCalls, r); return r, nil
}
func (s *stubAzProvider) DeleteRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) { return r, nil }
func (s *stubAzProvider) AppendRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) { return r, nil }

func TestAzure_StubRoundTrip_UpsertTXT(t *testing.T) {
    stub := &stubAzProvider{}
    if err := upsertTXTViaAzure(context.Background(), stub, "example.com", "_workflow-dns-policy", []string{"v=wfinfra-v1 o=sre"}, 300); err != nil {
        t.Fatalf("upsert: %v", err)
    }
    if len(stub.setCalls) != 1 { t.Errorf("SetRecords calls: %d", len(stub.setCalls)) }
}
```

**Step 2: Run tests to verify fail**

Run: `GOWORK=off go test ./internal/dnsprovider/...`
Expected: FAIL.

**Step 3: Add dep**

Run: `GOWORK=off go get github.com/libdns/azure@v0.5.0 && GOWORK=off go mod tidy`

**Step 4: Implement azure.go**

```go
package dnsprovider

import (
    "context"
    "fmt"
    "strings"
    "time"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
    libdnsazure "github.com/libdns/azure"
    "github.com/libdns/libdns"
)

var _ dnspolicy.Adapter = (*azureAdapter)(nil)

func init() { Register("azuredns", newAzureAdapter) }

type azureAdapter struct {
    provider *libdnsazure.Provider
}

func newAzureAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
    c := ExpandCredsMap(creds)
    sub := c["subscription_id"]
    rg := c["resource_group_name"]
    if sub == "" {
        return nil, fmt.Errorf("azuredns: missing creds.subscription_id (see docs/providers/azuredns.md)")
    }
    if rg == "" {
        return nil, fmt.Errorf("azuredns: missing creds.resource_group_name (see docs/providers/azuredns.md)")
    }
    tenant, client, secret := c["tenant_id"], c["client_id"], c["client_secret"]
    setCount := 0
    for _, v := range []string{tenant, client, secret} { if v != "" { setCount++ } }
    if setCount != 0 && setCount != 3 {
        var missing []string
        if tenant == "" { missing = append(missing, "tenant_id") }
        if client == "" { missing = append(missing, "client_id") }
        if secret == "" { missing = append(missing, "client_secret") }
        return nil, fmt.Errorf("azuredns: tenant_id/client_id/client_secret must all be set (service-principal) or all empty (managed-identity); missing: %s", strings.Join(missing, ","))
    }
    return &azureAdapter{provider: &libdnsazure.Provider{
        SubscriptionId:    sub,
        ResourceGroupName: rg,
        TenantId:          tenant,
        ClientId:          client,
        ClientSecret:      secret,
    }}, nil
}

func upsertTXTViaAzure(ctx context.Context, p interface {
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}, zone, relName string, values []string, ttl int) error {
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
    }
    _, err := p.SetRecords(ctx, zone, recs)
    return err
}

func (a *azureAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    recs, err := a.provider.GetRecords(ctx, zone)
    if err != nil { return nil, fmt.Errorf("azuredns: get records: %w (creds redacted)", err) }
    var out []string
    for _, r := range recs {
        rr := r.RR()
        if rr.Type == "TXT" && rr.Name == relName { out = append(out, rr.Data) }
    }
    return out, nil
}

func (a *azureAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    return upsertTXTViaAzure(ctx, a.provider, zone, relName, values, ttl)
}

func (a *azureAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
    if priority < 0 { return "", fmt.Errorf("azuredns: priority must be >= 0, got %d", priority) }
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}}
    res, err := a.provider.SetRecords(ctx, zone, recs)
    if err != nil { return "", fmt.Errorf("azuredns: upsert record: %w (creds redacted)", err) }
    if len(res) > 0 { return res[0].RR().Name, nil }
    return "", nil
}

func (a *azureAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name}}
    if _, err := a.provider.DeleteRecords(ctx, zone, recs); err != nil {
        return fmt.Errorf("azuredns: delete record: %w (creds redacted)", err)
    }
    return nil
}
```

**Step 5: Add docs/providers/azuredns.md**

```markdown
# Azure DNS

Provider key: `azuredns`

## Cred keys

| key | required | description |
|---|---|---|
| `subscription_id` | yes | Azure subscription ID |
| `resource_group_name` | yes | Resource group containing the DNS zone |
| `tenant_id` | SP only | Entra ID tenant — required for service-principal auth |
| `client_id` | SP only | App registration client ID |
| `client_secret` | SP only | App registration client secret |

## Auth modes

- **Service principal**: ALL of `tenant_id` + `client_id` + `client_secret` set.
- **Managed identity**: ALL three empty (ambient Azure managed identity, e.g. AKS workload identity).
- Mixed (1 or 2 set) → adapter rejects at construction.

## YAML examples

Service principal:
```yaml
provider: azuredns
provider_creds:
  subscription_id: $AZ_SUBSCRIPTION_ID
  resource_group_name: dns-rg
  tenant_id: $AZ_TENANT_ID
  client_id: $AZ_CLIENT_ID
  client_secret: $AZ_CLIENT_SECRET
```

Managed identity:
```yaml
provider: azuredns
provider_creds:
  subscription_id: $AZ_SUBSCRIPTION_ID
  resource_group_name: dns-rg
```

## Notes

- IAM role: "DNS Zone Contributor" on the resource group at minimum.
```

**Step 6: Run tests + build + vet**

Same as Task 1 Step 10.

**Verification:** Plugin change class — agreed exception.

**Rollback note:** Revert commit. `go mod tidy` after revert. Window: zero pipelines pinning `provider: azuredns`. Same revert-ordering caveat as Task 2.

**Step 7: Commit**

```bash
git checkout -b feat/dns-provider-v2-azure master
git add internal/dnsprovider/azure.go internal/dnsprovider/azure_test.go \
        docs/providers/azuredns.md go.mod go.sum
git commit -m "feat(dnsprovider): add Azure DNS adapter"
```

---

### Task 4: Namecheap adapter

**Files:**
- Create: `internal/dnsprovider/namecheap.go`
- Create: `internal/dnsprovider/namecheap_test.go`
- Create: `docs/providers/namecheap.md`
- Modify: `go.mod`, `go.sum` (add `github.com/libdns/namecheap v1.0.0`)

**Cred mapping (verified):**

| YAML cred key | upstream field | required? |
|---|---|---|
| `api_key` | `APIKey` (`api_key`) | yes |
| `user` | `User` (`user`) | yes |
| `client_ip` | `ClientIP` (`client_ip`) | yes (strict — no discovery fallback) |
| `api_endpoint` | `APIEndpoint` (`api_endpoint`) | optional |

**Step 1: Write failing tests (namecheap_test.go)**

```go
package dnsprovider

import (
    "context"
    "strings"
    "testing"

    libdnsnc "github.com/libdns/namecheap"
    "github.com/libdns/libdns"
)

func TestNewNamecheapAdapter_RequiresKeys(t *testing.T) {
    cases := []struct {
        in   map[string]string
        want string
    }{
        {map[string]string{"user": "u", "client_ip": "1.2.3.4"}, "creds.api_key"},
        {map[string]string{"api_key": "k", "client_ip": "1.2.3.4"}, "creds.user"},
        {map[string]string{"api_key": "k", "user": "u"}, "creds.client_ip"},
    }
    for _, tc := range cases {
        _, err := newNamecheapAdapter(tc.in)
        if err == nil || !strings.Contains(err.Error(), tc.want) {
            t.Errorf("input=%v want %q in error, got %v", tc.in, tc.want, err)
        }
    }
}

func TestNewNamecheapAdapter_MapsFieldsExact(t *testing.T) {
    a, err := newNamecheapAdapter(map[string]string{
        "api_key": "k", "user": "u", "client_ip": "1.2.3.4",
        "api_endpoint": "https://api.sandbox.namecheap.com/xml.response",
    })
    if err != nil { t.Fatalf("construct: %v", err) }
    n := a.(*namecheapAdapter)
    if n.provider.APIKey != "k" || n.provider.User != "u" || n.provider.ClientIP != "1.2.3.4" ||
        n.provider.APIEndpoint != "https://api.sandbox.namecheap.com/xml.response" {
        t.Errorf("fields: %+v", n.provider)
    }
}

func TestNewAdapter_NamecheapDispatch(t *testing.T) {
    a, err := NewAdapter("namecheap", map[string]string{"api_key": "k", "user": "u", "client_ip": "1.2.3.4"})
    if err != nil || a == nil { t.Fatalf("dispatch namecheap: %v / nil=%v", err, a == nil) }
}

// Stub round-trip per I-1.
type ncProviderIface interface {
    GetRecords(context.Context, string) ([]libdns.Record, error)
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}
var _ ncProviderIface = (*libdnsnc.Provider)(nil)

type stubNCProvider struct{ setCalls [][]libdns.Record }
func (s *stubNCProvider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) { return nil, nil }
func (s *stubNCProvider) SetRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
    s.setCalls = append(s.setCalls, r); return r, nil
}
func (s *stubNCProvider) AppendRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) { return r, nil }
func (s *stubNCProvider) DeleteRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) { return r, nil }

func TestNamecheap_StubRoundTrip_UpsertTXT(t *testing.T) {
    stub := &stubNCProvider{}
    if err := upsertTXTViaNC(context.Background(), stub, "example.com", "_workflow-dns-policy", []string{"v=wfinfra-v1 o=sre"}, 300); err != nil {
        t.Fatalf("upsert: %v", err)
    }
    if len(stub.setCalls) != 1 { t.Errorf("SetRecords calls: %d, want 1", len(stub.setCalls)) }
}
```

**Step 2: Run tests to verify fail**

Run: `GOWORK=off go test ./internal/dnsprovider/...`
Expected: FAIL.

**Step 3: Add dep**

Run: `GOWORK=off go get github.com/libdns/namecheap@v1.0.0 && GOWORK=off go mod tidy`

**Step 4: Implement namecheap.go**

```go
package dnsprovider

import (
    "context"
    "fmt"
    "time"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
    libdnsnc "github.com/libdns/namecheap"
    "github.com/libdns/libdns"
)

var _ dnspolicy.Adapter = (*namecheapAdapter)(nil)

func init() { Register("namecheap", newNamecheapAdapter) }

type namecheapAdapter struct {
    provider *libdnsnc.Provider
}

func newNamecheapAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
    c := ExpandCredsMap(creds)
    apiKey, user, clientIP := c["api_key"], c["user"], c["client_ip"]
    if apiKey == "" { return nil, fmt.Errorf("namecheap: missing creds.api_key (see docs/providers/namecheap.md)") }
    if user == "" { return nil, fmt.Errorf("namecheap: missing creds.user (see docs/providers/namecheap.md)") }
    if clientIP == "" { return nil, fmt.Errorf("namecheap: missing creds.client_ip (strict — whitelisted IP required; see docs/providers/namecheap.md)") }
    return &namecheapAdapter{provider: &libdnsnc.Provider{
        APIKey: apiKey, User: user, ClientIP: clientIP, APIEndpoint: c["api_endpoint"],
    }}, nil
}

func upsertTXTViaNC(ctx context.Context, p interface {
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}, zone, relName string, values []string, ttl int) error {
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
    }
    _, err := p.SetRecords(ctx, zone, recs)
    return err
}

func (a *namecheapAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    recs, err := a.provider.GetRecords(ctx, zone)
    if err != nil { return nil, fmt.Errorf("namecheap: get records: %w (creds redacted)", err) }
    var out []string
    for _, r := range recs {
        rr := r.RR()
        if rr.Type == "TXT" && rr.Name == relName { out = append(out, rr.Data) }
    }
    return out, nil
}

func (a *namecheapAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    return upsertTXTViaNC(ctx, a.provider, zone, relName, values, ttl)
}

func (a *namecheapAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
    if priority < 0 { return "", fmt.Errorf("namecheap: priority must be >= 0, got %d", priority) }
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}}
    res, err := a.provider.SetRecords(ctx, zone, recs)
    if err != nil { return "", fmt.Errorf("namecheap: upsert record: %w (creds redacted)", err) }
    if len(res) > 0 { return res[0].RR().Name, nil }
    return "", nil
}

func (a *namecheapAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name}}
    if _, err := a.provider.DeleteRecords(ctx, zone, recs); err != nil {
        return fmt.Errorf("namecheap: delete record: %w (creds redacted)", err)
    }
    return nil
}
```

**Step 5: Add docs/providers/namecheap.md**

```markdown
# Namecheap

Provider key: `namecheap`

## Cred keys

| key | required | description |
|---|---|---|
| `api_key` | yes | Namecheap API key (enable + whitelist IPs at namecheap.com → Profile → Tools → API Access) |
| `user` | yes | Namecheap API user (usually account username) |
| `client_ip` | **yes (strict)** | Whitelisted public IP of the calling machine. NO discovery fallback |
| `api_endpoint` | optional | API endpoint URL (defaults to production; use `https://api.sandbox.namecheap.com/xml.response` for sandbox) |

## YAML example

```yaml
provider: namecheap
provider_creds:
  api_key: $NAMECHEAP_API_KEY
  user: my-namecheap-username
  client_ip: 203.0.113.42  # whitelisted in Namecheap console
```

## Notes

- IP whitelist: enroll the calling machine's egress IP in the Namecheap API Access console before first call.
- Self-hosted runner egress IPs MUST be allocated/static and whitelisted.
- Upstream `libdns/namecheap.SetRecords` safely replaces records per (name,type) (verified via source spike 2026-05-26 — Get-merge-Set internally). Foreign-(name,type) records preserved.
```

**Step 6: Run tests + build + vet**

Same as Task 1 Step 10.

**Verification:** Plugin change class — agreed exception.

**Rollback note:** Revert commit. `go mod tidy`. Window: zero pipelines pinning `provider: namecheap`.

**Step 7: Commit**

```bash
git checkout -b feat/dns-provider-v2-namecheap master
git add internal/dnsprovider/namecheap.go internal/dnsprovider/namecheap_test.go \
        docs/providers/namecheap.md go.mod go.sum
git commit -m "feat(dnsprovider): add Namecheap adapter"
```

---

### Task 5: GoDaddy adapter

**Files:**
- Create: `internal/dnsprovider/godaddy.go`
- Create: `internal/dnsprovider/godaddy_test.go`
- Create: `docs/providers/godaddy.md`
- Modify: `go.mod`, `go.sum` (add `github.com/libdns/godaddy v1.1.0`)

**Cred mapping (verified — single field `APIToken`):**

| YAML cred key | upstream field | required? |
|---|---|---|
| `api_token` | `APIToken` (`api_token`) — format `<sso-key>:<sso-secret>` | yes |

**Step 1: Write failing tests (godaddy_test.go)**

```go
package dnsprovider

import (
    "context"
    "strings"
    "testing"

    libdnsgd "github.com/libdns/godaddy"
    "github.com/libdns/libdns"
)

func TestNewGoDaddyAdapter_RequiresToken(t *testing.T) {
    _, err := newGoDaddyAdapter(map[string]string{})
    if err == nil || !strings.Contains(err.Error(), "creds.api_token") {
        t.Errorf("want missing-api_token, got %v", err)
    }
}

func TestNewGoDaddyAdapter_RequiresColonFormat(t *testing.T) {
    _, err := newGoDaddyAdapter(map[string]string{"api_token": "bare-sso-key-no-colon"})
    if err == nil || !strings.Contains(err.Error(), "<sso-key>:<sso-secret>") {
        t.Errorf("want colon-format rejection, got %v", err)
    }
}

func TestNewGoDaddyAdapter_AcceptsConcatenatedToken(t *testing.T) {
    a, err := newGoDaddyAdapter(map[string]string{"api_token": "ssokey:ssosecret"})
    if err != nil { t.Fatalf("construct: %v", err) }
    g := a.(*godaddyAdapter)
    if g.provider.APIToken != "ssokey:ssosecret" { t.Errorf("APIToken: %q", g.provider.APIToken) }
}

func TestNewAdapter_GoDaddyDispatch(t *testing.T) {
    a, err := NewAdapter("godaddy", map[string]string{"api_token": "k:s"})
    if err != nil || a == nil { t.Fatalf("dispatch godaddy: %v / nil=%v", err, a == nil) }
}

// Stub round-trip per I-1.
type gdProviderIface interface {
    GetRecords(context.Context, string) ([]libdns.Record, error)
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}
var _ gdProviderIface = (*libdnsgd.Provider)(nil)

type stubGDProvider struct{ setCalls [][]libdns.Record }
func (s *stubGDProvider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) { return nil, nil }
func (s *stubGDProvider) SetRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
    s.setCalls = append(s.setCalls, r); return r, nil
}
func (s *stubGDProvider) AppendRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) { return r, nil }
func (s *stubGDProvider) DeleteRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) { return r, nil }

func TestGoDaddy_StubRoundTrip_UpsertTXT(t *testing.T) {
    stub := &stubGDProvider{}
    if err := upsertTXTViaGD(context.Background(), stub, "example.com", "_workflow-dns-policy", []string{"v=wfinfra-v1 o=sre"}, 300); err != nil {
        t.Fatalf("upsert: %v", err)
    }
    if len(stub.setCalls) != 1 { t.Errorf("SetRecords calls: %d, want 1", len(stub.setCalls)) }
}
```

**Step 2: Run tests to verify fail**

Run: `GOWORK=off go test ./internal/dnsprovider/...`
Expected: FAIL.

**Step 3: Add dep**

Run: `GOWORK=off go get github.com/libdns/godaddy@v1.1.0 && GOWORK=off go mod tidy`

**Step 4: Implement godaddy.go**

```go
package dnsprovider

import (
    "context"
    "fmt"
    "strings"
    "time"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
    libdnsgd "github.com/libdns/godaddy"
    "github.com/libdns/libdns"
)

var _ dnspolicy.Adapter = (*godaddyAdapter)(nil)

func init() { Register("godaddy", newGoDaddyAdapter) }

type godaddyAdapter struct {
    provider *libdnsgd.Provider
}

func newGoDaddyAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
    c := ExpandCredsMap(creds)
    token := c["api_token"]
    if token == "" {
        return nil, fmt.Errorf("godaddy: missing creds.api_token (format: \"<sso-key>:<sso-secret>\"; see docs/providers/godaddy.md)")
    }
    if !strings.Contains(token, ":") {
        return nil, fmt.Errorf("godaddy: creds.api_token must be \"<sso-key>:<sso-secret>\" (concatenated with ':'); see docs/providers/godaddy.md")
    }
    return &godaddyAdapter{provider: &libdnsgd.Provider{APIToken: token}}, nil
}

func upsertTXTViaGD(ctx context.Context, p interface {
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}, zone, relName string, values []string, ttl int) error {
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
    }
    _, err := p.SetRecords(ctx, zone, recs)
    return err
}

func (a *godaddyAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    recs, err := a.provider.GetRecords(ctx, zone)
    if err != nil { return nil, fmt.Errorf("godaddy: get records: %w (creds redacted)", err) }
    var out []string
    for _, r := range recs {
        rr := r.RR()
        if rr.Type == "TXT" && rr.Name == relName { out = append(out, rr.Data) }
    }
    return out, nil
}

func (a *godaddyAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    return upsertTXTViaGD(ctx, a.provider, zone, relName, values, ttl)
}

func (a *godaddyAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
    if priority < 0 { return "", fmt.Errorf("godaddy: priority must be >= 0, got %d", priority) }
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}}
    res, err := a.provider.SetRecords(ctx, zone, recs)
    if err != nil { return "", fmt.Errorf("godaddy: upsert record: %w (creds redacted)", err) }
    if len(res) > 0 { return res[0].RR().Name, nil }
    return "", nil
}

func (a *godaddyAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name}}
    if _, err := a.provider.DeleteRecords(ctx, zone, recs); err != nil {
        return fmt.Errorf("godaddy: delete record: %w (creds redacted)", err)
    }
    return nil
}
```

**Step 5: Add docs/providers/godaddy.md**

```markdown
# GoDaddy

Provider key: `godaddy`

## Cred keys

| key | required | description |
|---|---|---|
| `api_token` | yes | Format: `<sso-key>:<sso-secret>` (concatenated with colon). Generate at developer.godaddy.com → API Keys |

## ⚠ API access restriction

GoDaddy revoked public DNS API access for accounts with **fewer than 50 domains** (reported 2024-Q1, unresolved as of `libdns/godaddy v1.1.0` release Aug 2025). API returns 403 unauthorized for small-account holders. Test with your account before pinning to production.

## YAML example

```yaml
provider: godaddy
provider_creds:
  api_token: $GODADDY_SSO_KEY:$GODADDY_SSO_SECRET
```

## Notes

- Adapter validates colon-format at construction; runtime 403 from API surfaces as standard provider error.
- No live CI verification (per workspace cost discipline + user "unit tests only" choice).
```

**Step 6: Run tests + build + vet**

Same as Task 1 Step 10.

**Verification:** Plugin change class — agreed exception.

**Rollback note:** Revert commit. `go mod tidy`. Window: zero pipelines pinning `provider: godaddy`. If 50-domain restriction blocks all consumers, deprecate per stability policy.

**Step 7: Commit**

```bash
git checkout -b feat/dns-provider-v2-godaddy master
git add internal/dnsprovider/godaddy.go internal/dnsprovider/godaddy_test.go \
        docs/providers/godaddy.md go.mod go.sum
git commit -m "feat(dnsprovider): add GoDaddy adapter"
```

---

### Task 6: Hover adapter (blocked on workflow-plugin-hover#25)

**Prerequisite:** workflow-plugin-hover#25 ships `pkg/hoverclient` + tag. Pin tag in this task.

**Files:**
- Create: `internal/dnsprovider/hover.go`
- Create: `internal/dnsprovider/hover_test.go`
- Create: `docs/providers/hover.md`
- Modify: `go.mod`, `go.sum` (add `github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient @<tag>`)

**Cred mapping (custom client; no upstream JSON tag):**

| YAML cred key | required? |
|---|---|
| `username` | yes |
| `password` | yes |
| `totp_secret` | optional |

**Step 0: Verify pkg/hoverclient public surface (gating)**

Before any code:
```
GOWORK=off go doc github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient.Client
GOWORK=off go doc github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient.Config
GOWORK=off go doc github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient.NewClient
```
Expected: `Config` exposes fields covering `Username`/`Password`/`TOTPSecret` (or equivalent — note actual names). If field names differ from this plan, revise Steps 3+ before proceeding. If methods named differently (e.g. `ListRecords` vs `GetRecords`), revise method names in Step 3.

**Step 1: Write failing tests (hover_test.go)**

```go
package dnsprovider

import (
    "strings"
    "testing"
)

func TestNewHoverAdapter_RequiresUsername(t *testing.T) {
    _, err := newHoverAdapter(map[string]string{"password": "p"})
    if err == nil || !strings.Contains(err.Error(), "creds.username") {
        t.Errorf("want missing-username, got %v", err)
    }
}

func TestNewHoverAdapter_RequiresPassword(t *testing.T) {
    _, err := newHoverAdapter(map[string]string{"username": "u"})
    if err == nil || !strings.Contains(err.Error(), "creds.password") {
        t.Errorf("want missing-password, got %v", err)
    }
}

func TestNewHoverAdapter_AcceptsOptionalTOTP(t *testing.T) {
    a, err := newHoverAdapter(map[string]string{
        "username": "u", "password": "p", "totp_secret": "ABCD1234",
    })
    if err != nil { t.Fatalf("construct with TOTP: %v", err) }
    if a == nil { t.Fatal("nil adapter") }
}

func TestNewAdapter_HoverDispatch(t *testing.T) {
    a, err := NewAdapter("hover", map[string]string{"username": "u", "password": "p"})
    if err != nil || a == nil { t.Fatalf("dispatch hover: %v / nil=%v", err, a == nil) }
}
```

**Step 2: Run tests to verify fail**

Run: `GOWORK=off go test ./internal/dnsprovider/...`
Expected: FAIL.

**Step 3: Add dep**

Run: `GOWORK=off go get github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient@<tag-from-hover#25>`
(Replace `<tag-from-hover#25>` with actual tag — e.g. `v0.3.0`.)

**Step 4: Implement hover.go**

Exact `pkg/hoverclient.Client`/`Config`/method shape resolved at hover#25 tag via Step 0 godoc. Skeleton:

```go
package dnsprovider

import (
    "context"
    "fmt"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
    "github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient"
)

var _ dnspolicy.Adapter = (*hoverAdapter)(nil)

func init() { Register("hover", newHoverAdapter) }

type hoverAdapter struct {
    client *hoverclient.Client
}

func newHoverAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
    c := ExpandCredsMap(creds)
    username, password := c["username"], c["password"]
    if username == "" { return nil, fmt.Errorf("hover: missing creds.username (see docs/providers/hover.md)") }
    if password == "" { return nil, fmt.Errorf("hover: missing creds.password (see docs/providers/hover.md)") }
    client, err := hoverclient.NewClient(hoverclient.Config{
        Username: username, Password: password, TOTPSecret: c["totp_secret"],
    })
    if err != nil { return nil, fmt.Errorf("hover: client init: %w (creds redacted)", err) }
    return &hoverAdapter{client: client}, nil
}

// GetTXT/UpsertTXT/UpsertRecord/DeleteRecord delegate to hoverclient.
// Hover has no SetRecords primitive — UpsertTXT emulates RRset replace:
// list, delete TXT@relName, then create each value.
func (a *hoverAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    recs, err := a.client.ListRecords(ctx, zone)
    if err != nil { return nil, fmt.Errorf("hover: list records: %w (creds redacted)", err) }
    var out []string
    for _, r := range recs {
        if r.Type == "TXT" && r.Name == relName { out = append(out, r.Content) }
    }
    return out, nil
}

func (a *hoverAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    existing, err := a.client.ListRecords(ctx, zone)
    if err != nil { return fmt.Errorf("hover: list records: %w (creds redacted)", err) }
    for _, r := range existing {
        if r.Type == "TXT" && r.Name == relName {
            if err := a.client.DeleteRecord(ctx, zone, r.ID); err != nil {
                return fmt.Errorf("hover: delete stale TXT: %w (creds redacted)", err)
            }
        }
    }
    for _, v := range values {
        if _, err := a.client.CreateRecord(ctx, zone, hoverclient.Record{
            Type: "TXT", Name: relName, Content: v, TTL: ttl,
        }); err != nil {
            return fmt.Errorf("hover: create TXT: %w (creds redacted)", err)
        }
    }
    return nil
}

func (a *hoverAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
    if priority < 0 { return "", fmt.Errorf("hover: priority must be >= 0, got %d", priority) }
    rec, err := a.client.CreateRecord(ctx, zone, hoverclient.Record{
        Type: recordType, Name: name, Content: data, TTL: int(ttl), Priority: int(priority),
    })
    if err != nil { return "", fmt.Errorf("hover: upsert record: %w (creds redacted)", err) }
    return rec.ID, nil
}

func (a *hoverAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
    existing, err := a.client.ListRecords(ctx, zone)
    if err != nil { return fmt.Errorf("hover: list records: %w (creds redacted)", err) }
    for _, r := range existing {
        if r.Type == recordType && r.Name == name {
            if err := a.client.DeleteRecord(ctx, zone, r.ID); err != nil {
                return fmt.Errorf("hover: delete record: %w (creds redacted)", err)
            }
        }
    }
    return nil
}
```

Implementer adapts field/method names to match Step 0 godoc output.

**Step 5: Add docs/providers/hover.md**

```markdown
# Hover

Provider key: `hover`

## Cred keys

| key | required | description |
|---|---|---|
| `username` | yes | Hover account username |
| `password` | yes | Hover account password |
| `totp_secret` | optional | TOTP shared secret for 2FA accounts |

## YAML example

```yaml
provider: hover
provider_creds:
  username: $HOVER_USERNAME
  password: $HOVER_PASSWORD
  totp_secret: $HOVER_TOTP_SECRET  # optional
```

## Notes

- Hover has no public API. Implementation uses HTML scraping via `pkg/hoverclient` (extracted from workflow-plugin-hover#25).
- Scraping is fragile to Hover UI changes; report breakage at workflow-plugin-hover repo.
- Per-record CRUD only (no batch RRset semantics — UpsertTXT emulates RRset replace via list + delete + create).
```

**Step 6: Run tests + build + vet**

Same as Task 1 Step 10.

**Verification:** Plugin change class — agreed exception.

**Rollback note:** Revert commit. `go mod tidy`. Window: zero pipelines pinning `provider: hover`. Scraping fragility: if Hover UI breaks the client, deprecate provider per stability policy until upstream `pkg/hoverclient` ships fix.

**Step 7: Commit**

```bash
git checkout -b feat/dns-provider-v2-hover master
git add internal/dnsprovider/hover.go internal/dnsprovider/hover_test.go \
        docs/providers/hover.md go.mod go.sum
git commit -m "feat(dnsprovider): add Hover adapter (post hover#25 pkg/hoverclient)"
```

---

## Post-merge dep audit (operator note, not gated task)

After all 6 PRs merge:
```
GOWORK=off go list -m all | wc -l
```
If go.sum delta > 300 lines vs pre-v2 baseline (~150 modules), file followup issue for build-tag per-provider isolation.
