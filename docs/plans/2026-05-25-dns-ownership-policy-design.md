# DNS ownership policy gate — design

Per-record DNS ownership marker + policy enforcement gate. Lives in `workflow-plugin-infra` (the IaC layer for shared infra resources). Fired by `wfctl apply` before DNS mutations land.

## Problem

Shared DNS zones have multiple stakeholders:
- **SRE/owner** holds the zone (apex, MX, SPF/DMARC, NS records). Manages most records.
- **Applications** (multisite, BMW, ratchet) need to provision specific records (subdomains, ACME challenges, etc.) without SRE intervention at deploy time.
- SRE must NOT undo app records; apps must NOT touch records outside their scope.

Zone-level "managed-by" is too coarse. Per-record sidecar TXT records (ExternalDNS pattern) is too verbose and pollutes the zone. Need pattern-based ownership claims at the zone root, machine-parseable, DNS-native.

## Goal

A single TXT-record-based policy at `_dns-mgmt.<zone>` declares which named owners may manage which record-name patterns. The policy gate validates every DNS mutation against the policy before it lands.

## Non-goals

- Not a replacement for DNS provider auth (token-based). This is defense-in-depth on top of provider credentials.
- Not a runtime resolver-side enforcement (no resolver-side policy lookup). The policy is consulted at `wfctl apply` time by the workflow client; if a stale or unauthorized client bypasses `wfctl`, only the provider's auth gates the mutation.
- Not a generalized RBAC system for DNS. Pattern-based authorization only.
- Not part of `wfctl` or `workflow` core. Centralized in `workflow-plugin-infra` (per repo owner direction).

## Prior art research

| Project | Pattern | Adopt? |
|---|---|---|
| ExternalDNS (k8s-sigs) | Per-record companion TXT with `heritage=external-dns,external-dns/owner=<id>` | **Inspire-from**. Parser API is 80-line, forkable. Pattern itself doesn't fit (per-record vs zone-root). |
| octoDNS OwnershipProcessor | Per-record `_owner.<type>.<name>` TXT with value `*octodns*` | Inspire-from for the `_owner.<type>.<name>` companion-name convention (if we ever need per-record overrides). Not adopted as primary. |
| dnscontrol | None — assumes exclusive zone ownership | Ignore. |
| libdns | Provider-neutral Go interfaces | **Adopt for provider abstraction** when reading/writing the `_dns-mgmt` TXT against real providers. |
| miekg/dns | Low-level DNS RR parser | **Adopt for wire-level TXT parsing** — handles 255-byte chunking via `[]string`. |
| RFC 1464 | `key=value` TXT format with backquote escaping | **Adopt as syntactic model** — informational status. |
| RFC 8552 | `_underscore` label convention for scoped DNS attributes | **Cite as justification for `_dns-mgmt` naming**. |
| RFC 8659 (CAA) | Multiple RRs at one name, each granting a party a capability | **Structural analogy** — directly inspires our "one RR per owner at `_dns-mgmt.<zone>`" approach. |
| draft-ietf-dnsop-domain-verification-techniques-06 | `_<provider>-challenge.<domain>` convention | Cite for `_<scope>` naming legitimacy. |

**No existing OSS library solves this exact problem.** ExternalDNS comes closest but its per-record companion model is verbose for zones with many records. We write our own parser, ~80-120 lines, mimicking ExternalDNS's `labels.go` API for least Go-developer surprise.

## Design

### TXT record schema

At `_dns-mgmt.<zone>`, declare ownership policy via multiple TXT RRs — one RR per owner. Each RR carries one owner's `heritage`, `owner` name, and pattern list. CAA-style: multiple RRs co-exist, each grants a capability to a named party.

**Encoding** (RFC 1464 inspired, ExternalDNS heritage prefix):

```
heritage=wfinfra-v1 o=<owner> p=<pattern-csv> [t=<rtype-csv>] [d=true|false]
```

Field reference:

| Key | Required | Meaning |
|---|---|---|
| `heritage` | yes | Always `wfinfra-v1`. Distinguishes our policy RRs from unrelated TXT records (SPF, DMARC, Google verification, etc.). Schema version baked in (`-v1`) for forward-compat. |
| `o=<owner>` | yes | Canonical owner name. Short identifiers (e.g. `sre`, `multisite`, `bmw`, `ratchet`). Pattern: `[a-z0-9_-]{2,32}`. |
| `p=<pattern-csv>` | yes | Comma-separated record-name patterns this owner manages. Glob syntax: `*` matches a single label segment, `**` matches multiple segments. `@` matches the apex. Examples: `www`, `admin`, `tour.*`, `_acme-challenge.*`. |
| `t=<rtype-csv>` | optional | Record-type scoping. Default: all. Example: `t=A,AAAA,CNAME` restricts owner to those types. |
| `d=true` | optional | Default-owner flag. Exactly one RR per zone MAY set this; that owner gets any record not matched by another owner's patterns. |

**Why short keys (`o=`, `p=`, `t=`, `d=`)**: TXT-string-byte conservation. See "TXT limits" below.

**Multi-string handling**: each TXT RR is split into ≤255-byte strings at the DNS wire layer (`miekg/dns` does this transparently). Parser receives the joined string. Pattern-list growth is the main bloat vector; if a single owner's patterns exceed ~200 bytes of CSV, split into multiple RRs for the same owner (the parser unions them).

### Examples

#### Simple case (gocodealone.tech, SRE + multisite)

```
_dns-mgmt.gocodealone.tech. 60 IN TXT "heritage=wfinfra-v1 o=sre d=true"
_dns-mgmt.gocodealone.tech. 60 IN TXT "heritage=wfinfra-v1 o=multisite p=www,admin,_acme-challenge.www,_acme-challenge.admin"
```

Result:
- SRE owns the apex + MX + everything not matched below (catch-all via `d=true`)
- multisite owns 4 specific records
- bandname.gocodealone.tech apply by multisite → fail (no match)
- bandname.gocodealone.tech apply by SRE → pass (default)

#### Pattern + type scoping (BMW)

```
_dns-mgmt.buymywishlist.com. 60 IN TXT "heritage=wfinfra-v1 o=sre d=true"
_dns-mgmt.buymywishlist.com. 60 IN TXT "heritage=wfinfra-v1 o=bmw p=app,api,*,_acme-challenge.* t=A,AAAA,CNAME,TXT"
```

bmw may upsert A/AAAA/CNAME/TXT records matching the listed patterns. MX/NS/SOA: still SRE only.

### TXT byte budget

- Per TXT character-string: 255 bytes hard cap (RFC 1035). Multi-string per RR allowed; joined client-side.
- UDP+EDNS0 response budget: ~4096 bytes total. Keep `_dns-mgmt.<zone>` response under ~800 bytes to avoid TCP fallback + middlebox issues.
- Per-owner RR avg: ~110 bytes (`heritage=wfinfra-v1 o=multisite p=www,admin,_acme-challenge.www,_acme-challenge.admin,tour.*`). 800-byte budget → 5-6 owners comfortably.
- Compression strategies:
  1. **Short keys** (`o`, `p`, `t`, `d`) instead of full names — saves ~30 bytes/RR.
  2. **Pattern shorthands**: `_acme:X` → expands to `_acme-challenge.X` (saves ~10 bytes per ACME record).
  3. **Wildcard collapsing**: if patterns are `www,_acme-challenge.www`, allow `_with-acme:www` (planned v2).
  4. **Per-owner RR split**: if one owner exceeds budget, multiple RRs allowed; parser unions patterns.
- Hard cap: parser rejects single-owner policies that don't fit in 4× 255-byte RRs (~1020 bytes per owner). If you have one owner that needs > 1KB of patterns, restructure.

### Schema versioning

`heritage=wfinfra-v1` carries the version. Future bumps (`-v2`) allow breaking schema changes; clients must read both versions during transition. Parser ignores RRs with unknown heritage (forward-compat).

### Where the gate lives

`workflow-plugin-infra` exports a Go package `pkg/dnspolicy` (Go module path `github.com/GoCodeAlone/workflow-plugin-infra/pkg/dnspolicy`) with:

```go
package dnspolicy

// Policy holds parsed ownership claims for a zone.
type Policy struct {
    Zone     string
    Entries  []Entry // one per parsed _dns-mgmt RR
}

// Entry is a single owner's claim.
type Entry struct {
    Owner    string
    Patterns []string
    Types    []string // empty = all types
    Default  bool
}

// Parse parses one or more TXT RR strings (already concatenated per RR
// by the resolver) into a Policy.
func Parse(zone string, txtRRs []string) (*Policy, error)

// Serialize emits a Policy as a slice of TXT RR strings ready to write.
func Serialize(p *Policy) ([]string, error)

// CheckAllowed returns nil if owner may mutate (name, recordType) under
// the policy; otherwise returns an error explaining the denial (and
// which other owner, if any, holds the conflicting claim).
func (p *Policy) CheckAllowed(name, recordType, owner string) error
```

Plus a gate function invoked by IaC steps:

```go
package gate

// Gate is the policy-check entry point for IaC mutation steps.
// Invoked by every infra.dns_record step in workflow-plugin-infra
// before the actual provider-specific upsert/delete fires.
func Gate(ctx context.Context, provider DNSProvider, zone, name, recordType, owner string) error
```

`provider` abstracts the DNS provider read (libdns interface). The gate:
1. Calls `provider.GetTXT(ctx, "_dns-mgmt."+zone)` to fetch policy RRs.
2. Calls `Parse` to build the policy.
3. Calls `CheckAllowed` against `(name, recordType, owner)`.
4. Returns nil or a structured denial error.

### Owner identity

The `owner` field passed to `Gate` is the calling IaC module's identity. Modeling:
- For multisite host: `owner=multisite`
- For BMW: `owner=bmw`
- For SRE-direct apply (e.g. one-off `wfctl plugin infra dns upsert`): `owner=sre` (or whatever `--as-owner` flag value)
- Owner identity is config-supplied by the calling module, NOT auto-detected. This is a trust boundary; the policy gate trusts the owner string the IaC step provides. The actual auth/credential check happens at the DNS provider level (a provider API token belongs to one party).

### Apex policy bootstrap

For zones that don't yet have a `_dns-mgmt` TXT record, the gate defaults to **fail-closed** for non-SRE owners and **allow-with-warning** for SRE. (SRE setting up a new zone needs to be able to write the first `_dns-mgmt` record without circular-dependency lockout.) The "SRE" identity check is opt-in: callers pass `owner: "sre"` knowingly.

Alternative: fail-closed unconditionally, require `wfctl plugin infra dns bootstrap-policy <zone> --owner sre` before any other mutation. Cleaner trust model but adds friction. **Decision**: fail-closed unconditionally; provide a clean bootstrap command. Per repo owner: prefer correctness over convenience.

### Policy mirror in `gocodealone-dns`

`gocodealone-dns/ownership/<zone>.yaml` MIRRORS the live `_dns-mgmt` TXT for human review. The import workflow:
1. Fetches `_dns-mgmt.<zone>` TXT per zone.
2. Parses via `pkg/dnspolicy`.
3. Writes the parsed structure to `ownership/<zone>.yaml`.
4. Drift between yaml and live TXT → import script flags it; SRE reconciles via `wfctl plugin infra dns set-policy <zone> -f ownership/<zone>.yaml`.

### Multi-provider abstraction

`pkg/dnspolicy` doesn't talk to providers directly. It accepts a `DNSProvider` interface that callers implement (or wire via libdns adapters):

```go
type DNSProvider interface {
    GetTXT(ctx context.Context, name string) ([]string, error)
    UpsertTXT(ctx context.Context, name string, values []string, ttl int) error
}
```

Provider plugins (`workflow-plugin-digitalocean`, `workflow-plugin-cloudflare`, etc.) provide concrete implementations. `pkg/dnspolicy` stays provider-agnostic.

## Assumptions

- TXT records at `_dns-mgmt.<zone>` will not be hijacked by other tooling (the `wfinfra-v1` heritage sentinel + scoped name makes accidental collision unlikely).
- DNS providers we support allow TXT records at arbitrary names under a zone (true for all major providers — DO, Cloudflare, Hover, Namecheap, R53, GCP, Azure, GoDaddy).
- Owner identity passed to the gate is honest. (Trust boundary — see "Owner identity" above.)
- `_dns-mgmt` is a fresh DNS label not registered with IANA. Risk: future IETF standardization conflict. Mitigation: heritage sentinel protects against parser confusion; if IANA conflict emerges, version bump (`wfinfra-v2`) can migrate to a new label.
- Policy RR TTL is short (60s) so policy changes propagate quickly. SRE-supplied; not enforced by parser.

## Rollback

- Revert PR + delete `_dns-mgmt` TXT records via provider. Pre-rollback systems didn't enforce the gate → revert restores that state.
- Schema version bump path (`-v1` → `-v2`) allows in-place migration with dual-read during transition; no rollback needed for schema changes.
- Per-task rollback noted in implementation plan.

## Open questions (deferred to plan/execute phases)

1. Pattern syntax: `*` = single segment vs multi-segment? Pin to single-segment `*`, multi-segment `**` (mimics shell globs). Confirm.
2. Wildcard policy gotcha: `*.example.com` (wildcard DNS record) interaction with patterns. Document explicitly.
3. Caching: how long does the gate cache parsed policy in a single `wfctl apply` invocation? In-memory for the call duration; no cross-invocation persistent cache (safer for SRE policy edits to take effect immediately).
4. Tooling for "show me what owner X may touch in zone Y" — `wfctl plugin infra dns policy show <zone>` reads + pretty-prints. Schedule in plan.
5. Test fixtures: how do we mock the DNSProvider in unit tests? Standard fake/stub pattern; trivial.

## Related issues

- workflow#779 — cross-driver IaC ownership-tagging convention (Phase 2 of gocodealone-dns import). This design IS Phase 2 for the DNS resource class.
- gocodealone-dns PR #1 — initial DO DNS state import.
- gocodealone-multisite SPEC §C15 — plugin remains general-purpose; this host is one consumer (binds the owner identity model).
