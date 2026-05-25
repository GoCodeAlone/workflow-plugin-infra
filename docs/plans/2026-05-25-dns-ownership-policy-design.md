# DNS ownership policy gate — design

Per-record DNS ownership marker + policy enforcement gate. Lives in `workflow-plugin-infra` (the IaC layer for shared infra resources). Fired by `wfctl apply` before DNS mutations land.

## Problem

Shared DNS zones have multiple stakeholders:
- **SRE/owner** holds the zone (apex, MX, SPF/DMARC, NS records). Manages most records.
- **Applications** (multisite, BMW, ratchet) need to provision specific records (subdomains, ACME challenges, etc.) without SRE intervention at deploy time.
- SRE must NOT undo app records; apps must NOT touch records outside their scope.

Zone-level "managed-by" is too coarse. Per-record sidecar TXT records (ExternalDNS pattern) is too verbose and pollutes the zone. Need pattern-based ownership claims at the zone root, machine-parseable, DNS-native.

## Goal

A single TXT-record-based policy at `_workflow-dns-policy.<zone>` declares which named owners may manage which record-name patterns. The policy gate validates every DNS mutation against the policy before it lands.

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
| libdns | Provider-neutral Go interfaces | **Adopt as the provider abstraction**. Concrete libdns/digitalocean, libdns/cloudflare adapters exist for the providers we need. |
| miekg/dns | Low-level DNS RR parser | **Adopt for wire-level TXT parsing** — handles 255-byte chunking via `[]string`. |
| RFC 1464 | `key=value` TXT format with backquote escaping | **Adopt as syntactic model** — informational status. |
| RFC 8552 | `_underscore` label convention for scoped DNS attributes | **Cite as justification** for `_workflow-dns-policy` naming. Tool-scoped label minimizes collision risk vs. generic `_dns-mgmt`. |
| RFC 8659 (CAA) | Multiple RRs at one name, each granting a party a capability | **Structural analogy** — directly inspires "one RR per owner at the policy name" approach. |
| draft-ietf-dnsop-domain-verification-techniques-06 | `_<provider>-challenge.<domain>` convention | Cite for `_<scope>` naming legitimacy. |

**No existing OSS library solves this exact problem.** ExternalDNS comes closest but per-record companion model is too verbose. Write own parser (~80-120 lines), mimic ExternalDNS's `labels.go` API for least Go-developer surprise.

## Design

### TXT record schema

At `_workflow-dns-policy.<zone>`, declare ownership policy via multiple TXT RRs — one RR per owner. Each RR carries one owner's `heritage`, `owner` name, and pattern list. CAA-style: multiple RRs co-exist, each grants a capability to a named party.

**Why tool-scoped label `_workflow-dns-policy`** (revised from `_dns-mgmt` per adversarial I-1): the label explicitly identifies the consuming tool, reducing collision risk with future IETF or unrelated OSS. RFC 8552 allows tool-scoped underscore labels.

**Encoding** (RFC 1464 inspired, ExternalDNS heritage prefix):

```
heritage=wfinfra-v1 o=<owner> p=<pattern-csv> [t=<rtype-csv>] [d=true|false]
```

Field reference:

| Key | Required | Meaning |
|---|---|---|
| `heritage` | yes | Always `wfinfra-v1`. Distinguishes our policy RRs from unrelated TXT records (SPF, DMARC, Google verification, etc.). Schema version baked in (`-v1`) for forward-compat. |
| `o=<owner>` | yes | Canonical owner name. Short identifiers (e.g. `sre`, `multisite`, `bmw`, `ratchet`). Pattern: `[a-z0-9_-]{2,32}`. |
| `p=<pattern-csv>` | yes | Comma-separated record-name patterns this owner manages. Pattern syntax: `*` matches a single DNS label segment, `**` matches multiple segments, `@` matches the apex. Examples: `www`, `admin`, `tour.*`, `_acme-challenge.*`. **Locked at v1** (was deferred; closed per adversarial m-4). |
| `t=<rtype-csv>` | optional | Record-type scoping. Default: all types except `SOA` and `NS` (always SRE-only). Example: `t=A,AAAA,CNAME` restricts owner to those types. |
| `d=true` | optional | Default-owner flag. **Exactly one RR per zone MAY set `d=true`; multiple → parse error.** Owner with `d=true` claims any record not matched by another owner's patterns. **Zero `d=true` RRs**: records matching no pattern fail-closed (no implicit default). Both behaviors are explicit at parse time (closes adversarial I-5). |

**Why short keys (`o=`, `p=`, `t=`, `d=`)**: TXT-string-byte conservation. See "TXT byte budget" below.

**No ACME shorthand**: dropped per adversarial m-2 (YAGNI; 10 bytes saved doesn't justify ACME-specific parser path; future RFC churn risk).

**Multi-string handling**: each TXT RR is split into ≤255-byte strings at the DNS wire layer (`miekg/dns` does this transparently). Parser receives the joined string. Pattern-list growth is the main bloat vector; if a single owner's patterns exceed ~200 bytes of CSV, split into multiple RRs for the same owner (the parser unions them).

### Examples

#### Simple case (gocodealone.tech, SRE + multisite)

```
_workflow-dns-policy.gocodealone.tech. 60 IN TXT "heritage=wfinfra-v1 o=sre d=true"
_workflow-dns-policy.gocodealone.tech. 60 IN TXT "heritage=wfinfra-v1 o=multisite p=www,admin,_acme-challenge.www,_acme-challenge.admin"
```

Result:
- SRE owns the apex + MX + everything not matched below (catch-all via `d=true`)
- multisite owns 4 specific records
- bandname.gocodealone.tech apply by multisite → fail (no match)
- bandname.gocodealone.tech apply by SRE → pass (default)

#### Pattern + type scoping (BMW)

```
_workflow-dns-policy.buymywishlist.com. 60 IN TXT "heritage=wfinfra-v1 o=sre d=true"
_workflow-dns-policy.buymywishlist.com. 60 IN TXT "heritage=wfinfra-v1 o=bmw p=app,api,_acme-challenge.* t=A,AAAA,CNAME,TXT"
```

bmw may upsert A/AAAA/CNAME/TXT records matching the listed patterns. MX/NS/SOA: still SRE only.

### TXT byte budget (revised per adversarial m-1)

- Per TXT character-string: 255 bytes hard cap (RFC 1035). Multi-string per RR allowed; joined client-side.
- UDP response budget: 512 bytes classic, ~4096 with EDNS0 negotiated. **Realistic working budget: 700 bytes total response** (accounts for DNS header ~12B, question section ~30B per zone name, owner-name in each RR ~26B for `_workflow-dns-policy.gocodealone.tech.` minus wire compression). Some middleboxes/ISP resolvers reject EDNS0.
- Per-owner RR avg: ~110 bytes (`heritage=wfinfra-v1 o=multisite p=www,admin,_acme-challenge.www,_acme-challenge.admin,tour.*`). 700-byte budget → 4-5 owners per zone comfortably (revised down from 5-6).
- Compression strategies:
  1. **Short keys** (`o`, `p`, `t`, `d`) instead of full names — saves ~30 bytes/RR.
  2. **Per-owner RR split**: if one owner exceeds budget, multiple RRs allowed; parser unions patterns.
- **Single-owner cap**: 1020 bytes (4× 255-byte strings in one RR). Single owners requiring >1KB of patterns must restructure.

### Schema versioning

`heritage=wfinfra-v1` carries the version. Future bumps (`-v2`) allow breaking schema changes; clients must read both versions during transition. Parser ignores RRs with unknown heritage (forward-compat).

### Where the gate lives + how it wires

`workflow-plugin-infra` exports a Go package at `internal/dnspolicy` (revised from `pkg/` per adversarial m-3 — keep internal until concrete external need).

```go
package dnspolicy

// Policy holds parsed ownership claims for a zone.
type Policy struct {
    Zone     string
    Entries  []Entry // one per parsed _workflow-dns-policy RR
}

type Entry struct {
    Owner    string
    Patterns []string
    Types    []string // empty = all types except SOA/NS
    Default  bool
}

// Parse parses one or more TXT RR strings into a Policy.
func Parse(zone string, txtRRs []string) (*Policy, error)

// Serialize emits a Policy as a slice of TXT RR strings ready to write.
func Serialize(p *Policy) ([]string, error)

// CheckAllowed returns nil if owner may mutate (name, recordType) under
// the policy; otherwise returns an error explaining the denial.
func (p *Policy) CheckAllowed(name, recordType, owner string) error
```

### Provider wiring path (revised — closes adversarial C-3)

The original design assumed an `IaCProvider` gRPC delegation chain that doesn't exist in workflow-plugin-infra yet (`internal/plugin.go:193` has a stub). **Revised approach**: use `libdns` directly. Each infra DNS step's config already includes a provider type and credentials. The apply step instantiates a libdns adapter for that provider; the adapter implements a thin `DNSPolicyReader` interface used by the gate.

```go
package dnspolicy

// DNSPolicyReader is the minimal interface the gate needs from a DNS provider.
// Implementations are thin wrappers over libdns/<provider>.
type DNSPolicyReader interface {
    GetTXT(ctx context.Context, name string) ([]string, error)  // read policy RRs
    UpsertTXT(ctx context.Context, name string, values []string, ttl int) error  // write policy (bootstrap path only)
}
```

The libdns ecosystem covers DO (libdns/digitalocean), Cloudflare (libdns/cloudflare), Hover (libdns/hover), Namecheap (libdns/namecheap), R53, GCP, Azure. Each adapter is ~100 lines. workflow-plugin-infra's infra.dns_record step instantiates the libdns provider directly from the step's config secrets. No gRPC delegation chain required.

This removes the dependency on IaCProvider resolution and unblocks the gate immediately. (Future: if/when IaCProvider gRPC is built out, the gate can switch to it; the `DNSPolicyReader` interface stays stable.)

### Owner identity + trust model (revised — closes adversarial C-2 + I-3)

**Owner identity at call time**: a new `owner` field is added to the infra.dns_record step's config (not the proto contract — config-level only). Example:

```yaml
- type: infra.dns_record
  config:
    zone: gocodealone.tech
    owner: multisite          # NEW — declares calling owner identity
    name: www
    record_type: A
    data: 1.2.3.4
    provider: digitalocean
    provider_token: $secret.do_token
```

The owner string is **caller-supplied and unverifiable by the gate alone**. This is an accepted v1 risk; the mitigation is the credential trust boundary:

**Credential trust boundary**: each owner uses a different DNS provider API token (e.g., SRE's DO token has full zone access; multisite's DO token is restricted to specific records via DO's API token scoping where available). A malicious or buggy IaC config in multisite's pipeline can declare `owner=sre`, but the actual DNS write will fail at the provider auth layer because multisite's token can't write outside its allowed scope. The policy gate provides:
- **Pre-flight detection** of pattern violations (clearer error than a provider 403 mid-apply)
- **Defense in depth**: catches accidental config bugs before they hit the provider
- **Audit trail**: every gate denial is logged with owner+zone+name+type

**v1 limitation**: provider-side scoped tokens are only available for some providers (DO partial, Cloudflare yes, R53 yes via IAM). For providers without scoping, the gate is the only barrier and impersonation IS possible.

**v2 path** (deferred): bind owner identity to provider token. Gate calls `provider.WhoAmI()` (libdns extension, not currently in interface) to fetch authenticated identity; matches against `owner` field; fail if mismatch. Requires libdns ecosystem changes.

This addresses adversarial C-2 (owner availability) by adding the config field, and partially addresses I-3 (trust boundary) by documenting the credential-scoping mitigation + scoping v2 fix.

### Bootstrap path (revised — closes adversarial C-1)

The gate's `CheckAllowed` is invoked only by the `infra.dns_record` step. The bootstrap (writing the initial `_workflow-dns-policy` RR) uses a DIFFERENT command: `wfctl plugin infra dns set-policy <zone>`. This command:

1. Does NOT invoke `Gate.CheckAllowed`.
2. Calls `Serialize` to format the policy.
3. Calls `provider.UpsertTXT("_workflow-dns-policy."+zone, serialized, ttl)` directly.
4. Logs an audit entry: `policy-set zone=X by=<caller> owners=[...]`.

There is no circular dependency because the bootstrap command does not flow through the per-record gate. The trust check for bootstrap is the same as for any DNS provider write — token-based. Whoever has the DO token for zone X can write the policy for zone X. This is consistent with the credential-trust-boundary model.

Subsequent policy updates (adding/removing owners) use the same `set-policy` command. The command DOES read the existing policy and emit a diff for review; SRE confirms before write.

For zones that never had a policy: `infra.dns_record` mutations against such zones MUST fail-closed at the gate. The operator's bootstrap workflow is:
1. Run `wfctl plugin infra dns set-policy <zone> -f ownership/<zone>.yaml --as-owner sre`.
2. Confirm the diff (zero existing policy → all-new).
3. Apply.
4. Subsequent mutations from apps work normally.

This is one-time-per-zone setup. No circular dependency.

### Apex policy bootstrap edge case

For the FIRST zone to be policy-managed: SRE runs `set-policy` with `--bootstrap` flag (audit-logged) which writes the initial policy regardless of existing state. This bypasses any "policy must exist" precondition. For zones with a corrupted or partially-written policy: same flag.

### Multi-owner stranded-records recovery (closes adversarial I-2)

When SRE removes an owner from policy (e.g. retiring multisite), the gate enters a defined fallback for that owner's prior records:

1. **Drift detection** (`wfctl plugin infra dns drift <zone>`): records matching the removed owner's prior patterns are flagged as `orphaned-records` in the report — they exist in DNS but no current owner claims them.
2. **Apply behavior**: by default, `infra.dns_record` applies do NOT delete orphaned records (apply is upsert-only by default; explicit `delete: true` requires `--force-orphaned` flag).
3. **Transfer-ownership command**: `wfctl plugin infra dns transfer-records <zone> --from multisite --to sre --records www,admin` — emits a new policy RR with the records added to the target owner's patterns. Audit-logged.

This gives SRE a clean exit path: revoke delegation, then either delete orphaned records explicitly OR transfer them to a new owner.

### Race conditions (closes adversarial I-4, documented limitation)

Mid-flight race: SRE updates policy while app is mid-apply. Result: partial apply, some records gate-approved before policy change, some denied after.

**v1 behavior**: no transactional semantics. Documented as accepted risk. Mitigations:
- Long applies should re-fetch policy periodically (per-step, since policy is fetched per-step anyway).
- SRE policy updates should be announced to ops channels before applying.
- Audit log captures both the policy version at each gate call and the policy at update time; post-hoc reconciliation possible.

**v2 path** (deferred): policy TXT carries a generation counter (`g=<int>` field); gate captures generation at apply start; if generation changes mid-apply, gate fails remaining records with `policy-changed-mid-apply` error.

### Policy mirror in `gocodealone-dns`

`gocodealone-dns/ownership/<zone>.yaml` MIRRORS the live `_workflow-dns-policy` TXT for human review. The import workflow:
1. Fetches `_workflow-dns-policy.<zone>` TXT per zone.
2. Parses via `internal/dnspolicy.Parse`.
3. Writes the parsed structure to `ownership/<zone>.yaml`.
4. Drift between yaml and live TXT → import script flags it; SRE reconciles via `wfctl plugin infra dns set-policy <zone> -f ownership/<zone>.yaml`.

### DNSSEC interaction (closes adversarial m-5)

For zones using managed DNSSEC (DO, Cloudflare, Route53 — all auto-resign on TXT additions), the policy gate's TXT writes are transparent. For self-managed DNSSEC zones, the operator must re-sign after policy changes (`wfctl plugin infra dns set-policy` does NOT trigger re-signing; that's the zone's signing infrastructure responsibility). v1 scope: managed-DNSSEC zones only. Self-managed DNSSEC zone support deferred.

## Assumptions

- TXT records at `_workflow-dns-policy.<zone>` will not be hijacked by other tooling (the `wfinfra-v1` heritage sentinel + tool-scoped name minimizes accidental collision).
- DNS providers we support allow TXT records at arbitrary names under a zone (true for all major providers — DO, Cloudflare, Hover, Namecheap, R53, GCP, Azure, GoDaddy).
- Owner identity is caller-supplied; trust boundary is the provider credential, not the gate (v1 limitation; v2 path defined).
- `_workflow-dns-policy` is a fresh DNS label not registered with IANA or used in the wild. IANA check confirmed unregistered. Mitigation: heritage sentinel protects against parser confusion; if IANA conflict emerges, version bump migrates to a new label.
- Policy RR TTL is short (60s) so policy changes propagate quickly. SRE-supplied; not enforced by parser.
- Zones use managed DNSSEC (auto-resign) OR no DNSSEC.

## Rollback

- Revert PR + delete `_workflow-dns-policy` TXT records via provider. Pre-rollback systems didn't enforce the gate → revert restores that state. Apps can write any records (no gate). SRE direct edits return to baseline.
- Schema version bump path (`-v1` → `-v2`) allows in-place migration with dual-read during transition; no rollback needed for schema changes.
- Per-task rollback noted in implementation plan.

## Open questions (deferred to plan/execute phases — addressed at design time per adversarial findings)

1. ~~Pattern syntax `*` vs `**`~~ — RESOLVED: `*` single label, `**` multi-segment, `@` apex. Locked at v1.
2. ~~Wildcard policy gotcha (`*.example.com` interaction)~~ — RESOLVED: wildcard DNS records (`*.zone TXT ...`) are records like any other; if `*` is in a delegated pattern, the owner may manage them. Tests cover this case.
3. ~~Caching~~ — RESOLVED: in-memory per `wfctl apply` invocation. ~100 DNS mutations = 1 GetTXT per zone (not 100; cache key is zone). Acknowledged as acceptable load.
4. Tooling: `wfctl plugin infra dns policy show <zone>` reads + pretty-prints. Implementation plan tasks include.
5. Test fixtures: mock `DNSPolicyReader` via Go fakes. Trivial.

## Adversarial cycle 1 findings — resolutions inline

| Finding | Resolution |
|---|---|
| C-1 Bootstrap circular dep | `set-policy` command bypasses Gate (different code path). Token-based trust suffices. |
| C-2 No `owner` field in DNSConfig | Added `owner` config field at YAML config layer (not proto-level — additive, non-breaking). |
| C-3 IaCProvider gRPC chain unimplemented | Use libdns directly via thin `DNSPolicyReader` interface. Skip the gRPC delegation. |
| I-1 Heritage collision | Renamed label `_dns-mgmt` → `_workflow-dns-policy` (tool-scoped). Heritage sentinel preserved. |
| I-2 Stranded records on owner removal | Added `transfer-records` command + `--force-orphaned` flag + drift detection of orphans. |
| I-3 Owner trust | Documented credential-trust-boundary mitigation; v2 path defined (provider WhoAmI). |
| I-4 Race conditions | Documented as accepted v1 risk; v2 path (generation counter) defined. |
| I-5 `d=true` ambiguity | Defined: multiple → parse error; zero → fail-closed for unmatched. |
| m-1 EDNS0 budget optimism | Revised to 700-byte working budget; 4-5 owners per zone (not 5-6). |
| m-2 ACME shorthand | Dropped per YAGNI. |
| m-3 `pkg/` public surface | Moved to `internal/dnspolicy`. |
| m-4 Pattern syntax deferred | Locked at v1: `*` single label, `**` multi-segment, `@` apex. |
| m-5 DNSSEC | Documented; v1 scope = managed-DNSSEC zones only. |

## Related issues

- workflow#779 — cross-driver IaC ownership-tagging convention (Phase 2 of gocodealone-dns import). This design IS Phase 2 for the DNS resource class.
- gocodealone-dns PR #1 — initial DO DNS state import.
- gocodealone-multisite SPEC §C15 — plugin remains general-purpose; this host is one consumer (binds the owner identity model).
