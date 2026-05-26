# DNS provider v2 Implementation Plan

> **For the implementing agent:** REQUIRED SUB-SKILL: Use autodev:executing-plans to implement this plan task-by-task.

**Goal:** Extend `internal/dnsprovider/NewAdapter` from 2 → 8 providers (Route53, GCP Cloud DNS, Azure DNS, Namecheap, GoDaddy, Hover) with per-provider unit tests + cred-key docs.

**Architecture:** Per-provider adapter file under `internal/dnsprovider/`. Each implements `dnspolicy.Adapter` (= `DNSPolicyReader + DNSRecordWriter`). 5 providers wrap libdns packages directly; Hover wraps `pkg/hoverclient` (custom HTTP client, extracted via workflow-plugin-hover#25). One file per provider; one switch case per provider; one docs file per provider — no merge contention across PRs.

**Tech Stack:** Go 1.23, `github.com/libdns/{route53 v1.6.2, googleclouddns v1.2.0, azure v0.5.0, namecheap v1.0.0, godaddy v1.1.0}`, `github.com/libdns/libdns v1.1.1`, `github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient` (post-extraction).

**Base branch:** master

---

## Scope Manifest

**PR Count:** 6
**Tasks:** 6
**Estimated Lines of Change:** ~1800 (informational)

**Out of scope:**
- Live cloud integration tests (deferred to v3)
- AWS assume-role chain (`assume_role_arn`) — v3
- GCP inline JSON cred form — v3
- Provider aliases (`aws`/`gcp`/`azure` shorthand) — v3
- Cloudflare migration to multi-cred — v1 single-token preserved
- gocodealone-dns mirror extension — separate work
- workflow#779 cross-driver ownership tagging beyond DNS — separate work

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(dnsprovider): Route53 adapter + docs index | Task 1 | feat/dns-provider-v2-route53 |
| 2 | feat(dnsprovider): GCP Cloud DNS adapter | Task 2 | feat/dns-provider-v2-gcp |
| 3 | feat(dnsprovider): Azure DNS adapter | Task 3 | feat/dns-provider-v2-azure |
| 4 | feat(dnsprovider): Namecheap adapter | Task 4 | feat/dns-provider-v2-namecheap |
| 5 | feat(dnsprovider): GoDaddy adapter | Task 5 | feat/dns-provider-v2-godaddy |
| 6 | feat(dnsprovider): Hover adapter | Task 6 | feat/dns-provider-v2-hover |

**Status:** Draft

---

## Global Design Guidance

Source: `/Users/jon/workspace/docs/design-guidance.md`

| guidance | plan response |
|---|---|
| Go stdlib-first | All adapters Go; only new deps are libdns adapters + pkg/hoverclient |
| Dogfood workflow ecosystem | All work within existing `internal/dnsprovider/` switch; no new binaries |
| Reuse over rebuild | Hover via `pkg/hoverclient` (extract issue filed) |
| libdns isolated in `internal/<provider>/` | Per v1 precedent: single file under `internal/dnsprovider/<provider>.go` |
| Secrets never logged | Each adapter wraps upstream errors with `(creds redacted)` suffix; missing-cred errors name only the key |
| Cross-driver parity | All 6 implement `dnspolicy.Adapter` interface exactly |
| No mock-first | Per user choice: stub libdns providers for v2 (3rd-party API = legitimate stub boundary per guidance) |
| Cost discipline | No live cloud calls in CI |
| Plugin minEngineVersion | Unchanged (no engine ABI change) |

---

## Shared implementation patterns (read once; tasks reference)

**Adapter shape (canonical: `internal/dnsprovider/digitalocean.go` + `cloudflare.go`):**

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

type <prov>Adapter struct {
    provider *libdns<prov>.Provider
}

func new<Prov>Adapter(creds map[string]string) (dnspolicy.Adapter, error) {
    c := ExpandCredsMap(creds)
    // validate required cred keys; per missing → "<prov>: missing creds.<key>"
    return &<prov>Adapter{provider: &libdns<prov>.Provider{...}}, nil
}

func (a *<prov>Adapter) GetTXT(ctx context.Context, name string) ([]string, error) {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    recs, err := a.provider.GetRecords(ctx, zone)
    if err != nil { return nil, fmt.Errorf("<prov>: get records: %w (creds redacted)", err) }
    var out []string
    for _, r := range recs {
        rr := r.RR()
        if rr.Type == "TXT" && rr.Name == relName { out = append(out, rr.Data) }
    }
    return out, nil
}

func (a *<prov>Adapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
    }
    _, err := a.provider.SetRecords(ctx, zone, recs)
    if err != nil { return fmt.Errorf("<prov>: upsert TXT: %w (creds redacted)", err) }
    return nil
}

func (a *<prov>Adapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}}
    res, err := a.provider.SetRecords(ctx, zone, recs)
    if err != nil { return "", fmt.Errorf("<prov>: upsert record: %w (creds redacted)", err) }
    if len(res) > 0 { return res[0].RR().Name, nil }
    return "", nil
}

func (a *<prov>Adapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name}}
    _, err := a.provider.DeleteRecords(ctx, zone, recs)
    if err != nil { return fmt.Errorf("<prov>: delete record: %w (creds redacted)", err) }
    return nil
}
```

**Test pattern (canonical: `digitalocean_test.go` lines 1-77):**

```go
// per-provider iface that the libdns Provider satisfies
type <prov>ProviderIface interface {
    GetRecords(ctx context.Context, zone string) ([]libdns.Record, error)
    SetRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error)
    DeleteRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error)
    AppendRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error)
}

type stub<Prov>Provider struct {
    existing []libdns.Record
    calls    map[string]int
}
// implement iface methods; record calls
```

**Switch-case insertion (canonical: `internal/dnsprovider/adapter.go:18-26`):**

```go
case "<provider-key>":
    return new<Prov>Adapter(creds)
```

Update the `ErrUnknownProvider` default-branch supported-list string in same edit.

---

### Task 1: Route53 adapter + docs index

**Files:**
- Create: `internal/dnsprovider/route53.go`
- Create: `internal/dnsprovider/route53_test.go`
- Create: `docs/providers/README.md` (skeleton — first PR only)
- Create: `docs/providers/route53.md`
- Modify: `internal/dnsprovider/adapter.go` (add case + update supported list)
- Modify: `internal/dnsprovider/adapter_test.go` (add Route53 case to TestNewAdapter_CaseFold-equivalent)
- Modify: `go.mod`, `go.sum` (add `github.com/libdns/route53 v1.6.2`)

**Cred mapping (verified 2026-05-26 via `go doc github.com/libdns/route53.Provider`):**

| YAML cred key | upstream field | required? |
|---|---|---|
| `region` | `Region` (`region`) | yes |
| `access_key_id` | `AccessKeyId` (`access_key_id`) | yes unless ambient |
| `secret_access_key` | `SecretAccessKey` (`secret_access_key`) | yes unless ambient |
| `session_token` | `SessionToken` (`session_token`) | optional |
| `profile` | `Profile` (`profile`) | optional (alternative to access_key) |

**Step 1: Write failing tests (route53_test.go)**

```go
package dnsprovider

import (
    "context"
    "errors"
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
    // region present, no keys → adapter constructs (libdns uses ambient/env/IAM)
    a, err := newRoute53Adapter(map[string]string{"region": "us-east-1"})
    if err != nil { t.Fatalf("ambient mode rejected: %v", err) }
    if a == nil { t.Fatal("nil adapter") }
}

func TestNewRoute53Adapter_MapsFieldsExact(t *testing.T) {
    a, err := newRoute53Adapter(map[string]string{
        "region":            "us-east-1",
        "access_key_id":     "AKIA",
        "secret_access_key": "secret",
        "session_token":     "tok",
        "profile":           "p",
    })
    if err != nil { t.Fatalf("construct: %v", err) }
    r := a.(*route53Adapter)
    if r.provider.Region != "us-east-1" { t.Errorf("Region: %q", r.provider.Region) }
    if r.provider.AccessKeyId != "AKIA" { t.Errorf("AccessKeyId: %q", r.provider.AccessKeyId) }
    if r.provider.SecretAccessKey != "secret" { t.Errorf("SecretAccessKey: %q", r.provider.SecretAccessKey) }
    if r.provider.SessionToken != "tok" { t.Errorf("SessionToken: %q", r.provider.SessionToken) }
    if r.provider.Profile != "p" { t.Errorf("Profile: %q", r.provider.Profile) }
}

func TestNewAdapter_Route53Dispatch(t *testing.T) {
    a, err := NewAdapter("route53", map[string]string{"region": "us-east-1"})
    if err != nil || a == nil { t.Fatalf("dispatch route53: %v / nil=%v", err, a == nil) }
    a2, err := NewAdapter("Route53", map[string]string{"region": "us-east-1"})
    if err != nil || a2 == nil { t.Fatalf("case-fold Route53: %v / nil=%v", err, a2 == nil) }
}

// Round-trip test: stub libdns Provider, verify GetTXT/UpsertTXT compose.
type stubR53Provider struct {
    existing []libdns.Record
    setCalls [][]libdns.Record
    delCalls [][]libdns.Record
}

func (s *stubR53Provider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) {
    return s.existing, nil
}
func (s *stubR53Provider) SetRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
    s.setCalls = append(s.setCalls, r); return r, nil
}
func (s *stubR53Provider) DeleteRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
    s.delCalls = append(s.delCalls, r); return r, nil
}
func (s *stubR53Provider) AppendRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
    return r, nil
}

type r53ProviderIface interface {
    GetRecords(context.Context, string) ([]libdns.Record, error)
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}

// upsertTXTViaProvider mirrors route53Adapter.UpsertTXT for stub injection.
func upsertTXTViaProvider(ctx context.Context, p r53ProviderIface, zone, relName string, values []string, ttl int) error {
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v}
        _ = recs[i] // ttl irrelevant for stub
    }
    _, err := p.SetRecords(ctx, zone, recs)
    return err
}

func TestRoute53UpsertTXT_CallsSetRecordsOnce(t *testing.T) {
    stub := &stubR53Provider{}
    if err := upsertTXTViaProvider(context.Background(), stub, "example.com", "_workflow-dns-policy", []string{"v=wfinfra-v1 o=sre"}, 300); err != nil {
        t.Fatalf("upsert: %v", err)
    }
    if len(stub.setCalls) != 1 { t.Errorf("SetRecords calls: %d", len(stub.setCalls)) }
    if len(stub.setCalls[0]) != 1 { t.Errorf("record count: %d", len(stub.setCalls[0])) }
}

// Sanity: libdns Provider type used by adapter must satisfy r53ProviderIface.
var _ r53ProviderIface = (*libdnsr53.Provider)(nil)

// Unused-import suppressor.
var _ = errors.New
```

**Step 2: Run tests to verify fail**

Run: `cd /Users/jon/workspace/_worktrees/wf-infra-dns-provider-v2-route53 && GOWORK=off go test ./internal/dnsprovider/...`
Expected: FAIL (undefined: newRoute53Adapter, route53Adapter)

**Step 3: Add libdns/route53 dep**

Run: `GOWORK=off go get github.com/libdns/route53@v1.6.2`
Expected: go.sum updated; no compile errors yet.

**Step 4: Implement route53.go**

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

func (a *route53Adapter) GetTXT(ctx context.Context, name string) ([]string, error) {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    recs, err := a.provider.GetRecords(ctx, zone)
    if err != nil {
        return nil, fmt.Errorf("route53: get records: %w (creds redacted)", err)
    }
    var out []string
    for _, r := range recs {
        rr := r.RR()
        if rr.Type == "TXT" && rr.Name == relName {
            out = append(out, rr.Data)
        }
    }
    return out, nil
}

func (a *route53Adapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
    zone := zoneFromPolicyName(name)
    relName := relativeNameFromFQDN(name, zone)
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
    }
    if _, err := a.provider.SetRecords(ctx, zone, recs); err != nil {
        return fmt.Errorf("route53: upsert TXT: %w (creds redacted)", err)
    }
    return nil
}

func (a *route53Adapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
    if priority < 0 {
        return "", fmt.Errorf("route53: priority must be >= 0, got %d", priority)
    }
    recs := []libdns.Record{libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}}
    res, err := a.provider.SetRecords(ctx, zone, recs)
    if err != nil {
        return "", fmt.Errorf("route53: upsert record: %w (creds redacted)", err)
    }
    if len(res) > 0 {
        return res[0].RR().Name, nil
    }
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

**Step 5: Wire switch case in adapter.go**

Edit `internal/dnsprovider/adapter.go`:

```go
case "cloudflare":
    return newCloudflareAdapter(creds)
case "route53":
    return newRoute53Adapter(creds)
default:
    return nil, fmt.Errorf("%w %q (supported: digitalocean, cloudflare, route53)", ErrUnknownProvider, provider)
```

**Step 6: Add docs/providers/README.md skeleton**

```markdown
# DNS provider credentials

Per-provider cred-key documentation for `dnsprovider.NewAdapter`. Each adapter accepts a `map[string]string` of credentials. Values support `os.ExpandEnv` ($VAR / ${VAR}) — unset env vars expand to empty string.

## Stability note

Adding a provider is a feature (new switch case). Removing a provider is a breaking change: removed key emits a deprecation warning log on `NewAdapter` for 1 minor version, then errors. Plugin CHANGELOG documents removal.

## Supported providers

- [DigitalOcean](digitalocean.md) — v1
- [Cloudflare](cloudflare.md) — v1
- [Route53 / AWS](route53.md) — v2
- [GCP Cloud DNS](googleclouddns.md) — v2
- [Azure DNS](azuredns.md) — v2
- [Namecheap](namecheap.md) — v2
- [GoDaddy](godaddy.md) — v2
- [Hover](hover.md) — v2
```

(v1 docs files `digitalocean.md` and `cloudflare.md` not authored in this plan — out of scope.)

**Step 7: Add docs/providers/route53.md**

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

**Step 8: Run tests + build**

Run: `GOWORK=off go test ./internal/dnsprovider/... && GOWORK=off go build ./...`
Expected: tests PASS; build exit 0.

**Verification (Plugin change class):**

Run plugin compile + go vet:
```
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/wfinfra ./cmd/workflow-plugin-infra
ls -l /tmp/wfinfra && rm /tmp/wfinfra
```
Expected: vet clean; binary built (>0 bytes); removed.

**Rollback note:** Rollback = revert PR commit. `NewAdapter("route53", ...)` returns `ErrUnknownProvider`. Rollback window: only safe while zero YAML pipelines pin `provider: route53`. Per `docs/providers/README.md` stability note, post-adoption removal needs deprecation cycle.

**Step 9: Commit**

```bash
git add internal/dnsprovider/route53.go internal/dnsprovider/route53_test.go \
        internal/dnsprovider/adapter.go internal/dnsprovider/adapter_test.go \
        docs/providers/README.md docs/providers/route53.md \
        go.mod go.sum
git commit -m "feat(dnsprovider): Route53 adapter + docs index"
```

---

### Task 2: GCP Cloud DNS adapter

**Files:**
- Create: `internal/dnsprovider/googleclouddns.go`
- Create: `internal/dnsprovider/googleclouddns_test.go`
- Create: `docs/providers/googleclouddns.md`
- Modify: `internal/dnsprovider/adapter.go` (add case + update supported list)
- Modify: `go.mod`, `go.sum` (add `github.com/libdns/googleclouddns v1.2.0`)

**Cred mapping (verified):**

| YAML cred key | upstream field | required? |
|---|---|---|
| `gcp_project` | `Project` (`gcp_project`) | yes |
| `service_account_path` | `ServiceAccountJSON` (`gcp_application_default`) | optional (omit → ADC) |

**Step 1: Write failing tests (googleclouddns_test.go)**

```go
package dnsprovider

import (
    "strings"
    "testing"
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
        "gcp_project":          "proj-x",
        "service_account_path": "/etc/secrets/sa.json",
    })
    if err != nil { t.Fatalf("construct: %v", err) }
    g := a.(*gcpAdapter)
    if g.provider.Project != "proj-x" { t.Errorf("Project: %q", g.provider.Project) }
    if g.provider.ServiceAccountJSON != "/etc/secrets/sa.json" { t.Errorf("ServiceAccountJSON: %q", g.provider.ServiceAccountJSON) }
}

func TestNewAdapter_GCPDispatch(t *testing.T) {
    a, err := NewAdapter("googleclouddns", map[string]string{"gcp_project": "p"})
    if err != nil || a == nil { t.Fatalf("dispatch gcp: %v / nil=%v", err, a == nil) }
}
```

**Step 2: Run tests to verify fail**

Run: `GOWORK=off go test ./internal/dnsprovider/...`
Expected: FAIL (undefined: newGoogleCloudDNSAdapter, gcpAdapter)

**Step 3: Add dep**

Run: `GOWORK=off go get github.com/libdns/googleclouddns@v1.2.0`

**Step 4: Implement googleclouddns.go**

Mirror Route53 shape; cred constructor maps `gcp_project` + optional `service_account_path`. Switch case `"googleclouddns"`. Same pattern for GetTXT/UpsertTXT/UpsertRecord/DeleteRecord (all delegate to libdns).

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
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
    }
    if _, err := a.provider.SetRecords(ctx, zone, recs); err != nil {
        return fmt.Errorf("googleclouddns: upsert TXT: %w (creds redacted)", err)
    }
    return nil
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

**Step 5: Wire switch case**

`internal/dnsprovider/adapter.go`:

```go
case "route53":
    return newRoute53Adapter(creds)
case "googleclouddns":
    return newGoogleCloudDNSAdapter(creds)
default:
    return nil, fmt.Errorf("%w %q (supported: digitalocean, cloudflare, route53, googleclouddns)", ErrUnknownProvider, provider)
```

**Step 6: Add docs/providers/googleclouddns.md**

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

ADC mode:
```yaml
provider: googleclouddns
provider_creds:
  gcp_project: my-gcp-project
  # service_account_path omitted → ADC (GKE workload identity, GCE metadata server, or GOOGLE_APPLICATION_CREDENTIALS env)
```

## Notes

- Inline JSON cred form deferred to v3 (`service_account_path` accepts a file path only).
- IAM role: `roles/dns.admin` for the target managed zone (or narrower if granular RRset perms desired).
```

**Step 7: Run tests + build**

Run: `GOWORK=off go test ./internal/dnsprovider/... && GOWORK=off go vet ./... && GOWORK=off go build ./...`
Expected: PASS; vet clean; build exit 0.

**Verification (Plugin change class):** same as Task 1.

**Rollback note:** Revert commit. `NewAdapter("googleclouddns", ...)` returns `ErrUnknownProvider`. Window: zero pipelines pinning `provider: googleclouddns`.

**Step 8: Commit**

```bash
git add internal/dnsprovider/googleclouddns.go internal/dnsprovider/googleclouddns_test.go \
        internal/dnsprovider/adapter.go docs/providers/googleclouddns.md \
        go.mod go.sum
git commit -m "feat(dnsprovider): GCP Cloud DNS adapter"
```

---

### Task 3: Azure DNS adapter

**Files:**
- Create: `internal/dnsprovider/azure.go`
- Create: `internal/dnsprovider/azure_test.go`
- Create: `docs/providers/azuredns.md`
- Modify: `internal/dnsprovider/adapter.go`
- Modify: `go.mod`, `go.sum` (add `github.com/libdns/azure v0.5.0`)

**Cred mapping (verified — godoc confirms all-3-empty → managed identity):**

| YAML cred key | upstream field | required? |
|---|---|---|
| `subscription_id` | `SubscriptionId` (`subscription_id`) | yes |
| `resource_group_name` | `ResourceGroupName` (`resource_group_name`) | yes |
| `tenant_id` | `TenantId` (`tenant_id`) | service-principal mode (all-3 set) |
| `client_id` | `ClientId` (`client_id`) | service-principal mode |
| `client_secret` | `ClientSecret` (`client_secret`) | service-principal mode |

**Auth mode rule (enforced in constructor):**

- All 3 of `tenant_id` + `client_id` + `client_secret` set → service-principal auth.
- All 3 empty → managed-identity auth (ambient).
- 1 or 2 set → reject with "azuredns: tenant_id/client_id/client_secret must all be set (service-principal) or all be empty (managed-identity); got <count> set" naming the missing key(s).

**Step 1: Write failing tests (azure_test.go)**

```go
package dnsprovider

import (
    "strings"
    "testing"
)

func TestNewAzureAdapter_RequiresSubscriptionAndRG(t *testing.T) {
    _, err := newAzureAdapter(map[string]string{"resource_group_name": "rg"})
    if err == nil || !strings.Contains(err.Error(), "creds.subscription_id") {
        t.Errorf("want missing-subscription_id error, got %v", err)
    }
    _, err = newAzureAdapter(map[string]string{"subscription_id": "sub"})
    if err == nil || !strings.Contains(err.Error(), "creds.resource_group_name") {
        t.Errorf("want missing-resource_group_name error, got %v", err)
    }
}

func TestNewAzureAdapter_ManagedIdentityMode(t *testing.T) {
    a, err := newAzureAdapter(map[string]string{
        "subscription_id":     "sub",
        "resource_group_name": "rg",
    })
    if err != nil { t.Fatalf("MI mode rejected: %v", err) }
    if a == nil { t.Fatal("nil adapter") }
}

func TestNewAzureAdapter_ServicePrincipalMode(t *testing.T) {
    a, err := newAzureAdapter(map[string]string{
        "subscription_id":     "sub",
        "resource_group_name": "rg",
        "tenant_id":           "t",
        "client_id":           "c",
        "client_secret":       "s",
    })
    if err != nil { t.Fatalf("SP mode: %v", err) }
    if a == nil { t.Fatal("nil adapter") }
    az := a.(*azureAdapter)
    if az.provider.TenantId != "t" || az.provider.ClientId != "c" || az.provider.ClientSecret != "s" {
        t.Errorf("SP fields: t=%q c=%q s=%q", az.provider.TenantId, az.provider.ClientId, az.provider.ClientSecret)
    }
}

func TestNewAzureAdapter_PartialSPRejected(t *testing.T) {
    _, err := newAzureAdapter(map[string]string{
        "subscription_id":     "sub",
        "resource_group_name": "rg",
        "tenant_id":           "t",
        "client_id":           "c",
        // client_secret missing
    })
    if err == nil || !strings.Contains(err.Error(), "client_secret") {
        t.Errorf("want partial-SP rejection, got %v", err)
    }
}

func TestNewAdapter_AzureDispatch(t *testing.T) {
    a, err := NewAdapter("azuredns", map[string]string{"subscription_id": "s", "resource_group_name": "rg"})
    if err != nil || a == nil { t.Fatalf("dispatch azure: %v / nil=%v", err, a == nil) }
}
```

**Step 2: Run tests to verify fail**

Run: `GOWORK=off go test ./internal/dnsprovider/...`
Expected: FAIL (undefined: newAzureAdapter, azureAdapter)

**Step 3: Add dep**

Run: `GOWORK=off go get github.com/libdns/azure@v0.5.0`

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
    spFields := []string{tenant, client, secret}
    setCount := 0
    for _, v := range spFields {
        if v != "" { setCount++ }
    }
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
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
    }
    if _, err := a.provider.SetRecords(ctx, zone, recs); err != nil {
        return fmt.Errorf("azuredns: upsert TXT: %w (creds redacted)", err)
    }
    return nil
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

**Step 5: Wire switch case**

```go
case "googleclouddns":
    return newGoogleCloudDNSAdapter(creds)
case "azuredns":
    return newAzureAdapter(creds)
default:
    return nil, fmt.Errorf("%w %q (supported: digitalocean, cloudflare, route53, googleclouddns, azuredns)", ErrUnknownProvider, provider)
```

**Step 6: Add docs/providers/azuredns.md**

```markdown
# Azure DNS

Provider key: `azuredns`

## Cred keys

| key | required | description |
|---|---|---|
| `subscription_id` | yes | Azure subscription ID |
| `resource_group_name` | yes | Resource group containing the DNS zone |
| `tenant_id` | service-principal | Entra ID tenant — required for service-principal auth |
| `client_id` | service-principal | App registration client ID |
| `client_secret` | service-principal | App registration client secret |

## Auth modes

- **Service principal**: ALL of `tenant_id` + `client_id` + `client_secret` set.
- **Managed identity**: ALL three empty (uses ambient Azure-managed identity, e.g. AKS workload identity).
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

**Step 7: Run tests + build**

Run: `GOWORK=off go test ./internal/dnsprovider/... && GOWORK=off go vet ./... && GOWORK=off go build ./...`
Expected: PASS; vet clean; build exit 0.

**Verification:** Plugin change class. Same as Task 1.

**Rollback note:** Revert commit. Window: zero pipelines pinning `provider: azuredns`.

**Step 8: Commit**

```bash
git add internal/dnsprovider/azure.go internal/dnsprovider/azure_test.go \
        internal/dnsprovider/adapter.go docs/providers/azuredns.md \
        go.mod go.sum
git commit -m "feat(dnsprovider): Azure DNS adapter"
```

---

### Task 4: Namecheap adapter

**Files:**
- Create: `internal/dnsprovider/namecheap.go`
- Create: `internal/dnsprovider/namecheap_test.go`
- Create: `docs/providers/namecheap.md`
- Modify: `internal/dnsprovider/adapter.go`
- Modify: `go.mod`, `go.sum` (add `github.com/libdns/namecheap v1.0.0`)

**Cred mapping (verified):**

| YAML cred key | upstream field | required? |
|---|---|---|
| `api_key` | `APIKey` (`api_key`) | yes |
| `user` | `User` (`user`) | yes |
| `client_ip` | `ClientIP` (`client_ip`) | yes (strict — no discovery fallback for CI on private subnets) |
| `api_endpoint` | `APIEndpoint` (`api_endpoint`) | optional |

**Step 1: Write failing tests (namecheap_test.go)**

```go
package dnsprovider

import (
    "context"
    "strings"
    "testing"

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
        "api_key":      "k",
        "user":         "u",
        "client_ip":    "1.2.3.4",
        "api_endpoint": "https://api.sandbox.namecheap.com/xml.response",
    })
    if err != nil { t.Fatalf("construct: %v", err) }
    n := a.(*namecheapAdapter)
    if n.provider.APIKey != "k" { t.Errorf("APIKey: %q", n.provider.APIKey) }
    if n.provider.User != "u" { t.Errorf("User: %q", n.provider.User) }
    if n.provider.ClientIP != "1.2.3.4" { t.Errorf("ClientIP: %q", n.provider.ClientIP) }
    if n.provider.APIEndpoint != "https://api.sandbox.namecheap.com/xml.response" {
        t.Errorf("APIEndpoint: %q", n.provider.APIEndpoint)
    }
}

func TestNewAdapter_NamecheapDispatch(t *testing.T) {
    a, err := NewAdapter("namecheap", map[string]string{"api_key": "k", "user": "u", "client_ip": "1.2.3.4"})
    if err != nil || a == nil { t.Fatalf("dispatch namecheap: %v / nil=%v", err, a == nil) }
}

// Lock the foreign-record-survival contract: upstream libdns/namecheap.SetRecords
// does Get-merge-Set per (name,type) internally (verified via source spike 2026-05-26).
// Adapter delegates directly. This test exercises the libdns boundary semantics
// using the same stub-iface pattern as digitalocean_test.go.
type stubNCProvider struct {
    existing []libdns.Record
    setCalls [][]libdns.Record
}
func (s *stubNCProvider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) {
    return s.existing, nil
}
func (s *stubNCProvider) SetRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
    s.setCalls = append(s.setCalls, r); return r, nil
}
func (s *stubNCProvider) AppendRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) { return r, nil }
func (s *stubNCProvider) DeleteRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) { return r, nil }

type ncProviderIface interface {
    GetRecords(context.Context, string) ([]libdns.Record, error)
    SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
    DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}

// Locks the contract that the adapter delegates exactly one SetRecords call
// per UpsertTXT, passing only the desired values. The upstream Get-merge-Set
// algorithm is libdns/namecheap's responsibility (already source-verified).
func TestNamecheapUpsertTXT_DelegatesSetRecords(t *testing.T) {
    stub := &stubNCProvider{}
    recs := []libdns.Record{libdns.RR{Type: "TXT", Name: "_workflow-dns-policy", Data: "v=wfinfra-v1 o=sre"}}
    _, err := stub.SetRecords(context.Background(), "example.com", recs)
    if err != nil { t.Fatalf("set: %v", err) }
    if len(stub.setCalls) != 1 { t.Errorf("SetRecords call count: %d", len(stub.setCalls)) }
}
```

**Step 2: Run tests to verify fail**

Run: `GOWORK=off go test ./internal/dnsprovider/...`
Expected: FAIL (undefined: newNamecheapAdapter)

**Step 3: Add dep**

Run: `GOWORK=off go get github.com/libdns/namecheap@v1.0.0`

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
        APIKey:      apiKey,
        User:        user,
        ClientIP:    clientIP,
        APIEndpoint: c["api_endpoint"],
    }}, nil
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
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
    }
    if _, err := a.provider.SetRecords(ctx, zone, recs); err != nil {
        return fmt.Errorf("namecheap: upsert TXT: %w (creds redacted)", err)
    }
    return nil
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

**Step 5: Wire switch case**

```go
case "azuredns":
    return newAzureAdapter(creds)
case "namecheap":
    return newNamecheapAdapter(creds)
default:
    return nil, fmt.Errorf("%w %q (supported: digitalocean, cloudflare, route53, googleclouddns, azuredns, namecheap)", ErrUnknownProvider, provider)
```

**Step 6: Add docs/providers/namecheap.md**

```markdown
# Namecheap

Provider key: `namecheap`

## Cred keys

| key | required | description |
|---|---|---|
| `api_key` | yes | Namecheap API key (enable + whitelist IPs at namecheap.com → Profile → Tools → API Access) |
| `user` | yes | Namecheap API user (usually your account username) |
| `client_ip` | **yes (strict)** | Whitelisted public IP of the calling machine. NO discovery fallback (CI runners on private subnets can't rely on it) |
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
- Upstream `libdns/namecheap.SetRecords` safely replaces records per (name,type) (verified source spike) — foreign-(name,type) records are preserved.
```

**Step 7: Run tests + build**

Run: `GOWORK=off go test ./internal/dnsprovider/... && GOWORK=off go vet ./... && GOWORK=off go build ./...`
Expected: PASS; vet clean; build exit 0.

**Verification:** Plugin change class. Same as Task 1.

**Rollback note:** Revert commit. Window: zero pipelines pinning `provider: namecheap`.

**Step 8: Commit**

```bash
git add internal/dnsprovider/namecheap.go internal/dnsprovider/namecheap_test.go \
        internal/dnsprovider/adapter.go docs/providers/namecheap.md \
        go.mod go.sum
git commit -m "feat(dnsprovider): Namecheap adapter"
```

---

### Task 5: GoDaddy adapter

**Files:**
- Create: `internal/dnsprovider/godaddy.go`
- Create: `internal/dnsprovider/godaddy_test.go`
- Create: `docs/providers/godaddy.md`
- Modify: `internal/dnsprovider/adapter.go`
- Modify: `go.mod`, `go.sum` (add `github.com/libdns/godaddy v1.1.0`)

**Cred mapping (verified — single field `APIToken`):**

| YAML cred key | upstream field | required? |
|---|---|---|
| `api_token` | `APIToken` (`api_token`) — format `<sso-key>:<sso-secret>` | yes |

**Step 1: Write failing tests (godaddy_test.go)**

```go
package dnsprovider

import (
    "strings"
    "testing"
)

func TestNewGoDaddyAdapter_RequiresToken(t *testing.T) {
    _, err := newGoDaddyAdapter(map[string]string{})
    if err == nil || !strings.Contains(err.Error(), "creds.api_token") {
        t.Errorf("want missing-api_token error, got %v", err)
    }
}

func TestNewGoDaddyAdapter_RequiresColonFormat(t *testing.T) {
    _, err := newGoDaddyAdapter(map[string]string{"api_token": "bare-sso-key-no-colon"})
    if err == nil || !strings.Contains(err.Error(), "\"<sso-key>:<sso-secret>\"") {
        t.Errorf("want colon-format rejection, got %v", err)
    }
}

func TestNewGoDaddyAdapter_AcceptsConcatenatedToken(t *testing.T) {
    a, err := newGoDaddyAdapter(map[string]string{"api_token": "ssokey:ssosecret"})
    if err != nil { t.Fatalf("construct: %v", err) }
    g := a.(*godaddyAdapter)
    if g.provider.APIToken != "ssokey:ssosecret" {
        t.Errorf("APIToken: %q", g.provider.APIToken)
    }
}

func TestNewAdapter_GoDaddyDispatch(t *testing.T) {
    a, err := NewAdapter("godaddy", map[string]string{"api_token": "k:s"})
    if err != nil || a == nil { t.Fatalf("dispatch godaddy: %v / nil=%v", err, a == nil) }
}
```

**Step 2: Run tests to verify fail**

Run: `GOWORK=off go test ./internal/dnsprovider/...`
Expected: FAIL (undefined: newGoDaddyAdapter)

**Step 3: Add dep**

Run: `GOWORK=off go get github.com/libdns/godaddy@v1.1.0`

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
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
    }
    if _, err := a.provider.SetRecords(ctx, zone, recs); err != nil {
        return fmt.Errorf("godaddy: upsert TXT: %w (creds redacted)", err)
    }
    return nil
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

**Step 5: Wire switch case**

```go
case "namecheap":
    return newNamecheapAdapter(creds)
case "godaddy":
    return newGoDaddyAdapter(creds)
default:
    return nil, fmt.Errorf("%w %q (supported: digitalocean, cloudflare, route53, googleclouddns, azuredns, namecheap, godaddy)", ErrUnknownProvider, provider)
```

**Step 6: Add docs/providers/godaddy.md**

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

**Step 7: Run tests + build**

Run: `GOWORK=off go test ./internal/dnsprovider/... && GOWORK=off go vet ./... && GOWORK=off go build ./...`
Expected: PASS; vet clean; build exit 0.

**Verification:** Plugin change class. Same as Task 1.

**Rollback note:** Revert commit. Window: zero pipelines pinning `provider: godaddy`. If 50-domain restriction blocks all consumers, deprecate per `docs/providers/README.md` stability policy.

**Step 8: Commit**

```bash
git add internal/dnsprovider/godaddy.go internal/dnsprovider/godaddy_test.go \
        internal/dnsprovider/adapter.go docs/providers/godaddy.md \
        go.mod go.sum
git commit -m "feat(dnsprovider): GoDaddy adapter"
```

---

### Task 6: Hover adapter (blocked on workflow-plugin-hover#25)

**Prerequisite:** workflow-plugin-hover#25 ships `pkg/hoverclient` + tag. Pin tag in this task.

**Files:**
- Create: `internal/dnsprovider/hover.go`
- Create: `internal/dnsprovider/hover_test.go`
- Create: `docs/providers/hover.md`
- Modify: `internal/dnsprovider/adapter.go`
- Modify: `go.mod`, `go.sum` (add `github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient @<tag>`)

**Cred mapping (custom client; no upstream JSON tag):**

| YAML cred key | required? |
|---|---|
| `username` | yes |
| `password` | yes |
| `totp_secret` | optional |

**Pre-task reconciliation (gating):** Before opening PR 6, fetch merged hover#25 `pkg/hoverclient` public API. Verify the design's `username`/`password`/`totp_secret` map cleanly to the exported NewClient/Login signature. If not, revise this task's adapter constructor to match.

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
        t.Errorf("want missing-username error, got %v", err)
    }
}

func TestNewHoverAdapter_RequiresPassword(t *testing.T) {
    _, err := newHoverAdapter(map[string]string{"username": "u"})
    if err == nil || !strings.Contains(err.Error(), "creds.password") {
        t.Errorf("want missing-password error, got %v", err)
    }
}

func TestNewHoverAdapter_AcceptsOptionalTOTP(t *testing.T) {
    a, err := newHoverAdapter(map[string]string{
        "username":    "u",
        "password":    "p",
        "totp_secret": "ABCD1234",
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
Expected: FAIL (undefined: newHoverAdapter)

**Step 3: Add dep**

Run: `GOWORK=off go get github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient@<tag-from-hover#25>`
(Replace `<tag-from-hover#25>` with actual tag — e.g. `v0.3.0`.)

**Step 4: Implement hover.go**

Exact `pkg/hoverclient.NewClient` signature pinned at PR-6 open time. Skeleton based on design's cred-key shape:

```go
package dnsprovider

import (
    "context"
    "fmt"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
    "github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient"
)

type hoverAdapter struct {
    client *hoverclient.Client
}

func newHoverAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
    c := ExpandCredsMap(creds)
    username, password := c["username"], c["password"]
    if username == "" {
        return nil, fmt.Errorf("hover: missing creds.username (see docs/providers/hover.md)")
    }
    if password == "" {
        return nil, fmt.Errorf("hover: missing creds.password (see docs/providers/hover.md)")
    }
    client, err := hoverclient.NewClient(hoverclient.Config{
        Username:   username,
        Password:   password,
        TOTPSecret: c["totp_secret"],
    })
    if err != nil {
        return nil, fmt.Errorf("hover: client init: %w (creds redacted)", err)
    }
    return &hoverAdapter{client: client}, nil
}

// GetTXT/UpsertTXT/UpsertRecord/DeleteRecord delegate to hoverclient
// using pkg/hoverclient's exported DNS methods (exact names pinned at
// PR-6 open time per hover#25 final public surface).
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
    // Hover has no SetRecords primitive — emulate RRset replace: list, delete TXT@relName, then create each value.
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

Note: exact `hoverclient.Client` / `Config` / `Record` shape resolved at hover#25 merge time. Implementer adapts during PR 6 if API differs (low-risk; cred-key names are the load-bearing part).

**Step 5: Wire switch case**

```go
case "godaddy":
    return newGoDaddyAdapter(creds)
case "hover":
    return newHoverAdapter(creds)
default:
    return nil, fmt.Errorf("%w %q (supported: digitalocean, cloudflare, route53, googleclouddns, azuredns, namecheap, godaddy, hover)", ErrUnknownProvider, provider)
```

**Step 6: Add docs/providers/hover.md**

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

**Step 7: Run tests + build**

Run: `GOWORK=off go test ./internal/dnsprovider/... && GOWORK=off go vet ./... && GOWORK=off go build ./...`
Expected: PASS; vet clean; build exit 0.

**Verification:** Plugin change class. Same as Task 1.

**Rollback note:** Revert commit. Window: zero pipelines pinning `provider: hover`. Hover scraping fragility: if Hover UI changes break the client, deprecate provider per stability policy until upstream `pkg/hoverclient` ships fix.

**Step 8: Commit**

```bash
git add internal/dnsprovider/hover.go internal/dnsprovider/hover_test.go \
        internal/dnsprovider/adapter.go docs/providers/hover.md \
        go.mod go.sum
git commit -m "feat(dnsprovider): Hover adapter (post hover#25 pkg/hoverclient)"
```

---

## Post-merge dep audit (operator note, not a gated task)

After all 6 PRs merge, run:
```
GOWORK=off go list -m all | wc -l   # baseline vs new module count
```
If go.sum delta > 300 lines vs pre-v2 baseline, file followup issue for build-tag per-provider isolation.
