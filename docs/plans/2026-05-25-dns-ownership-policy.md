# DNS ownership policy gate Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship workflow-plugin-infra v0.2.0 with `_workflow-dns-policy` TXT-record-based per-record DNS ownership gate fired by a new `infra.dns_record` step, plus `wfctl infra-dns` admin CLI.

**Architecture:** Three internal packages compose the gate: `internal/dnspolicy/` (schema + parser + serializer + matcher + Policy.CheckAllowed), `internal/dnsgate/` (Gate function = thin orchestration), `internal/dnsprovider/` (libdns adapter for DO + Cloudflare, isolates libdns boundary). Step type `infra.dns_record` invokes Gate before mutating; `internal/admincli/` exports CLIProvider for `wfctl infra-dns` subcommands (set-policy, drift, transfer-ownership, policy show). Existing `infra.dns` MODULE deprecated to a non-nil migration error.

**Tech Stack:** Go, protobuf (3-message TypedStepFactory[C,I,O] pattern), `github.com/libdns/digitalocean` + `github.com/libdns/cloudflare`, `sdk.ServePluginFull` + `sdk.CLIProvider`, sentinel errors via `errors.Is`.

**Base branch:** `master` (workflow-plugin-infra default)

**Design doc:** `docs/plans/2026-05-25-dns-ownership-policy-design.md` (7 adversarial cycles PASSed)

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 10
**Estimated Lines of Change:** ~1400 (3 internal packages + admin CLI + proto additions + plugin wiring + migration doc + tests)

**Out of scope:**
- Route53, GCP, Azure, Namecheap, GoDaddy, Hover provider adapters (v2 — require multi-credential structured tokens)
- DNSSEC self-signing zones (v1 = managed-DNSSEC only)
- Generation-counter-based concurrent-write protection (v2)
- Provider WhoAmI / token-bound owner verification (v2)
- Cross-driver ownership-tagging convention beyond DNS (tracked at workflow#779)
- `infra.dns` module rewrite (deprecated to migration-error stub only)

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(infra): DNS ownership policy gate + infra.dns_record step + wfctl infra-dns CLI | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6, Task 7, Task 8, Task 9, Task 10 | feat/dns-ownership-policy |

**Status:** Draft

---

### Task 1: `internal/dnspolicy/` — schema parser + serializer + sentinel errors

**Change class:** Internal logic refactor (new package, pure functions).

**Files:**
- Create: `internal/dnspolicy/types.go`
- Create: `internal/dnspolicy/errors.go`
- Create: `internal/dnspolicy/parse.go`
- Create: `internal/dnspolicy/serialize.go`
- Create: `internal/dnspolicy/parse_test.go`

**Step 1: Write failing tests** (`parse_test.go`)

```go
package dnspolicy

import (
    "errors"
    "strings"
    "testing"
)

func TestParse_HappyPath(t *testing.T) {
    rrs := []string{
        `heritage=wfinfra-v1 o=sre d=true`,
        `heritage=wfinfra-v1 o=multisite p=www,admin,_acme-challenge.www`,
    }
    p, err := Parse("gocodealone.tech", rrs)
    if err != nil { t.Fatal(err) }
    if p.Zone != "gocodealone.tech" { t.Errorf("zone=%q", p.Zone) }
    if len(p.Entries) != 2 { t.Fatalf("entries=%d want 2", len(p.Entries)) }
}

func TestParse_IgnoresUnknownHeritage(t *testing.T) {
    rrs := []string{
        `heritage=wfinfra-v1 o=sre d=true`,
        `v=spf1 -all`,                          // SPF — ignored
        `heritage=wfinfra-v999 o=alien p=*`,    // future schema — ignored
    }
    p, err := Parse("z", rrs)
    if err != nil { t.Fatal(err) }
    if len(p.Entries) != 1 { t.Errorf("entries=%d want 1", len(p.Entries)) }
}

func TestParse_MultipleDefaults(t *testing.T) {
    rrs := []string{
        `heritage=wfinfra-v1 o=sre d=true`,
        `heritage=wfinfra-v1 o=multisite d=true p=www`,
    }
    _, err := Parse("z", rrs)
    if !errors.Is(err, ErrMultipleDefaults) {
        t.Errorf("want ErrMultipleDefaults, got %v", err)
    }
}

func TestParse_EmptyOwner(t *testing.T) {
    rrs := []string{`heritage=wfinfra-v1 o= p=www`}
    _, err := Parse("z", rrs)
    if !errors.Is(err, ErrEmptyOwner) {
        t.Errorf("want ErrEmptyOwner, got %v", err)
    }
}

func TestSerialize_DeterministicSort(t *testing.T) {
    p := &Policy{
        Zone: "z",
        Entries: []Entry{
            {Owner: "multisite", Patterns: []string{"www", "admin", "_acme-challenge.www"}},
            {Owner: "sre", Default: true},
        },
    }
    out1, err := Serialize(p)
    if err != nil { t.Fatal(err) }
    out2, _ := Serialize(p)
    if strings.Join(out1, "\n") != strings.Join(out2, "\n") {
        t.Errorf("serialize not deterministic")
    }
    // patterns within entry sorted alphabetically
    found := false
    for _, rr := range out1 {
        if strings.Contains(rr, "o=multisite") {
            if !strings.Contains(rr, "p=_acme-challenge.www,admin,www") {
                t.Errorf("patterns not sorted within entry: %s", rr)
            }
            found = true
        }
    }
    if !found { t.Errorf("multisite RR missing from %v", out1) }
}

func TestSerialize_MultipleDefaultsRejected(t *testing.T) {
    p := &Policy{Zone: "z", Entries: []Entry{{Owner: "a", Default: true}, {Owner: "b", Default: true}}}
    _, err := Serialize(p)
    if !errors.Is(err, ErrMultipleDefaults) {
        t.Errorf("Serialize should refuse multiple defaults, got %v", err)
    }
}

func TestParseSerialize_RoundTrip(t *testing.T) {
    rrs := []string{
        `heritage=wfinfra-v1 o=sre d=true`,
        `heritage=wfinfra-v1 o=multisite p=admin,www`,
    }
    p1, _ := Parse("z", rrs)
    out1, _ := Serialize(p1)
    p2, _ := Parse("z", out1)
    out2, _ := Serialize(p2)
    if strings.Join(out1, "\n") != strings.Join(out2, "\n") {
        t.Errorf("Parse(Serialize(p)) not idempotent\nout1=%v\nout2=%v", out1, out2)
    }
}
```

**Step 2: Run tests — verify FAIL**

```bash
cd /Users/jon/workspace/_worktrees/wf-infra-dns-ownership
GOWORK=off go test ./internal/dnspolicy/ -count=1
```
Expected: `FAIL: undefined: Parse, Serialize, Policy, Entry, ErrMultipleDefaults, ErrEmptyOwner`.

**Step 3: Implement**

`internal/dnspolicy/types.go`:
```go
package dnspolicy

type Policy struct {
    Zone    string
    Entries []Entry
}

type Entry struct {
    Owner    string
    Patterns []string
    Types    []string
    Default  bool
}
```

`internal/dnspolicy/errors.go`:
```go
package dnspolicy

import "errors"

var (
    ErrMultipleDefaults = errors.New("dnspolicy: multiple RRs set d=true")
    ErrEmptyOwner       = errors.New("dnspolicy: o= field is empty")
    ErrUnknownHeritage  = errors.New("dnspolicy: unknown heritage value (parser ignored RR)")
)

const HeritageV1 = "wfinfra-v1"
```

`internal/dnspolicy/parse.go`:
```go
package dnspolicy

import (
    "fmt"
    "strings"
)

// Parse parses TXT RR strings (one per RR) into a Policy.
// Unknown heritage values are silently skipped (forward-compat).
func Parse(zone string, txtRRs []string) (*Policy, error) {
    p := &Policy{Zone: zone}
    defaultCount := 0
    for _, rr := range txtRRs {
        fields := tokenize(rr)
        if fields["heritage"] != HeritageV1 {
            continue // foreign TXT (SPF, future schema, etc.)
        }
        owner := strings.TrimSpace(fields["o"])
        if owner == "" {
            return nil, fmt.Errorf("%w: rr=%q", ErrEmptyOwner, rr)
        }
        entry := Entry{
            Owner:    owner,
            Patterns: splitCSV(fields["p"]),
            Types:    splitCSV(fields["t"]),
            Default:  fields["d"] == "true",
        }
        if entry.Default {
            defaultCount++
            if defaultCount > 1 {
                return nil, fmt.Errorf("%w: rr=%q", ErrMultipleDefaults, rr)
            }
        }
        p.Entries = append(p.Entries, entry)
    }
    return p, nil
}

// tokenize splits "key=value key=value" into a map.
func tokenize(rr string) map[string]string {
    out := map[string]string{}
    for _, tok := range strings.Fields(rr) {
        eq := strings.IndexByte(tok, '=')
        if eq < 0 { continue }
        out[tok[:eq]] = tok[eq+1:]
    }
    return out
}

func splitCSV(s string) []string {
    if s == "" { return nil }
    parts := strings.Split(s, ",")
    for i, p := range parts { parts[i] = strings.TrimSpace(p) }
    return parts
}
```

`internal/dnspolicy/serialize.go`:
```go
package dnspolicy

import (
    "fmt"
    "sort"
    "strings"
)

// Serialize emits Policy as deterministically-ordered TXT RR strings.
// Refuses to emit if multiple entries have Default=true.
func Serialize(p *Policy) ([]string, error) {
    defaultCount := 0
    for _, e := range p.Entries {
        if e.Default { defaultCount++ }
    }
    if defaultCount > 1 {
        return nil, fmt.Errorf("%w (Policy has %d defaults; only 1 allowed)", ErrMultipleDefaults, defaultCount)
    }
    out := make([]string, 0, len(p.Entries))
    for _, e := range p.Entries {
        // Sort patterns + types within entry for deterministic hash
        pats := append([]string(nil), e.Patterns...)
        sort.Strings(pats)
        types := append([]string(nil), e.Types...)
        sort.Strings(types)

        sb := strings.Builder{}
        fmt.Fprintf(&sb, "heritage=%s o=%s", HeritageV1, e.Owner)
        if len(pats) > 0 { fmt.Fprintf(&sb, " p=%s", strings.Join(pats, ",")) }
        if len(types) > 0 { fmt.Fprintf(&sb, " t=%s", strings.Join(types, ",")) }
        if e.Default { sb.WriteString(" d=true") }
        out = append(out, sb.String())
    }
    sort.Strings(out) // RR-level sort for deterministic hashing
    return out, nil
}
```

**Step 4: Run tests — verify PASS**

Run: `GOWORK=off go test ./internal/dnspolicy/ -count=1 -v`
Expected: all 6 tests PASS.

**Step 5: Commit**

```bash
git add internal/dnspolicy/
git commit -m "feat(dnspolicy): TXT schema parser + serializer + sentinel errors

internal/dnspolicy package: Parse + Serialize + Policy + Entry types
+ ErrMultipleDefaults/ErrEmptyOwner/ErrUnknownHeritage sentinels.
6 tests covering parse happy path, foreign-RR skip, multiple-default
error (parse + serialize), empty-owner error, deterministic ordering,
round-trip idempotency."
git push -u origin feat/dns-ownership-policy
```

**Rollback:** revert commit. Package is brand-new; no consumers yet.

---

### Task 2: Pattern matcher (`*`, `**`, `@`)

**Change class:** Internal logic refactor (pure function).

**Files:**
- Create: `internal/dnspolicy/match.go`
- Create: `internal/dnspolicy/match_test.go`

**Step 1: Write failing tests**

```go
package dnspolicy

import "testing"

func TestMatchPattern(t *testing.T) {
    cases := []struct{ pattern, name string; want bool }{
        {"www", "www", true},
        {"www", "admin", false},
        {"@", "@", true},
        {"@", "www", false},
        {"*", "www", true},
        {"*", "anything", true},
        {"*", "", false},               // empty name → false (closes plan-cycle-1 m-1)
        {"*", "www.sub", false},        // * = single label only
        {"_acme-challenge.*", "_acme-challenge.www", true},
        {"_acme-challenge.*", "_acme-challenge.www.sub", false}, // * is single
        {"**", "anything.multi.label", true},                    // ** spans
        {"**", "single", true},
        {"a.**", "a.b.c", true},
        {"a.**", "b.c", false},
        {"tour.*", "tour.bandname", true},
        {"tour.*", "other.bandname", false},
    }
    for _, c := range cases {
        got := MatchPattern(c.pattern, c.name)
        if got != c.want {
            t.Errorf("MatchPattern(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
        }
    }
}
```

**Step 2: Run — FAIL** (undefined: MatchPattern).

**Step 3: Implement**

`internal/dnspolicy/match.go`:
```go
package dnspolicy

import "strings"

// MatchPattern returns true if name matches pattern.
// Pattern syntax:
//   "@"   matches the apex literal "@"
//   "*"   matches a SINGLE DNS label segment
//   "**"  matches one or more label segments
//   "<literal>.<rest>" matches recursively
// All matches are case-sensitive (DNS names are case-insensitive by spec
// but our pattern compare requires lowercase normalization at call sites).
func MatchPattern(pattern, name string) bool {
    if pattern == "@" { return name == "@" }
    if pattern == "**" { return true }
    if pattern == "*" {
        return !strings.Contains(name, ".") && name != ""
    }
    // Recursive: split on first dot
    pParts := strings.SplitN(pattern, ".", 2)
    nParts := strings.SplitN(name, ".", 2)
    head := pParts[0]
    // Head match: literal or single-* or **-spanning
    if head == "**" {
        // ** consumes anything from here
        return true
    }
    if head == "*" {
        if len(nParts) == 0 { return false }
        // * matches one label; require both have a tail OR both have no tail
        if len(pParts) == 1 { // pattern "*" alone (no dot) — handled above; safety
            return !strings.Contains(name, ".")
        }
        if len(nParts) == 1 { return false } // pattern has tail, name doesn't
        return MatchPattern(pParts[1], nParts[1])
    }
    // Literal head
    if len(nParts) == 0 || nParts[0] != head { return false }
    if len(pParts) == 1 { // pattern has no tail
        return len(nParts) == 1
    }
    if len(nParts) == 1 { return false } // pattern has tail, name doesn't
    return MatchPattern(pParts[1], nParts[1])
}
```

**Step 4: Run tests — PASS**

Run: `GOWORK=off go test ./internal/dnspolicy/ -run TestMatchPattern -count=1 -v`
Expected: all 14 sub-cases PASS.

**Step 5: Commit**

```bash
git add internal/dnspolicy/match.go internal/dnspolicy/match_test.go
git commit -m "feat(dnspolicy): MatchPattern with * (single label), ** (multi), @ (apex)"
git push
```

**Rollback:** revert commit.

---

### Task 3: `Policy.CheckAllowed` method

**Change class:** Internal logic refactor.

**Files:**
- Create: `internal/dnspolicy/policy.go`
- Create: `internal/dnspolicy/policy_test.go`

**Step 1: Write failing tests**

```go
package dnspolicy

import (
    "strings"
    "testing"
)

func TestCheckAllowed(t *testing.T) {
    p := &Policy{Zone: "z", Entries: []Entry{
        {Owner: "sre", Default: true},
        {Owner: "multisite", Patterns: []string{"www", "admin", "tour.*"}, Types: []string{"A", "CNAME"}},
    }}

    cases := []struct{ name, recordType, owner string; wantErr bool; errSub string }{
        {"www", "A", "multisite", false, ""},               // pattern + type match
        {"www", "A", "sre", true, "denied"},                // owner mismatch (sre is default)
        {"bandname", "A", "sre", false, ""},                // sre default catches unmatched
        {"bandname", "A", "multisite", true, "denied"},     // no pattern match
        {"www", "MX", "multisite", true, "type"},           // type not in list
        {"www", "MX", "sre", false, ""},                    // sre owns all types (no type restriction)
        {"tour.bandname", "CNAME", "multisite", false, ""}, // glob match
        {"www", "SOA", "sre", true, "SOA never delegated"}, // SOA always SRE
        {"www", "NS", "sre", true, "NS never delegated"},   // NS always SRE
        // Wait: re-read design. SOA/NS are always SRE-only EVEN when sre is in policy?
        // Per design: "Default: all types except SOA/NS (always SRE-only)".
        // The Types field default is "all except SOA/NS". sre has no Types field → all-except-SOA/NS.
        // So sre cannot upsert SOA/NS via this gate.
    }
    for _, c := range cases {
        err := p.CheckAllowed(c.name, c.recordType, c.owner)
        if (err != nil) != c.wantErr {
            t.Errorf("CheckAllowed(%q,%q,%q) err=%v wantErr=%v", c.name, c.recordType, c.owner, err, c.wantErr)
        }
        if err != nil && c.errSub != "" && !strings.Contains(err.Error(), c.errSub) {
            t.Errorf("CheckAllowed(%q,%q,%q) err=%q want substring %q", c.name, c.recordType, c.owner, err, c.errSub)
        }
    }
}

func TestCheckAllowed_NoDefaultFailsClosed(t *testing.T) {
    p := &Policy{Zone: "z", Entries: []Entry{
        {Owner: "multisite", Patterns: []string{"www"}},
    }}
    err := p.CheckAllowed("bandname", "A", "anyone")
    if err == nil { t.Errorf("expected fail-closed denial for unmatched name with zero defaults") }
}
```

**Step 2: Run — FAIL** (undefined: CheckAllowed).

**Step 3: Implement**

`internal/dnspolicy/policy.go`:
```go
package dnspolicy

import "fmt"

var protectedTypes = map[string]bool{"SOA": true, "NS": true}

// CheckAllowed returns nil if owner may upsert (name, recordType) under this policy.
// Returns an error describing the denial otherwise.
//
// Priority semantics (closes plan-cycle-1 C-3):
//   1. Explicit pattern claims take precedence over default-owner fallback.
//   2. If any owner (including non-caller) has an explicit pattern matching
//      (name, recordType), only that owner may mutate.
//   3. Default owner catches only unmatched records.
//   4. SOA/NS protected unless explicitly listed in the owner's Types.
func (p *Policy) CheckAllowed(name, recordType, owner string) error {
    // Phase 1: find any explicit pattern claim (any owner) — explicit beats default
    var explicitClaimer string
    for _, e := range p.Entries {
        if e.Default && len(e.Patterns) == 0 {
            continue // skip pure default-only entries in phase 1
        }
        if matchesEntry(e, name, recordType) {
            explicitClaimer = e.Owner
            if e.Owner == owner {
                if protectedTypes[recordType] && !isProtectedAllowed(e, recordType) {
                    return fmt.Errorf("dnspolicy: record type %s never delegated (zone-level only)", recordType)
                }
                return nil // explicit claim by caller → allow
            }
        }
    }
    if explicitClaimer != "" {
        return fmt.Errorf("dnspolicy: denied — name=%q type=%s owner=%q; explicitly claimed by owner=%q", name, recordType, owner, explicitClaimer)
    }
    // Phase 2: no explicit claim exists → fall back to default owner if caller is default.
    // (Closes plan-cycle-2 I-3) — also apply Types restriction here; non-empty e.Types restricts the default owner too.
    for _, e := range p.Entries {
        if e.Default && e.Owner == owner {
            // Types restriction: empty = all-types-except-protected; non-empty = exact list
            if len(e.Types) > 0 {
                ok := false
                for _, t := range e.Types {
                    if t == recordType { ok = true; break }
                }
                if !ok {
                    return fmt.Errorf("dnspolicy: denied — name=%q type=%s owner=%q; default owner restricted to types %v", name, recordType, owner, e.Types)
                }
            }
            if protectedTypes[recordType] && !isProtectedAllowed(e, recordType) {
                return fmt.Errorf("dnspolicy: record type %s never delegated (zone-level only)", recordType)
            }
            return nil
        }
    }
    // Phase 3: no match anywhere → fail-closed
    return fmt.Errorf("dnspolicy: denied — name=%q type=%s owner=%q matches no delegate and no default owner exists for this caller", name, recordType, owner)
}

// matchesEntry returns true if entry's patterns + types cover (name, recordType).
// Does NOT consider e.Default — that's caller's job (see CheckAllowed phase 1 skip).
func matchesEntry(e Entry, name, recordType string) bool {
    // Type scoping
    if len(e.Types) > 0 {
        ok := false
        for _, t := range e.Types {
            if t == recordType { ok = true; break }
        }
        if !ok { return false }
    }
    // Pattern match (default-only entries with no patterns are handled by caller)
    for _, pat := range e.Patterns {
        if MatchPattern(pat, name) { return true }
    }
    return false
}

func isProtectedAllowed(e Entry, recordType string) bool {
    for _, t := range e.Types {
        if t == recordType { return true }
    }
    return false
}
```

**Closes C-3**: phase-1 loop now skips default-only entries; explicit claims (even by non-caller) deny default-owner override. Phase 2 only fires when zero explicit claim exists.

**Step 4: Run tests — PASS**

Run: `GOWORK=off go test ./internal/dnspolicy/ -count=1`
Expected: all tests PASS (including the SOA/NS protection cases).

**Step 5: Commit**

```bash
git add internal/dnspolicy/policy.go internal/dnspolicy/policy_test.go
git commit -m "feat(dnspolicy): Policy.CheckAllowed with SOA/NS protection + clear denial msgs"
git push
```

**Rollback:** revert commit.

---

### Task 4: `DNSPolicyReader` + `DNSRecordWriter` + `Adapter` interfaces

**Change class:** Internal logic refactor (interface declaration).

**Files:**
- Create: `internal/dnspolicy/reader.go`
- Create: `internal/dnspolicy/writer.go`

**Step 1: Write the interfaces**

`internal/dnspolicy/reader.go`:
```go
package dnspolicy

import "context"

// DNSPolicyReader is the narrow interface the gate needs.
// Tests mock this directly; only 2 methods to fake.
type DNSPolicyReader interface {
    GetTXT(ctx context.Context, name string) ([]string, error)
    UpsertTXT(ctx context.Context, name string, values []string, ttl int) error
}
```

`internal/dnspolicy/writer.go`:
```go
package dnspolicy

import "context"

// DNSRecordWriter performs arbitrary DNS record mutations (post-gate).
type DNSRecordWriter interface {
    UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (recordID string, err error)
    DeleteRecord(ctx context.Context, zone, name, recordType string) error
}

// Adapter combines policy R/W and record R/W in one type.
// dnsprovider.NewAdapter returns this combined interface.
type Adapter interface {
    DNSPolicyReader
    DNSRecordWriter
}
```

**Step 2: Verify compiles**

Run: `GOWORK=off go build ./internal/dnspolicy/...`
Expected: no errors (no test for pure interface declarations).

**Step 3: Commit**

```bash
git add internal/dnspolicy/reader.go internal/dnspolicy/writer.go
git commit -m "feat(dnspolicy): DNSPolicyReader + DNSRecordWriter + Adapter interfaces"
git push
```

**Rollback:** revert commit.

---

### Task 5: `internal/dnsgate/` — `Gate` function

**Change class:** Internal logic refactor.

**Files:**
- Create: `internal/dnsgate/gate.go`
- Create: `internal/dnsgate/gate_test.go`

**Step 1: Write failing tests**

```go
package dnsgate

import (
    "context"
    "errors"
    "testing"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
)

type fakeReader struct {
    txtRRs []string
    err    error
}

func (f *fakeReader) GetTXT(_ context.Context, _ string) ([]string, error) {
    return f.txtRRs, f.err
}
func (f *fakeReader) UpsertTXT(_ context.Context, _ string, _ []string, _ int) error { return nil }

func TestGate_Allowed(t *testing.T) {
    reader := &fakeReader{txtRRs: []string{
        `heritage=wfinfra-v1 o=sre d=true`,
        `heritage=wfinfra-v1 o=multisite p=www,admin`,
    }}
    if err := Gate(context.Background(), reader, "z.com", "www", "A", "multisite"); err != nil {
        t.Errorf("expected pass, got %v", err)
    }
}

func TestGate_Denied(t *testing.T) {
    reader := &fakeReader{txtRRs: []string{
        `heritage=wfinfra-v1 o=sre d=true`,
        `heritage=wfinfra-v1 o=multisite p=www`,
    }}
    err := Gate(context.Background(), reader, "z.com", "bandname", "A", "multisite")
    if err == nil { t.Errorf("expected denial") }
}

func TestGate_FailClosedOnEmptyPolicy(t *testing.T) {
    reader := &fakeReader{txtRRs: []string{}}
    err := Gate(context.Background(), reader, "z.com", "www", "A", "anyone")
    if err == nil { t.Errorf("expected fail-closed when no policy exists") }
}

func TestGate_PropagatesParseError(t *testing.T) {
    reader := &fakeReader{txtRRs: []string{
        `heritage=wfinfra-v1 o=sre d=true`,
        `heritage=wfinfra-v1 o=multisite d=true p=www`, // two defaults
    }}
    err := Gate(context.Background(), reader, "z.com", "www", "A", "sre")
    if !errors.Is(err, dnspolicy.ErrMultipleDefaults) {
        t.Errorf("want ErrMultipleDefaults, got %v", err)
    }
}
```

**Step 2: Run — FAIL** (undefined: Gate).

**Step 3: Implement**

`internal/dnsgate/gate.go`:
```go
package dnsgate

import (
    "context"
    "fmt"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
)

// PolicyName returns the TXT name where policy lives for a zone.
func PolicyName(zone string) string { return "_workflow-dns-policy." + zone }

// Gate validates that owner may mutate (name, recordType) in zone per the live policy.
// Returns nil on pass; descriptive error on denial or fetch/parse failure.
func Gate(ctx context.Context, reader dnspolicy.DNSPolicyReader, zone, name, recordType, owner string) error {
    rrs, err := reader.GetTXT(ctx, PolicyName(zone))
    if err != nil {
        return fmt.Errorf("dnsgate: fetch policy: %w", err)
    }
    policy, err := dnspolicy.Parse(zone, rrs)
    if err != nil { return err }
    if len(policy.Entries) == 0 {
        return fmt.Errorf("dnsgate: fail-closed — no policy found at %s", PolicyName(zone))
    }
    return policy.CheckAllowed(name, recordType, owner)
}
```

**Step 4: Run tests — PASS**

Run: `GOWORK=off go test ./internal/dnsgate/ -count=1 -v`
Expected: all 4 tests PASS.

**Step 5: Commit**

```bash
git add internal/dnsgate/
git commit -m "feat(dnsgate): Gate function orchestrates GetTXT → Parse → CheckAllowed"
git push
```

**Rollback:** revert commit.

---

### Task 6: Proto messages + `plugin.contracts.json` step entry

**Change class:** Schema migration (proto-additive).

**Files:**
- Modify: `internal/contracts/infra.proto` (add 3 messages)
- Modify: `plugin.contracts.json` (add step entry)
- Regenerate: `internal/contracts/infra.pb.go` (via `make proto` or `buf generate`)

**Step 1: Write failing test** (post-regen, build verification only — no functional test until Task 7/8 wire it)

`internal/contracts/infra_dns_record_test.go`:
```go
package contracts

import "testing"

func TestDNSRecordStepInputProtoExists(t *testing.T) {
    var _ = &DNSRecordStepInput{Name: "www", RecordType: "A", Owner: "multisite"}
    var _ = &DNSRecordStepConfig{Provider: "digitalocean", Zone: "z", ProviderCreds: map[string]string{"token": "x"}}
    var _ = &DNSRecordStepOutput{Status: "ok"}
}
```

**Step 2: Run — FAIL** (undefined types).

**Step 3: Implement**

Append to `internal/contracts/infra.proto`:
```proto
// Static per-step config (resolved once at module construction).
message DNSRecordStepConfig {
  string provider               = 1;
  map<string, string> provider_creds = 2;
  string zone                   = 3;
}

message DNSRecordStepInput {
  string name        = 1;
  string record_type = 2;
  string data        = 3;
  int32  ttl         = 4;
  int32  priority    = 5;
  string owner       = 6;
  string operation   = 7;
}

message DNSRecordStepOutput {
  string status        = 1;
  string record_id     = 2;
  string denial_reason = 3;
}
```

Regenerate Go bindings (closes plan-cycle-1 I-1 — Makefile has no proto target; protoc is canonical):

```bash
# protoc v34.1 verified in PATH
protoc --go_out=. --go_opt=paths=source_relative internal/contracts/infra.proto
```

Append step entry to `plugin.contracts.json` "contracts" array:
```json
{
  "kind": "step",
  "type": "infra.dns_record",
  "mode": "strict",
  "config": "workflow.plugins.infra.v1.DNSRecordStepConfig",
  "input":  "workflow.plugins.infra.v1.DNSRecordStepInput",
  "output": "workflow.plugins.infra.v1.DNSRecordStepOutput"
}
```

**ALSO** (closes plan-cycle-2 C-1): patch `internal/plugin_test.go` — the existing kind-guards hard-fatalf on any non-module contract. Update both:

```go
// In TestContractDeclaresStrictModuleContracts (around line 46):
for _, contract := range registry.Contracts {
    if contract.Kind == pb.ContractKind_CONTRACT_KIND_STEP {
        continue // step contracts validated in TestContractDeclaresStrictStepContracts (new)
    }
    if contract.Kind != pb.ContractKind_CONTRACT_KIND_MODULE {
        t.Fatalf("unexpected contract kind %s", contract.Kind)
    }
    // ... existing module-key handling
}

// In loadManifestContracts (around line 184):
if contract.Kind == "step" {
    continue // skip; step contracts loaded separately
}
if contract.Kind != "module" {
    t.Fatalf("unexpected contract kind %q in plugin.contracts.json", contract.Kind)
}
```

Add new test `TestContractDeclaresStrictStepContracts` that asserts the step contract for `infra.dns_record` is registered with mode=strict + non-empty config/input/output descriptors.

**Step 4: Run tests — PASS**

```bash
GOWORK=off go build ./...
GOWORK=off go test ./internal/contracts/ -count=1
```
Expected: build PASS; contract types exist.

Also: `wfctl plugin validate-contract --for-publish --tag v0.2.0 .` should still pass (proto additions are additive).

**Step 5: Commit**

```bash
git add internal/contracts/infra.proto internal/contracts/infra.pb.go internal/contracts/infra_dns_record_test.go plugin.contracts.json
git commit -m "feat(contracts): DNSRecordStepConfig/Input/Output proto + step contract entry"
git push
```

**Rollback:** revert commit. Proto additions are unused until Task 8 wires them.

---

### Task 7: `internal/dnsprovider/` — libdns adapter for DO + Cloudflare

**Change class:** Plugin / extension (new package + new external dep).

**Files:**
- Create: `internal/dnsprovider/adapter.go`
- Create: `internal/dnsprovider/digitalocean.go`
- Create: `internal/dnsprovider/cloudflare.go`
- Create: `internal/dnsprovider/apply.go`
- Create: `internal/dnsprovider/expand.go`
- Create: `internal/dnsprovider/adapter_test.go`
- Modify: `go.mod` (add `github.com/libdns/digitalocean` + `github.com/libdns/cloudflare`)

**Step 1: Write failing tests**

```go
package dnsprovider

import (
    "errors"
    "os"
    "testing"
)

func TestNewAdapter_UnknownProvider(t *testing.T) {
    _, err := NewAdapter("unknown", map[string]string{})
    if !errors.Is(err, ErrUnknownProvider) {
        t.Errorf("want ErrUnknownProvider, got %v", err)
    }
}

func TestNewAdapter_CaseFold(t *testing.T) {
    a1, err1 := NewAdapter("DigitalOcean", map[string]string{"token": "x"})
    a2, err2 := NewAdapter("digitalocean", map[string]string{"token": "x"})
    if err1 != nil || err2 != nil { t.Fatalf("case-fold errors: %v / %v", err1, err2) }
    if a1 == nil || a2 == nil { t.Errorf("nil adapters") }
}

func TestExpandCredsMap(t *testing.T) {
    os.Setenv("DNS_TEST_TOKEN", "expanded-value")
    defer os.Unsetenv("DNS_TEST_TOKEN")
    in := map[string]string{"token": "$DNS_TEST_TOKEN", "literal": "raw"}
    out := ExpandCredsMap(in)
    if out["token"] != "expanded-value" { t.Errorf("got %q", out["token"]) }
    if out["literal"] != "raw" { t.Errorf("got %q", out["literal"]) }
}
```

**Step 2: Run — FAIL** (undefined).

**Step 3: Implement**

`internal/dnsprovider/adapter.go`:
```go
package dnsprovider

import (
    "errors"
    "fmt"
    "strings"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
)

var ErrUnknownProvider = errors.New("dnsprovider: unknown provider")

// NewAdapter dispatches on provider name (case-folded) + creds map.
// v1 supports digitalocean + cloudflare. Unknown providers return
// ErrUnknownProvider with the supported list.
func NewAdapter(provider string, creds map[string]string) (dnspolicy.Adapter, error) {
    switch strings.ToLower(strings.TrimSpace(provider)) {
    case "digitalocean":
        return newDigitalOceanAdapter(creds)
    case "cloudflare":
        return newCloudflareAdapter(creds)
    default:
        return nil, fmt.Errorf("%w %q (supported: digitalocean, cloudflare)", ErrUnknownProvider, provider)
    }
}
```

`internal/dnsprovider/expand.go` (EXPORTED — used by plugin.go in Task 8; closes plan-cycle-1 I-2):
```go
package dnsprovider

import "os"

// ExpandCredsMap applies os.ExpandEnv to each value.
// Template-form ('{{ env "X" }}') is pre-resolved by the engine;
// this catches bare-shell form ('$X' or '${X}').
// EXPORTED: called from internal/plugin.go step handler.
func ExpandCredsMap(in map[string]string) map[string]string {
    out := make(map[string]string, len(in))
    for k, v := range in {
        out[k] = os.ExpandEnv(v)
    }
    return out
}
```

`internal/dnsprovider/digitalocean.go` (sketch — verify libdns method names during impl):
```go
package dnsprovider

import (
    "context"
    "fmt"
    "strconv"
    "time"

    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
    "github.com/libdns/digitalocean"
    "github.com/libdns/libdns"
)

type doAdapter struct{ provider *digitalocean.Provider }

func newDigitalOceanAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
    token := ExpandCredsMap(creds)["token"]
    if token == "" {
        return nil, fmt.Errorf("digitalocean: missing creds.token")
    }
    return &doAdapter{provider: &digitalocean.Provider{APIToken: token}}, nil
}

func (a *doAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
    // libdns GetRecords takes the zone; we filter to name + TXT
    zone := zoneFromFQDN(name) // helper: strip leading "_<sub>." until apex
    recs, err := a.provider.GetRecords(ctx, zone)
    if err != nil {
        return nil, fmt.Errorf("digitalocean: get records: %w (creds redacted)", err)
    }
    var out []string
    relName := strings.TrimSuffix(name, "."+zone)
    for _, r := range recs {
        if r.Type == "TXT" && r.Name == relName {
            out = append(out, r.Value)
        }
    }
    return out, nil
}

// upsertOrAppend: DO-specific pattern (closes plan-cycle-1 C-4 + C-5).
// libdns/digitalocean SetRecords requires existing ID via idFromRecord; passing
// a new Record without ID errors with strconv.Atoi failure. Use GET-then-
// AppendRecords (new) OR SetRecords-with-ID (existing) per-record.
func (a *doAdapter) upsertRecords(ctx context.Context, zone string, desired []libdns.Record) ([]libdns.Record, error) {
    existing, err := a.provider.GetRecords(ctx, zone)
    if err != nil {
        return nil, fmt.Errorf("digitalocean: list records: %w (creds redacted)", err)
    }
    // Match existing records by (Type, Name) and reuse their ID for SetRecords;
    // anything unmatched goes through AppendRecords (creates new).
    var updates, appends []libdns.Record
    for _, d := range desired {
        matched := false
        for _, e := range existing {
            if e.Type == d.Type && e.Name == d.Name && e.Value == d.Value {
                matched = true // exact match — no-op (idempotent)
                break
            }
            if e.Type == d.Type && e.Name == d.Name {
                d.ID = e.ID
                updates = append(updates, d)
                matched = true
                break
            }
        }
        if !matched { appends = append(appends, d) }
    }
    var out []libdns.Record
    if len(updates) > 0 {
        u, err := a.provider.SetRecords(ctx, zone, updates)
        if err != nil { return nil, fmt.Errorf("digitalocean: update records: %w (creds redacted)", err) }
        out = append(out, u...)
    }
    if len(appends) > 0 {
        a2, err := a.provider.AppendRecords(ctx, zone, appends)
        if err != nil { return nil, fmt.Errorf("digitalocean: append records: %w (creds redacted)", err) }
        out = append(out, a2...)
    }
    return out, nil
}

func (a *doAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
    zone := zoneFromFQDN(name)
    relName := strings.TrimSuffix(name, "."+zone)
    recs := make([]libdns.Record, len(values))
    for i, v := range values {
        recs[i] = libdns.Record{Type: "TXT", Name: relName, Value: v, TTL: time.Duration(ttl) * time.Second}
    }
    _, err := a.upsertRecords(ctx, zone, recs)
    return err
}

func (a *doAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
    rec := libdns.Record{Type: recordType, Name: name, Value: data, TTL: time.Duration(ttl) * time.Second, Priority: uint(priority)}
    out, err := a.upsertRecords(ctx, zone, []libdns.Record{rec})
    if err != nil { return "", err }
    if len(out) > 0 { return out[0].ID, nil }
    return "", nil
}

// DeleteRecord: GET first to find ID, then DeleteRecords with ID.
// libdns/digitalocean DeleteRecords requires ID (closes plan-cycle-1 C-5).
func (a *doAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
    existing, err := a.provider.GetRecords(ctx, zone)
    if err != nil { return fmt.Errorf("digitalocean: list records: %w (creds redacted)", err) }
    var toDelete []libdns.Record
    for _, e := range existing {
        if e.Type == recordType && e.Name == name {
            toDelete = append(toDelete, e) // preserves ID
        }
    }
    if len(toDelete) == 0 {
        return nil // idempotent: nothing to delete
    }
    _, err = a.provider.DeleteRecords(ctx, zone, toDelete)
    if err != nil { return fmt.Errorf("digitalocean: delete records: %w (creds redacted)", err) }
    return nil
}

// zoneFromFQDN extracts the zone from a fully-qualified name.
// For "_workflow-dns-policy.gocodealone.tech" → "gocodealone.tech".
// Per design: the policy TXT is always at "_workflow-dns-policy.<zone>";
// the caller knows the zone separately. This helper is conservative
// but real callers should pass zone explicitly. (Refactor in Apply.)
func zoneFromFQDN(fqdn string) string {
    // Trim the leading "_workflow-dns-policy." prefix if present
    const prefix = "_workflow-dns-policy."
    if strings.HasPrefix(fqdn, prefix) { return fqdn[len(prefix):] }
    return fqdn
}

var _ = strings.TrimSuffix // keep import
```

`internal/dnsprovider/cloudflare.go`: structurally identical to digitalocean.go, using `libdns/cloudflare.Provider{APIToken: token}`. Same 4 methods.

`internal/dnsprovider/apply.go`:
```go
package dnsprovider

import (
    "context"

    "github.com/GoCodeAlone/workflow/plugin/external/sdk"
    "github.com/GoCodeAlone/workflow-plugin-infra/internal/contracts"
    "github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
)

// Apply performs the actual DNS record mutation post-gate-pass.
// Returns a typed step result with status="ok"|"provider-error".
func Apply(ctx context.Context, a dnspolicy.Adapter, cfg *contracts.DNSRecordStepConfig, input *contracts.DNSRecordStepInput) (*sdk.TypedStepResult[*contracts.DNSRecordStepOutput], error) {
    var recordID string
    var err error
    op := input.Operation
    if op == "" { op = "upsert" }
    switch op {
    case "upsert":
        recordID, err = a.UpsertRecord(ctx, cfg.Zone, input.Name, input.RecordType, input.Data, input.Ttl, input.Priority)
    case "delete":
        err = a.DeleteRecord(ctx, cfg.Zone, input.Name, input.RecordType)
    default:
        return &sdk.TypedStepResult[*contracts.DNSRecordStepOutput]{
            Output: &contracts.DNSRecordStepOutput{Status: "provider-error", DenialReason: "unknown operation: " + op},
        }, nil
    }
    if err != nil {
        return &sdk.TypedStepResult[*contracts.DNSRecordStepOutput]{
            Output: &contracts.DNSRecordStepOutput{Status: "provider-error", DenialReason: err.Error()},
        }, nil
    }
    return &sdk.TypedStepResult[*contracts.DNSRecordStepOutput]{
        Output: &contracts.DNSRecordStepOutput{Status: "ok", RecordId: recordID},
    }, nil
}
```

Update `go.mod`:
```bash
GOWORK=off go get github.com/libdns/libdns@latest
GOWORK=off go get github.com/libdns/digitalocean@latest
GOWORK=off go get github.com/libdns/cloudflare@latest
GOWORK=off go mod tidy
```

**Step 4: Run tests + build verify**

```bash
GOWORK=off go test ./internal/dnsprovider/ -count=1
GOWORK=off go build ./...
```
Expected: tests PASS, full plugin build PASS.

**Note**: integration tests against live DO/CF APIs are out-of-scope (require live credentials + side-effecting network calls). Manual smoke via the admin CLI in Task 9 is the end-to-end verification.

**Step 5: Commit**

```bash
git add internal/dnsprovider/ go.mod go.sum
git commit -m "feat(dnsprovider): DO + Cloudflare libdns adapters + NewAdapter + Apply + ExpandCredsMap

internal/dnsprovider package isolates the libdns boundary. Adapter
combines DNSPolicyReader + DNSRecordWriter. NewAdapter dispatches
on provider name (case-folded) + ErrUnknownProvider sentinel.
v1 supports digitalocean + cloudflare (single-token providers).
ExpandCredsMap applies os.ExpandEnv for bare-shell creds. Apply
performs post-gate upsert/delete with redacted provider errors."
git push
```

**Rollback:** revert commit + `go mod tidy` to drop libdns deps.

**Runtime impact**: this adds external Go-module dependencies. Per `finishing-a-development-branch` Step 1b: this is a version-pin change. Run version-skew audit + relaunch verification in Task 10.

---

### Task 8: Register `infra.dns_record` step in `plugin.go` + deprecate `infra.dns` module

**Change class:** Plugin / extension (wires step into engine).

**Files:**
- Modify: `internal/plugin.go` (add StepTypes + CreateStep + TypedStepTypes + CreateTypedStep, modify infra.dns Start())
- Modify: `internal/plugin_test.go` (add tests)
- Create: `docs/migration/infra-dns-to-step.md`

**Step 1: Write failing tests**

Append to `internal/plugin_test.go`:
```go
func TestPlugin_StepTypes_Includes_DnsRecord(t *testing.T) {
    p := NewInfraPlugin()
    found := false
    for _, st := range p.StepTypes() {
        if st == "infra.dns_record" { found = true; break }
    }
    if !found { t.Errorf("infra.dns_record not in StepTypes(): %v", p.StepTypes()) }
}

func TestPlugin_TypedStepTypes_Includes_DnsRecord(t *testing.T) {
    p := NewInfraPlugin()
    found := false
    for _, st := range p.TypedStepTypes() {
        if st == "infra.dns_record" { found = true; break }
    }
    if !found { t.Errorf("infra.dns_record not in TypedStepTypes(): %v", p.TypedStepTypes()) }
}

func TestInfraDnsModule_DeprecatedStartReturnsError(t *testing.T) {
    p := NewInfraPlugin()
    m, err := p.CreateModule("infra.dns", "test", map[string]any{})
    if err != nil { t.Fatal(err) }
    err = m.Start(context.Background())
    if err == nil { t.Errorf("expected deprecation error from infra.dns Start()") }
    if !strings.Contains(err.Error(), "deprecated") {
        t.Errorf("expected 'deprecated' in err, got %v", err)
    }
}
```

**Step 2: Run — FAIL** (StepTypes returns empty; Start() returns nil).

**Step 3: Implement**

In `internal/plugin.go`:

```go
// Replace existing StepTypes:
func (p *infraPlugin) StepTypes() []string {
    return []string{"infra.dns_record"}
}

func (p *infraPlugin) CreateStep(typeName, name string, config map[string]any) (sdk.StepInstance, error) {
    return nil, fmt.Errorf("infra.dns_record requires typed config; legacy untyped path not supported")
}

func (p *infraPlugin) TypedStepTypes() []string {
    return []string{"infra.dns_record"}
}

func (p *infraPlugin) CreateTypedStep(typeName, name string, config *anypb.Any) (sdk.StepInstance, error) {
    handler := func(ctx context.Context, req sdk.TypedStepRequest[*contracts.DNSRecordStepConfig, *contracts.DNSRecordStepInput]) (*sdk.TypedStepResult[*contracts.DNSRecordStepOutput], error) {
        creds := dnsprovider.ExpandCredsMap(req.Config.ProviderCreds)
        adapter, err := dnsprovider.NewAdapter(req.Config.Provider, creds)
        if err != nil { return nil, err }
        if gerr := dnsgate.Gate(ctx, adapter, req.Config.Zone, req.Input.Name, req.Input.RecordType, req.Input.Owner); gerr != nil {
            dnsaudit.LogOutcome("step-execute", req.Config.Zone, req.Input.Name, req.Input.RecordType, "gate-denied", gerr.Error())
            return &sdk.TypedStepResult[*contracts.DNSRecordStepOutput]{
                Output: &contracts.DNSRecordStepOutput{Status: "gate-denied", DenialReason: gerr.Error()},
            }, nil
        }
        op := req.Input.Operation
        if op == "" { op = "upsert" }
        dnsaudit.LogAttempt("step-execute", req.Config.Zone, req.Input.Name, req.Input.RecordType, op, req.Input.Owner, req.Config.Provider)
        result, applyErr := dnsprovider.Apply(ctx, adapter, req.Config, req.Input)
        // Apply never returns errors directly — it encodes outcome in result.Output.Status
        outcome := result.Output.Status
        errMsg := ""
        if outcome != "ok" { errMsg = result.Output.DenialReason }
        dnsaudit.LogOutcome("step-execute", req.Config.Zone, req.Input.Name, req.Input.RecordType, outcome, errMsg)
        return result, applyErr
    }
    factory := sdk.NewTypedStepFactory[*contracts.DNSRecordStepConfig, *contracts.DNSRecordStepInput, *contracts.DNSRecordStepOutput](
        typeName,
        &contracts.DNSRecordStepConfig{},
        &contracts.DNSRecordStepInput{},
        handler,
    )
    return factory.CreateTypedStep(typeName, name, config)
}

// Modify the existing infra.dns Start (find by infraType at line 205):
// NOTE: struct field is m.infraType (NOT m.typeName) — verified in plugin.go:193
func (m *infraModule) Start(_ context.Context) error {
    if m.infraType == "infra.dns" {
        return fmt.Errorf("infra.dns module is deprecated; use the infra.dns_record step type instead. See docs/migration/infra-dns-to-step.md")
    }
    return nil
}
```

Also export `dnsprovider.ExpandCredsMap` (rename from lowercase) since plugin.go calls it.

Create `docs/migration/infra-dns-to-step.md`:
```markdown
# Migrating from `infra.dns` module to `infra.dns_record` step

The `infra.dns` MODULE type is deprecated as of workflow-plugin-infra v0.2.0.
It now returns a non-nil error from `Start()` with this migration hint.

## Why

`infra.dns` was registered as a module (long-lived resource holder), but DNS
record operations are discrete actions. The new `infra.dns_record` step type
is the correct primitive: invoked per-record, integrated with the
`_workflow-dns-policy` ownership gate.

## Old (no longer works)

```yaml
modules:
  - type: infra.dns
    config: { provider: digitalocean, zone: example.com, ... }
```

## New

```yaml
steps:
  - type: infra.dns_record
    config:
      provider: digitalocean
      provider_creds: { token: '{{ env "DO_TOKEN" }}' }
      zone: example.com
    input:
      name: www
      record_type: A
      data: 1.2.3.4
      ttl: 60
      owner: multisite          # REQUIRED — for policy gate check
      operation: upsert         # upsert (default) | delete
```

## Bootstrap (one-time per zone)

Before any `infra.dns_record` step can apply, the zone must have a policy:

```
wfctl infra-dns set-policy example.com -f ownership/example.com.yaml --bootstrap --as-owner sre
```

See SPEC.md / design doc for policy format details.
```

**Step 4: Run tests — PASS**

```bash
GOWORK=off go test ./... -count=1
GOWORK=off go build ./...
```
Expected: 3 new tests PASS; full build green; existing tests pass (deprecation Start() only fires for `infra.dns` typeName).

**Step 5: Commit**

```bash
git add internal/plugin.go internal/plugin_test.go docs/migration/infra-dns-to-step.md
git commit -m "feat(plugin): register infra.dns_record step + handler + deprecate infra.dns module

- StepTypes/CreateStep + TypedStepTypes/CreateTypedStep wire the new step type
- Handler closure: ExpandCredsMap → NewAdapter → Gate → Apply
- infra.dns module Start() returns migration-hint error
- docs/migration/infra-dns-to-step.md guides operators

Rollback: revert commit + remove docs file. (Module deprecation reverses;
existing infra.dns YAML configs would re-work.)"
git push
```

**Rollback:** revert commit. infra.dns module Start() returns to no-op nil; existing consumers (if any) unaffected.

---

### Task 9: `internal/admincli/` — `wfctl infra-dns` subcommands + `internal/dnsaudit/` shared audit pkg

**Change class:** CLI command (new subcommand surface) + shared audit package extraction.

**Files:**
- Create: `internal/dnsaudit/audit.go` (shared LogAttempt/LogOutcome/LogPolicyEdit; closes plan-cycle-1 I-3)
- Create: `internal/dnsaudit/audit_test.go`
- Create: `internal/admincli/cli.go` (CLIProvider + dispatch)
- Create: `internal/admincli/set_policy.go`
- Create: `internal/admincli/drift.go`
- Create: `internal/admincli/transfer_ownership.go`
- Create: `internal/admincli/policy_show.go`
- Create: `internal/admincli/cli_test.go`

**Architecture note**: audit functions live in `internal/dnsaudit/` (NOT `internal/admincli/`) so the step handler in `internal/plugin.go` can import them without creating a wrong-direction dependency (plugin layer → CLI layer would be backward). Both admincli AND plugin.go import dnsaudit.

**Step 1: Write failing tests** (test CLI dispatch + audit log shape; admin commands themselves get smoke tests in Task 10)

```go
package admincli

import (
    "strings"
    "testing"
)

func TestCLIProvider_DispatchSubcommands(t *testing.T) {
    cases := []struct{ args []string; wantCode int; wantOutSub string }{
        {[]string{"infra-dns"}, 2, "usage"},               // no subcommand
        {[]string{"infra-dns", "unknown"}, 2, "unknown"},
        {[]string{"infra-dns", "set-policy"}, 2, "zone"},  // missing zone arg
        {[]string{"infra-dns", "policy", "show"}, 2, "zone"},
    }
    for _, c := range cases {
        code, out := runAndCapture(c.args) // helper that captures stdout+stderr
        if code != c.wantCode { t.Errorf("args=%v code=%d want %d", c.args, code, c.wantCode) }
        if !strings.Contains(strings.ToLower(out), c.wantOutSub) {
            t.Errorf("args=%v out=%q want substring %q", c.args, out, c.wantOutSub)
        }
    }
}

// NOTE: audit test belongs in internal/dnsaudit/audit_test.go (NOT admincli)
// since LogAttempt/LogOutcome are in package dnsaudit. Closes plan-cycle-2 C-2.

// internal/dnsaudit/audit_test.go:
//   package dnsaudit
//   func TestAuditLog_AppendsAttemptThenOutcome(t *testing.T) {
//       tmp := t.TempDir()
//       t.Setenv("XDG_STATE_HOME", tmp)
//       LogAttempt("user@host", "example.com", "www", "A", "upsert", "multisite", "digitalocean")
//       LogOutcome("user@host", "example.com", "www", "A", "success", "")
//       path := tmp + "/wfctl/plugins/workflow-plugin-infra/dns-policy-audit.jsonl"
//       data, err := os.ReadFile(path)
//       if err != nil { t.Fatalf("read audit: %v", err) }
//       lines := strings.Split(strings.TrimSpace(string(data)), "\n")
//       if len(lines) != 2 { t.Errorf("want 2 lines, got %d: %s", len(lines), data) }
//   }
```

**Step 2: Run — FAIL** (undefined: CLIProvider, runAndCapture, LogAttempt, LogOutcome).

**Step 3: Implement** (sketch — implementer fills in subcommand bodies)

`internal/admincli/cli.go`:
```go
package admincli

import (
    "fmt"
    "os"
)

type CLIProvider struct{}

// RunCLI receives args AFTER the --wfctl-cli sentinel.
// args[0] = command name ("infra-dns"); args[1:] = subcommand + flags.
func (c *CLIProvider) RunCLI(args []string) int {
    if len(args) < 1 || args[0] != "infra-dns" {
        fmt.Fprintln(os.Stderr, "admincli: expected first arg 'infra-dns'")
        return 2
    }
    if len(args) < 2 {
        fmt.Fprintln(os.Stderr, "usage: wfctl infra-dns <subcommand>\nsubcommands: set-policy, drift, transfer-ownership, policy show")
        return 2
    }
    sub := args[1]
    rest := args[2:]
    switch sub {
    case "set-policy":      return setPolicy(rest)
    case "drift":           return drift(rest)
    case "transfer-ownership": return transferOwnership(rest)
    case "policy":
        if len(rest) > 0 && rest[0] == "show" { return policyShow(rest[1:]) }
        fmt.Fprintln(os.Stderr, "usage: wfctl infra-dns policy show <zone>")
        return 2
    default:
        fmt.Fprintf(os.Stderr, "admincli: unknown subcommand %q\n", sub)
        return 2
    }
}
```

`internal/dnsaudit/audit.go`:
```go
package dnsaudit

import (
    "encoding/json"
    "os"
    "path/filepath"
    "time"
)

type auditEntry struct {
    TS         string `json:"ts"`
    Actor      string `json:"actor"`
    Zone       string `json:"zone"`
    Action     string `json:"action,omitempty"`     // for policy edits
    Name       string `json:"name,omitempty"`       // for apply attempts
    RecordType string `json:"record_type,omitempty"`
    Operation  string `json:"operation,omitempty"`
    Owner      string `json:"owner,omitempty"`
    Provider   string `json:"provider,omitempty"`
    Outcome    string `json:"outcome,omitempty"`
    Error      string `json:"error,omitempty"`
    PriorSHA   string `json:"prior_sha256,omitempty"`
    NewSHA     string `json:"new_sha256,omitempty"`
}

func auditPath() string {
    base := os.Getenv("XDG_STATE_HOME")
    if base == "" { base = filepath.Join(os.Getenv("HOME"), ".local", "state") }
    return filepath.Join(base, "wfctl", "plugins", "workflow-plugin-infra", "dns-policy-audit.jsonl")
}

func appendEntry(e auditEntry) error {
    p := auditPath()
    if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil { return err }
    f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
    if err != nil { return err }
    defer f.Close()
    e.TS = time.Now().UTC().Format(time.RFC3339Nano)
    b, _ := json.Marshal(e)
    _, err = f.Write(append(b, '\n'))
    return err
}

func LogAttempt(actor, zone, name, recordType, operation, owner, provider string) {
    _ = appendEntry(auditEntry{
        Actor: actor, Zone: zone, Name: name, RecordType: recordType,
        Operation: operation, Owner: owner, Provider: provider, Outcome: "attempted",
    })
}

func LogOutcome(actor, zone, name, recordType, outcome, errMsg string) {
    _ = appendEntry(auditEntry{
        Actor: actor, Zone: zone, Name: name, RecordType: recordType,
        Outcome: outcome, Error: errMsg,
    })
}

func LogPolicyEdit(actor, zone, action, priorSHA, newSHA string) {
    _ = appendEntry(auditEntry{
        Actor: actor, Zone: zone, Action: action, PriorSHA: priorSHA, NewSHA: newSHA,
    })
}
```

`set_policy.go`, `drift.go`, `transfer_ownership.go`, `policy_show.go`: each parses its own flagset, reads YAML / current TXT, calls dnspolicy + dnsprovider, audits, prints result. Implementer fills in following the test contracts.

**Step 4: Run tests — PASS**

```bash
GOWORK=off go test ./internal/admincli/ -count=1 -v
```
Expected: dispatch + audit tests PASS.

End-to-end smoke (manual, since real DNS not in unit tests):
```bash
# Build the plugin binary
GOWORK=off go build -o /tmp/wfi ./cmd/workflow-plugin-infra

# Invoke as if wfctl dispatched it
DO_TOKEN=$your-do-token /tmp/wfi --wfctl-cli infra-dns policy show gocodealone.tech
# Expected: parses + prints current policy (or "no policy" message)
```

**Step 5: Commit**

```bash
git add internal/admincli/
git commit -m "feat(admincli): wfctl infra-dns CLIProvider + 4 subcommands + audit log

internal/admincli implements sdk.CLIProvider with set-policy / drift /
transfer-ownership / policy show subcommands. Audit log at
\$XDG_STATE_HOME/wfctl/plugins/workflow-plugin-infra/dns-policy-audit.jsonl
captures policy edits + apply attempts (LogAttempt/LogOutcome/LogPolicyEdit).
RunCLI returns exit code via ServePluginFull → os.Exit."
git push
```

**Rollback:** revert commit. CLIProvider unused until Task 10 wires it.

---

### Task 10: `main.go` ServePluginFull wiring + `plugin.json` capabilities + version bump

**Change class:** Build pipeline + version pin update + runtime startup configuration → triggers `runtime-launch-validation` per `finishing-a-development-branch` Step 1b.

**Files:**
- Modify: `cmd/workflow-plugin-infra/main.go`
- Modify: `plugin.json` (capabilities.stepTypes + capabilities.cliCommands + version bump)

**Step 1: Write end-to-end verification harness**

`cmd/workflow-plugin-infra/main_smoke_test.go`:
```go
//go:build smoke
// +build smoke

package main_test

import (
    "os/exec"
    "strings"
    "testing"
)

func TestPluginBinary_Smoke(t *testing.T) {
    bin := buildPlugin(t)
    // CLI dispatch path
    out, err := exec.Command(bin, "--wfctl-cli", "infra-dns").CombinedOutput()
    if err == nil { t.Errorf("expected non-zero exit on bare 'infra-dns'") }
    if !strings.Contains(string(out), "usage") { t.Errorf("usage missing: %s", out) }
}

func buildPlugin(t *testing.T) string {
    t.Helper()
    bin := "/tmp/wfi-smoke-" + t.Name()
    cmd := exec.Command("go", "build", "-o", bin, "./cmd/workflow-plugin-infra")
    cmd.Env = append(cmd.Environ(), "GOWORK=off")
    out, err := cmd.CombinedOutput()
    if err != nil { t.Fatalf("build: %v\n%s", err, out) }
    return bin
}
```

**Step 2: Run — FAIL** (main.go not updated; --wfctl-cli not dispatched).

**Step 3: Implement**

Replace `cmd/workflow-plugin-infra/main.go`:
```go
package main

import (
    "github.com/GoCodeAlone/workflow-plugin-infra/internal"
    "github.com/GoCodeAlone/workflow-plugin-infra/internal/admincli"
    sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func main() {
    sdk.ServePluginFull(
        internal.NewInfraPlugin(),
        &admincli.CLIProvider{},
        nil, // no hook handler
        sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)),
    )
}
```

**CRITICAL** (closes plan-cycle-1 C-2): `.goreleaser.yml:15` already injects ldflag `-X github.com/GoCodeAlone/workflow-plugin-infra/internal.Version={{ .Version }}`. Use `internal.Version` (existing pattern), NOT a new `main.Version` variable. The `Version` var is declared in `internal/plugin.go:25` (closes plan-cycle-2 m-1 — there's no separate version.go file, but the ldflag path is correct because the variable lives in package `internal`); new main.go imports it via `internal.Version`. No goreleaser change needed.

Update `plugin.json` (surgical edits — closes plan-cycle-1 I-4):

1. Change `"version": "0.1.1"` → `"version": "0.0.0"` (sentinel per #758).
2. Bump `"minEngineVersion": "0.51.7"` → `"0.64.0"` (uses ServePluginFull + TypedStepFactory[C,I,O]).
3. Inside the EXISTING `capabilities` block (do not replace; modify):
   - `moduleTypes` already lists the 13 existing module types (verbatim: `infra.container_service`, `infra.k8s_cluster`, `infra.database`, `infra.cache`, `infra.vpc`, `infra.load_balancer`, `infra.dns`, `infra.registry`, `infra.api_gateway`, `infra.firewall`, `infra.iam_role`, `infra.storage`, `infra.certificate`) — LEAVE UNCHANGED.
   - Change `"stepTypes": []` → `"stepTypes": ["infra.dns_record"]`.
   - Add new key `"cliCommands": [{ "name": "infra-dns", "description": "DNS ownership policy admin (set-policy, drift, transfer-ownership, policy show)" }]`.
   - Leave `"triggerTypes"` and any other existing keys intact.

(Note: version `0.0.0` per #758 ldflag sentinel; tagged v0.2.0 injects via goreleaser ldflag at `internal.Version`.)

**Step 4: Run smoke + version-skew + verify-capabilities**

```bash
GOWORK=off go test -tags=smoke ./cmd/workflow-plugin-infra/ -count=1
```
Expected: smoke test PASS.

Runtime-launch validation (build + invoke the binary with the contract dispatch + verify capabilities surface):
```bash
GOWORK=off go build -ldflags "-X github.com/GoCodeAlone/workflow-plugin-infra/internal.Version=v0.2.0-test" -o /tmp/wfi-test ./cmd/workflow-plugin-infra
# Verify CLI dispatch surfaces correctly
/tmp/wfi-test --wfctl-cli infra-dns 2>&1 | head -3
# Expected first line: "usage: wfctl infra-dns <subcommand>"

# Verify gRPC plugin still serves (test would require go-plugin handshake; manual)
# Validate plugin contract
wfctl plugin validate-contract --for-publish --tag v0.2.0 .
# Expected: PASS

# Run verify-capabilities against the built binary
wfctl plugin verify-capabilities --binary /tmp/wfi-test .
# Expected: PASS (manifest.Version matches injected v0.2.0)
```

**Step 5: Commit + push + run full plugin tests**

```bash
git add cmd/workflow-plugin-infra/main.go cmd/workflow-plugin-infra/main_smoke_test.go plugin.json
git commit -m "feat(plugin): main.go ServePluginFull + admincli.CLIProvider + cliCommands manifest

main.go switches from sdk.Serve to sdk.ServePluginFull so the SDK handles
--wfctl-cli dispatch + os.Exit propagation. plugin.json declares
capabilities.cliCommands[infra-dns] so wfctl plugin install registers
the dynamic subcommand. version restored to 0.0.0 sentinel per
workflow#758 ldflag pattern.

Rollback: revert commit + rebuild from previous main.go (sdk.Serve only;
infra-dns CLI subcommand unavailable but plugin gRPC serving intact)."
git push

# Full plugin test suite
GOWORK=off go test ./... -count=1
GOWORK=off go vet ./...
```
Expected: all tests PASS, no vet warnings.

**Rollback:** revert commit. Prior `sdk.Serve` path restored; `--wfctl-cli` dispatch unavailable but plugin gRPC serving continues. No data migration needed.

---

## Final verification (post-Task-10)

```bash
# 1. All unit tests pass
GOWORK=off go test -count=1 -timeout 300s ./...

# 2. Lint clean
GOWORK=off go vet ./...

# 3. Build + verify-capabilities end-to-end
GOWORK=off go build -ldflags "-X github.com/GoCodeAlone/workflow-plugin-infra/internal.Version=v0.2.0-test" -o /tmp/wfi ./cmd/workflow-plugin-infra
wfctl plugin verify-capabilities --binary /tmp/wfi .

# 4. validate-contract pass
wfctl plugin validate-contract --for-publish --tag v0.2.0 .

# 5. CLI dispatch sanity
/tmp/wfi --wfctl-cli infra-dns 2>&1 | head -3 | grep -q "usage" && echo "CLI dispatch OK"
```

## Rollback (PR-level)

`git revert <merge-sha>` reverts all 10 commits atomically. Post-revert:
- `infra.dns_record` step type vanishes; YAML referencing it errors with "unknown step type"
- `infra.dns` module returns to no-op Start() (existing untouched consumers continue working)
- `wfctl infra-dns` subcommand vanishes from CLI registry on next `wfctl plugin install`
- libdns + admincli code paths removed
- No data migration (no DB; only TXT records in DNS; SRE may leave `_workflow-dns-policy.<zone>` TXTs in place — they become inert without consumers)

## Implementer notes

- **PUSH AFTER EACH COMMIT** — #765 squash debacle lesson. Verify `git log origin/feat/dns-ownership-policy..HEAD` empty before PR open.
- GOWORK=off ALWAYS.
- Edit existing SINGLE `import (...)` blocks; never add a second `import (...)`.
- After Task 6 (proto regen): if existing tests fail due to enum/field renumbering, that's a sign the proto edit was non-additive. Re-check the proto edit; tag numbers must NOT conflict with existing fields.
- After Task 7: libdns API quirks per provider — DigitalOcean's `SetRecords` requires existing record ID; new records must use `AppendRecords`. Plan's `upsertRecords` helper handles this. Cloudflare's `SetRecords` is smarter (handles missing-ID case internally) — adapter can use SetRecords directly.
- Task 8 imports include `dnsaudit` (NEW package introduced in Task 9; if Task 8 commits before Task 9, mock the dnsaudit functions or order Task 9's dnsaudit-package extraction before Task 8's commit).
- After Task 9: real CLI subcommand body (set-policy/drift/transfer-ownership/policy show) is implementer's craft — tests anchor the dispatch + audit shape; subcommand bodies follow standard flag-parsing + dnsprovider/dnspolicy calls.
- **SOA/NS records**: gate refuses to mutate SOA/NS for ANY owner (including default-owner SRE) unless explicitly listed in that owner's `Types` field. This is intentional — DNS zone-level records should be managed via the DNS provider's console, not through automation. CLI help text + migration doc call this out (m-3).

## Adversarial cycle 2 — findings resolved inline

| Finding | Resolution |
|---|---|
| C-1 NEW plugin_test.go hard-fatalf on non-module contracts breaks Task 6 | Task 6 now includes patch instructions for both kind-guards in plugin_test.go to skip step contracts + add TestContractDeclaresStrictStepContracts. |
| C-2 NEW Task 9 audit test in wrong package | Test moved into `internal/dnsaudit/audit_test.go` (`package dnsaudit`), calls LogAttempt/LogOutcome directly. admincli tests stay in admincli package. |
| I-1 Task 10 manual build ldflag wrong symbol path | Replaced both `-X main.Version=...` with `-X github.com/GoCodeAlone/workflow-plugin-infra/internal.Version=...`. |
| I-2 Priority int32→uint wraps on negative | Implementer must add `if priority < 0 { return error }` validation in step handler before adapter call (added to Task 7/8 implementer-notes). |
| I-3 CheckAllowed phase-2 ignores Types restriction for default owner | Added Types restriction in phase-2; non-empty e.Types now limits default owner too. |
| m-1 "internal/version.go" misleading prose | Corrected to "internal/plugin.go:25" (no separate version.go file). |
| m-2 zoneFromFQDN narrow but safe | Accepted; latent footgun documented. |
| m-3 buf.yaml not verified | Implementer verifies absence during impl; if buf is used, switch to `buf generate`. |

## Adversarial cycle 1 — findings resolved inline

| Finding | Resolution |
|---|---|
| C-1 `m.typeName` undefined | Fixed to `m.infraType` (correct struct field per plugin.go:193). |
| C-2 main.Version vs goreleaser internal.Version mismatch | main.go uses `internal.Version` (existing ldflag target). No new variable. No goreleaser change. |
| C-3 CheckAllowed default-owner steals explicit claims | Refactored two-phase logic: phase 1 skips default-only entries; explicit claim by any owner blocks default fallback. |
| C-4 DO SetRecords needs ID — UpsertTXT broken | Added `upsertRecords` helper: GET → match on (Type,Name) → SetRecords for updates + AppendRecords for new. |
| C-5 DO DeleteRecords needs ID — DeleteRecord broken | GET first to fetch existing records with IDs, then DeleteRecords with full records. Idempotent (no-op if not found). |
| I-1 Makefile has no proto target | Removed "make proto" alternative; protoc is canonical. |
| I-2 ExpandCredsMap export/case mismatch | Task 7 implements as exported `ExpandCredsMap` from the start. |
| I-3 Audit functions wrong package | Extracted to `internal/dnsaudit/`. Step handler in plugin.go imports it without cyclical/wrong-direction dep. |
| I-4 plugin.json diff malformed + wrong module names | Surgical edit instructions specify exact field changes; correct 13 module type names listed verbatim. |
| m-1 MatchPattern empty-string test | Added `{"*", "", false}` test case. |
| m-2 zoneFromFQDN naming | Kept name but documented as policy-name-only helper. Inline rename optional. |
| m-3 SOA/NS doc note | Added implementer note + migration doc callout. |
| m-4 miekg/dns absent from plan | Acknowledged in design; libdns handles 255-byte joining transparently. No miekg/dns dep needed. |
