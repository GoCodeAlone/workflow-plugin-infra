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
| `d=true` | optional | Default-owner flag. **Exactly one RR per zone MAY set `d=true`; multiple → parse error AND serialize error.** Both validation paths (parse + serialize) prevent invalid policies from reaching DNS (closes adversarial I-5 + cycle-2 m-3 + cycle-3 I-4). Sentinel errors defined in `internal/dnspolicy/errors.go`:<br/>`var ErrMultipleDefaults = errors.New("dnspolicy: multiple RRs set d=true")`<br/>`var ErrUnknownHeritage = errors.New("dnspolicy: unknown heritage value")`<br/>`var ErrEmptyOwner = errors.New("dnspolicy: o= field is empty")`<br/>Callers use `errors.Is(err, ErrMultipleDefaults)` to switch behavior. Owner with `d=true` claims any record not matched by another owner's patterns. **Zero `d=true` RRs**: records matching no pattern fail-closed (no implicit default). |

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

### Provider wiring path (revised — closes adversarial cycle-1 C-3 + cycle-2 I-1/I-2)

The original cycle-1 design assumed an `IaCProvider` gRPC delegation chain that doesn't exist (`internal/plugin.go:193` has a stub). Cycle-2 design proposed direct libdns import in the gate package, which couples gate to provider libraries.

**Cycle-3 revised approach**: introduce a SEPARATE `internal/dnsprovider/` package that owns the libdns boundary. `internal/dnsgate/` and `internal/dnspolicy/` depend only on a small interface, not on libdns:

```go
// internal/dnspolicy/types.go
type DNSPolicyReader interface {
    GetTXT(ctx context.Context, name string) ([]string, error)  // read policy RRs
    UpsertTXT(ctx context.Context, name string, values []string, ttl int) error  // write policy (bootstrap path only)
}

// internal/dnsprovider/adapter.go (THIS is the libdns boundary)
package dnsprovider

import (
    "github.com/libdns/digitalocean"
    "github.com/libdns/cloudflare"
    // hover NOT in libdns — use workflow-plugin-hover gRPC instead
    // see "Provider coverage matrix" below
)

func NewAdapter(provider, token string) (dnspolicy.DNSPolicyReader, error) { ... }
```

**Provider coverage matrix** (revised honestly per cycle-2 I-1):

| Provider | Adapter source | Status |
|---|---|---|
| DigitalOcean | `libdns/digitalocean` | ✓ exists |
| Cloudflare | `libdns/cloudflare` | ✓ exists |
| Namecheap | `libdns/namecheap` | ✓ exists |
| Route53 (AWS) | `libdns/route53` | ✓ exists |
| GCP Cloud DNS | `libdns/googleclouddns` | ✓ exists |
| Azure DNS | `libdns/azure` | ✓ exists |
| **Hover** | NO libdns adapter | ✗ — embed lightweight `hover.Client` HTTP calls directly in `internal/dnsprovider/hover.go`. The client (~80 lines) currently lives inside workflow-plugin-hover's internal package and supports TXT records via its `Type` field. Either (a) extract to `github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient` (preferred for reuse) and import here, or (b) copy the ~80 lines into this plugin (acceptable for v1 if extraction is too disruptive). v1 starts with option (b); option (a) is a follow-up cleanup. (Closes cycle-3 I-1/I-3 — gRPC wrap was architecturally wrong; Hover plugin is an IaCProvider server, not a TXT read/write API.) |
| GoDaddy | `libdns/godaddy` | ✓ exists |

Hover gap: `workflow-plugin-hover` already implements DNS read/write against Hover's account UI (no API). For v1, wrap its existing gRPC interface into the `DNSPolicyReader` interface within `internal/dnsprovider/hover.go`. No new Hover dependency needed.

**libdns dependency burden** (closes cycle-2 I-2): each libdns adapter is its own Go module. Isolating libdns imports to `internal/dnsprovider/` means:
- API breakage in any libdns adapter only requires touching one file
- Gate package (`internal/dnsgate/`) and policy package (`internal/dnspolicy/`) stay test-isolated with fake `DNSPolicyReader` implementations
- Adding a new provider = adding one file under `internal/dnsprovider/`
- Adapter packages can be loaded conditionally via build tags if dependency surface gets large enough to warrant (not v1 scope; flag as future option)

This removes the dependency on IaCProvider resolution and unblocks the gate immediately. (Future: if/when IaCProvider gRPC is built out, swap `dnsprovider` for an IaCProvider-based adapter; the `DNSPolicyReader` interface stays stable.)

### Gate invocation site: new `infra.dns_record` STEP type (revised — closes adversarial cycle-2 C-2)

The original cycle-2 design referenced `infra.dns_record` as if it existed — it does not. workflow-plugin-infra currently has only `infra.dns` MODULE (long-lived; with an unimplemented `Start()` stub at plugin.go:193). The gate's natural home is a discrete operation, not a module lifecycle.

**Resolution**: register a NEW step type `infra.dns_record` in workflow-plugin-infra. Step types are additive (no breaking proto change). Define typed step input + output protos.

```protobuf
// internal/contracts/infra.proto — ADDITIVE additions

// Static per-step config (resolved once at module construction).
message DNSRecordStepConfig {
  string provider           = 1; // "digitalocean" | "cloudflare" | "hover" | ...
  string provider_token_ref = 2; // YAML secret reference (engine resolves)
  string zone               = 3; // e.g. "gocodealone.tech"
}

// Per-execution input (resolved per Execute call).
message DNSRecordStepInput {
  string name        = 1; // e.g. "www" (relative to zone) or "@" for apex
  string record_type = 2; // "A" | "AAAA" | "CNAME" | "TXT" | "MX" | "SRV" | ...
  string data        = 3; // record value (e.g. "1.2.3.4", "alias.target.")
  int32  ttl         = 4; // seconds; 0 = provider default
  int32  priority    = 5; // MX/SRV
  string owner       = 6; // *REQUIRED* — caller's owner identity for gate check
  string operation   = 7; // "upsert" (default) | "delete"
}

// Per-execution output.
message DNSRecordStepOutput {
  string status        = 1; // "ok" | "gate-denied" | "provider-error"
  string record_id     = 2; // provider-assigned ID on upsert
  string denial_reason = 3; // populated when status="gate-denied"
}
```

**3-message split per `sdk.TypedStepFactory[C, I, O]` pattern** (closes adversarial cycle-3 C-1). Matches the `workflow-plugin-platform` proto convention.

`owner` is a typed proto field in the per-execution input — STRICT_PROTO validates it; YAML authors must supply it. Engine config-decode never silently drops it.

Step registration in plugin.go — both `TypedStepProvider` (primary, strict) AND `StepProvider` (legacy fallback) wiring (closes adversarial cycle-3 m-2):
```go
// Untyped path (sdk.StepProvider — never called when typed path is present, but required by interface)
func (p *infraPlugin) StepTypes() []string {
    return []string{"infra.dns_record"}
}
func (p *infraPlugin) CreateStep(typeName, name string, config map[string]any) (sdk.StepInstance, error) {
    return nil, fmt.Errorf("infra.dns_record requires typed config; legacy untyped path not supported")
}

// Typed path (sdk.TypedStepProvider — actual gate fire site)
func (p *infraPlugin) TypedStepTypes() []string {
    return []string{"infra.dns_record"}
}
func (p *infraPlugin) CreateTypedStep(typeName, name string, config *anypb.Any) (sdk.StepInstance, error) {
    factory := sdk.NewTypedStepFactory(typeName,
        &contracts.DNSRecordStepConfig{},
        &contracts.DNSRecordStepInput{},
        // step body: invokes dnsgate.Gate then dnsprovider.Upsert/Delete
    )
    return factory.CreateTypedStep(typeName, name, config)
}
```

### plugin.contracts.json updates (closes adversarial cycle-3 I-2)

Adding the step type requires a matching contract entry so `wfctl plugin validate-contract` recognizes it:

```json
{
  "contracts": [
    // ... existing 13 module entries ...
    {
      "kind": "step",
      "type": "infra.dns_record",
      "mode": "strict",
      "config_descriptor": "workflow.plugins.infra.v1.DNSRecordStepConfig",
      "input_descriptor": "workflow.plugins.infra.v1.DNSRecordStepInput",
      "output_descriptor": "workflow.plugins.infra.v1.DNSRecordStepOutput"
    }
  ]
}
```

### infra.dns deprecation cleanup (closes adversarial cycle-3 m-3)

The existing `infra.dns` MODULE registration in plugin.go is kept for backwards compatibility but its `Start()` returns a non-nil error with a migration hint:

```go
func (m *infraDNSModule) Start(ctx context.Context) error {
    return fmt.Errorf("infra.dns module is deprecated; use the infra.dns_record step type instead. " +
        "See https://github.com/GoCodeAlone/workflow-plugin-infra/blob/master/docs/migration/infra-dns-to-step.md")
}
```

YAML authors writing `type: infra.dns` get an immediate clear failure instead of a silent no-op. A migration doc accompanies the change.

The step's `Execute()` method is the gate fire site:
1. Validate input (owner non-empty, zone/name/record_type valid).
2. Resolve `provider_token_ref` → secret.
3. Instantiate `DNSPolicyReader` via `internal/dnsprovider.NewAdapter(provider, token)`.
4. Call `Gate(ctx, reader, zone, name, record_type, owner)` from `internal/dnsgate`.
5. On gate pass: instantiate full provider client via libdns (or workflow-plugin-hover for Hover), perform upsert/delete.
6. Return typed output.

The previously-existing `infra.dns` MODULE remains untouched. Its `Start()` stub will be filled in or removed in a separate effort outside this design's scope.

### Owner identity + trust model (revised — closes adversarial cycle-1 I-3 + cycle-2 C-1)

**Owner identity at call time**: typed `owner` field in `DNSRecordStepInput` (see above). STRICT_PROTO enforced. Cannot be silently absent.

The owner string is **caller-supplied and unverifiable by the gate alone**. This is an accepted v1 risk; the mitigation is the credential trust boundary:

**Credential trust boundary**: each owner uses a different DNS provider API token (e.g., SRE's DO token has full zone access; multisite's DO token is restricted to specific records via DO's API token scoping where available). A malicious or buggy IaC config in multisite's pipeline can declare `owner=sre`, but the actual DNS write will fail at the provider auth layer because multisite's token can't write outside its allowed scope. The policy gate provides:
- **Pre-flight detection** of pattern violations (clearer error than a provider 403 mid-apply)
- **Defense in depth**: catches accidental config bugs before they hit the provider
- **Audit trail**: every gate denial is logged with owner+zone+name+type

**v1 limitation**: provider-side scoped tokens are only available for some providers (DO partial, Cloudflare yes, R53 yes via IAM). For providers without scoping, the gate is the only barrier and impersonation IS possible.

**v2 path** (deferred): bind owner identity to provider token. Gate calls `provider.WhoAmI()` (libdns extension, not currently in interface) to fetch authenticated identity; matches against `owner` field; fail if mismatch. Requires libdns ecosystem changes.

This addresses adversarial C-2 (owner availability) by adding the config field, and partially addresses I-3 (trust boundary) by documenting the credential-scoping mitigation + scoping v2 fix.

### Bootstrap path + admin CLI dispatch (revised — closes adversarial cycle-1 C-1 + cycle-3 C-2)

**Command surface delivery via plugin-binary dispatch** (closes cycle-3 C-2): the design originally said `wfctl plugin infra dns set-policy` — this subcommand path doesn't exist + wfctl has no mechanism for external plugins to add nested subcommands under `wfctl plugin <name>`. Adding it would require core wfctl changes (violates non-goal).

**Resolution**: ship a standalone `cmd/wfctl-infra-dns` binary inside workflow-plugin-infra. wfctl's existing plugin-binary dispatch (`BuildCLIRegistry(defaultPluginCommandDir())` in wfctl main.go) picks up any binary named `wfctl-<name>` in the plugin directory and exposes it as `wfctl <name>`. After `wfctl plugin install workflow-plugin-infra`, the operator gains:

```
wfctl infra-dns set-policy <zone> -f ownership/<zone>.yaml --as-owner sre
wfctl infra-dns set-policy <zone> --bootstrap --overwrite-existing
wfctl infra-dns transfer-ownership <zone> --from multisite --to sre --records www,admin
wfctl infra-dns drift <zone>
wfctl infra-dns policy show <zone>
```

Zero changes to wfctl core. The binary is built by the plugin's goreleaser config (added to existing builds:); `wfctl plugin install` already handles placement.

**Bootstrap flow** (gate-bypass details):
1. Operator runs `wfctl infra-dns set-policy <zone> -f ownership/<zone>.yaml --as-owner sre`.
2. The standalone binary (NOT the `infra.dns_record` step) loads the YAML policy, calls `dnspolicy.Serialize`, then calls `dnsprovider.NewAdapter(provider, token).UpsertTXT("_workflow-dns-policy."+zone, serialized, 60)`.
3. NO gate check — bootstrap is the gate's exception. Trust check is the provider API token (whoever holds the token may write).
4. Audit entry written via `slog` to a `wfctl-infra-dns-audit.jsonl` file in the plugin's data dir (matches existing wfctl logging patterns — confirmed pre-flight; no shared audit infra exists yet).

**For pre-existing policy** (`--bootstrap` overwrite guard, closes cycle-2 m-1): without `--overwrite-existing`, abort with: `error: existing policy detected at _workflow-dns-policy.<zone> (heritage=wfinfra-v1 found in N RRs); re-run with --overwrite-existing to replace (audit-logged)`.

There is no circular dependency: bootstrap binary does not invoke the gate. Subsequent app deploys go through `infra.dns_record` step → `dnsgate.Gate.CheckAllowed` → pass/fail.

### Apex policy bootstrap edge case (revised — closes cycle-2 m-1)

For the FIRST zone to be policy-managed: SRE runs `set-policy` with `--bootstrap` flag (audit-logged) which writes the initial policy regardless of existing state.

**Overwrite guard**: `--bootstrap` requires `--overwrite-existing` if any RR matching the heritage sentinel already exists at `_workflow-dns-policy.<zone>`. Without the second flag, the command aborts with: `error: existing policy detected at _workflow-dns-policy.<zone>; re-run with --overwrite-existing to replace (audit-logged)`. This prevents accidental clobber of an existing policy via mis-invoked bootstrap.

### Multi-owner stranded-records recovery (closes adversarial I-2)

When SRE removes an owner from policy (e.g. retiring multisite), the gate enters a defined fallback for that owner's prior records:

1. **Drift detection** (`wfctl plugin infra dns drift <zone>`): records matching the removed owner's prior patterns are flagged as `orphaned-records` in the report — they exist in DNS but no current owner claims them.
2. **Apply behavior**: by default, `infra.dns_record` applies do NOT delete orphaned records (apply is upsert-only by default; explicit `delete: true` requires `--force-orphaned` flag).
3. **Transfer-ownership command**: `wfctl plugin infra dns transfer-ownership <zone> --from multisite --to sre --records www,admin` — emits a new policy RR with the records added to the target owner's patterns. Audit-logged.

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

## Adversarial cycle 1 + 2 findings — resolutions inline

### Cycle 1 (3 Critical + 5 Important + 5 Minor)

| Finding | Resolution |
|---|---|
| C-1 Bootstrap circular dep | `set-policy` command bypasses Gate (different code path). Token-based trust suffices. |
| C-2 No `owner` field in DNSConfig | **Cycle-3 fix**: typed `owner` field on NEW `DNSRecordStepInput` proto (additive new step type, not modifying existing DNSConfig). STRICT_PROTO validates. |
| C-3 IaCProvider gRPC chain unimplemented | Use libdns via isolated `internal/dnsprovider/` package; gate package stays libdns-free behind `DNSPolicyReader` interface. |
| I-1 Heritage collision | Renamed label `_dns-mgmt` → `_workflow-dns-policy` (tool-scoped). Heritage sentinel preserved. |
| I-2 Stranded records on owner removal | Added `transfer-ownership` command + `--force-orphaned` flag + drift detection of orphans. |
| I-3 Owner trust | Documented credential-trust-boundary mitigation; v2 path defined (provider WhoAmI). |
| I-4 Race conditions | Documented as accepted v1 risk; v2 path (generation counter) defined. |
| I-5 `d=true` ambiguity | Defined: multiple → parse AND serialize error; zero → fail-closed for unmatched. |
| m-1 EDNS0 budget optimism | Revised to 700-byte working budget; 4-5 owners per zone (not 5-6). |
| m-2 ACME shorthand | Dropped per YAGNI. |
| m-3 `pkg/` public surface | Moved to `internal/dnspolicy`. |
| m-4 Pattern syntax deferred | Locked at v1: `*` single label, `**` multi-segment, `@` apex. |
| m-5 DNSSEC | Documented; v1 scope = managed-DNSSEC zones only. |

### Cycle 2 (2 NEW Critical + 3 Important + 3 Minor — introduced by cycle-1 fixes)

| Finding | Resolution |
|---|---|
| **C-1 NEW** STRICT_PROTO rejects unknown root YAML keys; "config-level only owner" is wrong | Replaced with typed `owner` field in NEW `DNSRecordStepInput` proto. STRICT_PROTO validates; no silent drop. |
| **C-2 NEW** `infra.dns_record` step type does not exist | Design now EXPLICITLY registers new step type in plugin.go. Lives separate from existing `infra.dns` module (which is untouched). |
| I-1 NEW libdns/hover doesn't exist | Added explicit provider coverage matrix; Hover uses existing workflow-plugin-hover gRPC plugin as adapter (no libdns). |
| I-2 NEW libdns module burden not acknowledged | Added `internal/dnsprovider/` package that isolates libdns boundary; gate/policy packages stay libdns-free. |
| I-3 NEW C-3 fix regression (step/module conflation) | Resolved by C-2 NEW fix: explicit step-type registration is the gate fire site. |
| m-1 `--bootstrap` overwrite guard missing | Added `--overwrite-existing` requirement when heritage-sentinel RR detected. Aborts otherwise. |
| m-2 `transfer-records` naming misleading | Renamed to `transfer-ownership` throughout. |
| m-3 `d=true` Serialize() validation missing | `Serialize()` now also validates multiple-default; returns ErrMultipleDefaults at write time before TXT bytes hit DNS. |

### Cycle 3 (2 NEW Critical + 4 Important + 3 Minor — introduced by cycle-2 fixes)

| Finding | Resolution |
|---|---|
| **C-1 NEW** DNSRecordStepInput conflates Config+Input; `sdk.TypedStepFactory[C,I,O]` requires 3 messages | Split into `DNSRecordStepConfig` (static) + `DNSRecordStepInput` (dynamic) + `DNSRecordStepOutput`. Matches `workflow-plugin-platform` precedent. |
| **C-2 NEW** `wfctl plugin infra dns *` subcommands don't exist + no mechanism to add without core change | Ship `cmd/wfctl-infra-dns` binary; wfctl plugin-binary dispatch picks it up as `wfctl infra-dns <subcommand>`. Zero core changes. |
| I-1 NEW Hover wrap-gRPC architecturally unsound (hover plugin is IaC server, not TXT R/W API) | Embed lightweight `hover.Client` HTTP calls directly in `internal/dnsprovider/hover.go`. v1 copies ~80 lines; v2 extracts to `pkg/hoverclient` for reuse. |
| I-2 NEW plugin.contracts.json must declare new step type | Added explicit entry in design: `{"kind":"step","type":"infra.dns_record","mode":"strict",...}`. |
| I-3 NEW Hover IaC interface ≠ DNS TXT API | Folded into I-1 fix above. |
| I-4 NEW `ErrMultipleDefaults` sentinel undefined | Declared explicitly in design: `internal/dnspolicy/errors.go` exports `ErrMultipleDefaults`, `ErrUnknownHeritage`, `ErrEmptyOwner` for `errors.Is()` use. |
| m-1 `--overwrite-existing` flag on non-existent cmd | Resolved by C-2 NEW (cmd now exists as `wfctl infra-dns`). |
| m-2 Step registration snippet incomplete (typed path missing) | Added full TypedStepProvider wiring (CreateTypedStep + TypedStepTypes). |
| m-3 `infra.dns` dead module footgun | Stub now returns non-nil error with migration hint pointing at `docs/migration/infra-dns-to-step.md`. |

## Related issues

- workflow#779 — cross-driver IaC ownership-tagging convention (Phase 2 of gocodealone-dns import). This design IS Phase 2 for the DNS resource class.
- gocodealone-dns PR #1 — initial DO DNS state import.
- gocodealone-multisite SPEC §C15 — plugin remains general-purpose; this host is one consumer (binds the owner identity model).
