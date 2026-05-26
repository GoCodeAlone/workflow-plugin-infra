# DNS provider contract — cross-plugin pattern

**Status:** Draft (cycle 1)
**Author:** codingsloth@pm.me
**Date:** 2026-05-26
**Predecessor:** `docs/plans/2026-05-26-dns-provider-v2-design.md` (in-process libdns adapter pattern)
**Guidance:** `/Users/jon/workspace/docs/design-guidance.md`

## Goal

Replace `workflow-plugin-infra/internal/dnsprovider/` in-process libdns adapter pattern with a plugin-owned strict gRPC contract that peer plugins implement. workflow-plugin-infra becomes the contract owner + dispatcher; cloud-specific DNS handling moves to the respective cloud provider plugins (`workflow-plugin-cloudflare`, `workflow-plugin-namecheap`, `workflow-plugin-digitalocean`, future: aws/azure/gcp/godaddy). Add an account-scoped `ListZones` + `ListRecords` capability to the contract so wfctl operators can enumerate DNS state across all configured providers via a single subcommand.

## Why now

The v1 (DNS ownership policy gate, 2026-05-25) and v2 (multi-provider libdns adapter expansion, 2026-05-26) cascades shipped a `dnspolicy.Adapter` interface with seven concrete implementations sitting inside `workflow-plugin-infra/internal/dnsprovider/`. Each impl imports a `libdns/<provider>` library directly; workflow-plugin-infra carries the union of all libdns deps in its `go.mod`. The pattern violates the proper provider boundary: adding a new provider requires a PR against workflow-plugin-infra, and provider-specific bugs land in the cross-cutting orchestrator repo rather than in the provider's own repo. No external consumer depends on the current shape — admincli is the only caller — so the refactor lands without back-compat shims.

## Global Design Guidance

Source: `/Users/jon/workspace/docs/design-guidance.md`

| guidance | design response |
|---|---|
| wfctl is user-facing CLI; no new bare binaries | `dns import` (+ existing `policy-show`/`set-policy`/`transfer-ownership`/`drift`) ship as workflow-plugin-infra `plugin.json.capabilities.cliCommands[]` |
| Reuse over rebuild | Provider plugins already vendor their cloud SDKs (cloudflare-go, namecheap-go-sdk, godo) for `infra.dns` typed-step factory; new gRPC service is served by the same plugin process using the same SDK |
| Plugin contracts via typed gRPC; no `structpb`/`Any` at wire layer | dnsprovider.proto uses concrete `Zone`/`Record`/`Provider` messages; provider-extras carried as `bytes provider_extras_json` (plugin-owned serialization) |
| libdns/cloud-sdks isolated in `internal/<provider>/` | workflow-plugin-infra strips libdns deps entirely; dispatcher holds only contract types + gRPC client; cloud SDKs live in each provider plugin |
| Plugin Contracts & Extensibility — peer dispatch via EngineCallbackService.GetService + InvokeService | Dispatcher in workflow-plugin-infra looks up provider plugin handles by name at admincli runtime, invokes typed methods via the strict contract |
| Cross-driver parity (≥2 drivers before declaring done) | 3 drivers in scope this work (DO, CF, NC). aws/azure/gcp/godaddy slot in via the same contract in follow-up plans |
| No mock-first development | Provider impls unit-tested with stubbed SDK clients; integration tests gated on live creds via env (`INFRA_DNS_CONTRACT_LIVE=1`) and run on self-hosted runner |
| Secrets never logged | Cred-map values never appear in errors; missing-cred errors name only the key (existing v2 convention preserved) |
| Audit trail for state-mutating ops | Read-only contract methods (ListZones/ListRecords) skip audit; mutating methods (UpsertTXT/UpsertRecord/DeleteRecord) inherit existing dnsaudit append-only JSONL pattern |
| Goreleaser v2 + ldflag Version | All four repos already conform; bumps required for the new release |
| Plugin minEngineVersion + capabilities populated | Each repo declares contract capability in `plugin.json` (see Components §) |

## Architecture

```
                         workflow-plugin-infra
                         ─────────────────────
                         contracts/
                           dnsprovider.proto                 ← plugin-owned strict contract
                           dnsproviderpb/                    ← generated Go stubs
                         internal/
                           dnspolicy/                        ← KEEP (parser, no provider deps)
                           dnsgate/                          ← REWRITE: dispatch via contract
                           dnsaudit/                         ← KEEP
                           admincli/                         ← EXTEND: + dns_import.go
                                                                drift/policy_show/set_policy/transfer_ownership
                                                                rewritten to dispatch via contract
                           dnsprovider/                      ← REWRITE: dispatcher (no impls)
                                                                Lookup(name, host) → Client
                                                                Register(name) on plugin discovery
                                                                DELETE: cloudflare.go, namecheap.go,
                                                                        digitalocean.go, azure.go,
                                                                        godaddy.go, googleclouddns.go,
                                                                        route53.go
                                                                DROP: libdns/* go.mod deps
                         plugin.json
                           capabilities.cliCommands += {dns import,…}
                           capabilities.contracts   = ["DNSProvider"]   (owner advertisement)

         workflow-plugin-cloudflare      workflow-plugin-namecheap      workflow-plugin-digitalocean
         ──────────────────────────      ─────────────────────────      ────────────────────────────
         internal/dnsprovider_server.go  internal/dnsprovider_server.go internal/dnsprovider_server.go
           uses cloudflare-go/v7           uses go-namecheap-sdk          uses godo
           serves DNSProvider              serves DNSProvider             serves DNSProvider
         ContractRegistry registers      ContractRegistry registers     ContractRegistry registers
           DNSProvider impl                DNSProvider impl               DNSProvider impl
         plugin.json                     plugin.json                    plugin.json
           capabilities.contracts +=       capabilities.contracts +=      capabilities.contracts +=
             {service:"DNSProvider",         {service:"DNSProvider",        {service:"DNSProvider",
              source:"workflow-plugin-       source:"workflow-plugin-       source:"workflow-plugin-
                      infra"}                        infra"}                        infra"}
```

Dispatch flow when an admincli command runs (e.g., `wfctl plugin infra dns import --provider cloudflare`):

1. wfctl host loads workflow-plugin-infra and every other plugin in the host registry
2. admincli command invokes `dnsprovider.Lookup("cloudflare")` inside workflow-plugin-infra
3. Dispatcher calls `EngineCallbackService.GetService("workflow.plugin.dnsprovider.cloudflare")` on the host
4. Host returns a service handle (or NotFound — surfaces as `dnsprovider: provider not loaded`)
5. Dispatcher invokes typed methods (`ListZones`, `ListRecords`, `GetTXT`, etc.) via `InvokeService(handle, method, typed_input)`
6. Plugin server in workflow-plugin-cloudflare dispatches to its `dnsprovider_server.go` which calls into cloudflare-go and returns typed output

## Components

### 1. dnsprovider.proto (new, workflow-plugin-infra/contracts/)

```proto
syntax = "proto3";
package workflow.plugin.dnsprovider.v1;
option go_package =
  "github.com/GoCodeAlone/workflow-plugin-infra/contracts/dnsproviderpb";

import "google/protobuf/timestamp.proto";

service DNSProvider {
  rpc ListZones(ListZonesRequest)     returns (ListZonesResponse);
  rpc ListRecords(ListRecordsRequest) returns (ListRecordsResponse);

  rpc GetTXT(GetTXTRequest)             returns (GetTXTResponse);
  rpc UpsertTXT(UpsertTXTRequest)       returns (UpsertTXTResponse);
  rpc UpsertRecord(UpsertRecordRequest) returns (UpsertRecordResponse);
  rpc DeleteRecord(DeleteRecordRequest) returns (DeleteRecordResponse);
}

message Provider { string name = 1; }

message ListZonesRequest  { Provider provider = 1; }
message ListZonesResponse { repeated Zone zones = 1; }

message Zone {
  string id   = 1;   // provider-native zone identifier
  string name = 2;   // FQDN (no trailing dot)
  int64  created_unix = 3;
  bytes  provider_extras_json = 9;  // opaque plugin-owned shape
}

message ListRecordsRequest  {
  Provider provider = 1;
  string   zone     = 2;
}
message ListRecordsResponse { repeated Record records = 1; }

message Record {
  string id   = 1;
  string type = 2;       // A, AAAA, CNAME, MX, TXT, SRV, CAA, NS, …
  string name = 3;       // relative (use "@" for apex)
  string data = 4;
  int32  ttl  = 5;
  int32  priority = 6;   // MX/SRV; 0 when N/A
  bytes  provider_extras_json = 9; // CF proxied, NC email_type, DO weight/port/flags/tag, etc.
}

message GetTXTRequest   { Provider provider = 1; string name = 2; }
message GetTXTResponse  { repeated string values = 1; }

message UpsertTXTRequest {
  Provider provider = 1;
  string   name     = 2;
  repeated string values = 3;
  int32    ttl      = 4;
}
message UpsertTXTResponse { }

message UpsertRecordRequest {
  Provider provider = 1;
  string   zone     = 2;
  Record   record   = 3;
}
message UpsertRecordResponse { string record_id = 1; }

message DeleteRecordRequest {
  Provider provider = 1;
  string   zone     = 2;
  string   name     = 3;
  string   type     = 4;
}
message DeleteRecordResponse { }
```

Hard invariants (mirror iac.proto §):
- No `structpb.Struct`/`Any` types anywhere. Provider-specific extras travel as `bytes provider_extras_json` — plugin owns shape.
- Every method REQUIRED for any plugin advertising `DNSProvider`. SDK type-assert compile-enforces full impl.
- Provider-name normalization (lower-case, trim) happens dispatcher-side before lookup; plugins receive the normalized name unchanged.

### 2. workflow-plugin-infra/internal/dnsprovider/ rewrite

```go
package dnsprovider

type Client interface {
    ListZones(ctx context.Context) ([]Zone, error)
    ListRecords(ctx context.Context, zone string) ([]Record, error)
    // mirrors of the proto service for in-process use
    GetTXT(ctx context.Context, name string) ([]string, error)
    UpsertTXT(ctx context.Context, name string, values []string, ttl int) error
    UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error)
    DeleteRecord(ctx context.Context, zone, name, recordType string) error
}

// Lookup returns a Client that dispatches to the named provider plugin.
// Returns ErrProviderNotLoaded if no plugin registered the contract for `name`.
func Lookup(ctx context.Context, host EngineCallback, name string) (Client, error) { … }
```

Existing call-sites in `admincli/{drift,policy_show,set_policy,transfer_ownership}.go` swap from `NewAdapter(provider, creds)` → `Lookup(ctx, host, provider)`. Cred maps move from "passed to in-process adapter" → "provider plugin reads from its own initialized cred-key map at module init"; the dispatcher does not forward creds.

Files DELETED:
```
internal/dnsprovider/cloudflare.go
internal/dnsprovider/cloudflare_test.go
internal/dnsprovider/namecheap.go
internal/dnsprovider/namecheap_test.go
internal/dnsprovider/digitalocean.go
internal/dnsprovider/digitalocean_test.go
internal/dnsprovider/azure.go
internal/dnsprovider/azure_test.go
internal/dnsprovider/godaddy.go
internal/dnsprovider/godaddy_test.go
internal/dnsprovider/googleclouddns.go
internal/dnsprovider/googleclouddns_test.go
internal/dnsprovider/route53.go
internal/dnsprovider/route53_test.go
internal/dnsprovider/expand.go      (creds-map expansion now provider-side)
```

go.mod DROPS:
```
github.com/libdns/libdns
github.com/libdns/cloudflare
github.com/libdns/namecheap
github.com/libdns/digitalocean
github.com/libdns/route53
github.com/libdns/googleclouddns
github.com/libdns/azure
github.com/GoCodeAlone/workflow-plugin-hover (Hover client was extracted as pkg/hoverclient last session)
```

### 3. admincli/dns_import.go (new)

```
wfctl plugin infra dns import [--provider <name>|--all] [--zones-filter <glob>] \
                              [--out <dir>] [--format json|yaml] [--dry-run]
```

Behavior:
- `--provider X`: enumerate one provider. `--all` (default): iterate every plugin that advertises `DNSProvider` capability.
- For each provider: `ListZones` → for each zone `ListRecords` → emit per-zone artifact to `<out>/<provider>/<zone>/state.json` (workflow `ResourceState` shape; `Type=infra.dns`; `Provider=<name>`; `ProviderID=<zone.id>`; `AppliedConfig.records=[…]`; `AppliedConfig.provider_extras=<map>`; `Outputs.zone_id=<id>`).
- `--dry-run`: stdout only; no file writes.
- Exit code: 0 if ≥1 provider succeeded; non-zero only when no provider returned data and ≥1 was attempted.
- Per-provider failure isolated: log + continue; failed providers listed in final summary.
- Per-zone failure isolated: log + record `import_status: error` in zone metadata; do not overwrite stale state.json.

### 4. Provider plugin servers (workflow-plugin-{cloudflare,namecheap,digitalocean})

Each plugin's `internal/dnsprovider_server.go`:
- Implements `dnsproviderpb.DNSProviderServer` (generated stub from workflow-plugin-infra/contracts/).
- Registers via the host's module-init pathway: at plugin start, calls the SDK to register a service named `workflow.plugin.dnsprovider.<provider>` (e.g., `workflow.plugin.dnsprovider.cloudflare`).
- Reads creds from the plugin's existing module config block (already in `plugin.json` — same `iac.provider.<name>` config the IaC driver consumes).
- Uses the plugin's native cloud SDK (cloudflare-go, namecheap-go-sdk, godo) for all RPCs.

`plugin.json` capability addition (each provider plugin):
```json
{
  "capabilities": {
    "contracts": [
      { "service": "DNSProvider",
        "source":  "workflow-plugin-infra",
        "service_name": "workflow.plugin.dnsprovider.<provider>" }
    ]
  }
}
```

### 5. ContractRegistry advertisement

Each provider plugin's `ContractRegistry()` returns descriptors for the `DNSProvider` service methods (one `ContractDescriptor` per method with `service_name`, `method`, `input_message`, `output_message`) plus the file descriptor set built from `workflow-plugin-infra/contracts/dnsprovider.proto`. workflow-plugin-infra's dispatcher queries `ContractRegistry` at handle-open to verify schema parity before dispatching.

## Data flow

```
admincli command (in workflow-plugin-infra)
    ↓
dnsprovider.Lookup(ctx, host, "<normalized provider>")
    ↓
EngineCallbackService.GetService(ctx, "workflow.plugin.dnsprovider.<provider>")
    ↓ returns handle
InvokeService(handle, "ListZones", typed_input)
    ↓ host routes to provider plugin process
DNSProviderServer.ListZones (provider plugin)
    ↓
provider native SDK (cloudflare-go / godo / namecheap-go)
    ↓ HTTPS to cloud API
zones[]
    ↓ unmarshalled into proto, returned through host
admincli command serializes to ResourceState shape on disk
```

## Error handling

| failure | dispatcher response | exit code |
|---|---|---|
| Provider plugin not loaded (GetService NotFound) | `dnsprovider: provider %q not loaded — check wfctl plugin list` | 2 (per-provider in `--all`: warn+continue) |
| Provider plugin loaded but contract not registered | `dnsprovider: provider %q does not advertise DNSProvider (check plugin.json capabilities.contracts)` | 2 |
| InvokeService transport error | wrap with provider name + RPC; do not include cred values | 2 (per-provider warn+continue) |
| Provider RPC returned error | surface verbatim (already cred-redacted by provider plugin) | 2 |
| Empty zone list | log `no zones found for %q`; do not delete existing state files | 0 |
| Per-zone ListRecords failure | record `import_status: error` in metadata.yaml; keep prior state.json | 0 if ≥1 zone succeeded |
| Mid-operation `^C` | flush per-zone state.json atomically (write-tmp + rename); partial run leaves complete-zone artifacts and an `import.run.json` summary | n/a |

## Multi-Component Validation

1. **In-process unit tests** in workflow-plugin-infra:
   - dispatcher_test.go covers Lookup, NotFound paths, contract-mismatch paths using a fake `EngineCallback` + stub provider server.
   - admincli/dns_import_test.go drives the `import` command end-to-end against fake provider stubs serving canned ListZones/ListRecords.

2. **Per-provider integration tests** (env-gated `INFRA_DNS_CONTRACT_LIVE=1`):
   - Each provider plugin grows `internal/dnsprovider_server_live_test.go`.
   - Runs against real cloud account using read-only creds; asserts ListZones non-empty, ListRecords returns ≥1 record for at least one zone, and round-trip Marshal/Unmarshal preserves the `Record` shape modulo provider_extras.

3. **End-to-end cross-plugin smoke**: Docker compose stack with wfctl host + workflow-plugin-infra + one provider plugin + that provider's stubbed cloud API (httptest server). `wfctl plugin infra dns import --provider <stubbed>` exits 0, writes expected artifacts to a tempdir. This catches what unit tests miss: plugin discovery + ContractRegistry + InvokeService wiring under real plugin loading.

4. **Cross-driver parity**: e2e smoke runs against all three providers (CF/NC/DO) before any PR merges to verify the contract is genuinely uniform.

## Security Review

- Read-only scope for the new CLI's primary path: ListZones + ListRecords use Zone:Read-equivalent tokens per provider. Mutating methods (UpsertTXT/UpsertRecord/DeleteRecord) are existing surfaces unchanged in capability; rewrite preserves their dnsaudit append-only JSONL trail.
- Cred values never cross the dispatch wire. Each provider plugin reads its own `iac.provider.<name>` config block; dispatcher carries no `map[string]string` creds payload.
- Error messages name cred-key NAMES only when missing (`namecheap: required cred "api_key" missing`); values never logged.
- Wrap dispatch errors with `fmt.Errorf("dnsprovider %s: %w (creds redacted)", name, err)` to defend against accidental cred leak in provider-plugin error wrapping.
- `EngineCallbackService` trust boundary: dispatcher trusts the host's `GetService` response. Hostile peer plugin could in theory register a `DNSProvider` impl for a provider name it does not actually serve. Defense: dispatcher cross-checks `ContractRegistry` advertisement against `plugin.json.capabilities.contracts` and refuses to dispatch when the source-of-truth doesn't match. Out of scope: cryptographic attestation — that lives in workflow-plugin-supply-chain.
- Live integration tests require self-hosted runner with stable egress IP (Namecheap allowlist + responsible-rate-limit posture against CF/DO).

## Infrastructure Impact

- workflow-plugin-infra ships a minor version bump (capability surface changes; `cliCommands.dns import` added; libdns deps removed). Existing consumers of `dnsprovider.NewAdapter`: only `admincli/*.go` inside this same plugin; all migrate atomically inside PR 1.
- workflow-plugin-{cloudflare,namecheap,digitalocean} each ship a minor bump (new `DNSProvider` capability advertised; no other surface change). Each repo's existing IaC `infra.dns` resource driver is untouched.
- New runtime dependency: `workflow-plugin-infra/contracts/dnsproviderpb` Go module is imported by each provider plugin. Module path lives under workflow-plugin-infra repo; provider plugins add `replace` directives during development across the worktrees and consume tagged versions in release.
- Self-hosted runner required for live integration tests; existing mandate per workspace design-guidance § Infrastructure.
- No engine ABI change. No new cloud resources. No DB migrations. No production deploy.

## Rollback

- PR 1 (workflow-plugin-infra): revert via `git revert` if dispatcher path proves broken. Existing v2 libdns impls are deleted in the same PR — rollback restores them from git history.
- PRs 2/3/4 (provider plugins): revert per-plugin. With PR 1 reverted, admincli still works on the pre-refactor adapter. With PR 1 in place and an individual provider PR reverted, that provider stops being available for dns-* admincli commands (other providers continue to work — failure is isolated).
- Version pin rollback: any consumer (none in tree today, but gocodealone-dns will be one post-this-design) pins a specific workflow-plugin-infra release in its workflow file; revert pin for rollback.

## Assumptions

- A1: `EngineCallbackService.GetService` + `InvokeService` reliably route from inside one loaded plugin's runtime to another loaded plugin's registered service when invoked from a CLI command dispatched by wfctl. Verified by reading `workflow/plugin/external/proto/plugin.proto` lines 56-90; runtime path verified by reading `workflow/plugin/external/sdk/grpc_server.go` callback wiring. Empirical proof comes in PR 1's e2e smoke test.
- A2: `ContractRegistry` accepts arbitrary plugin-owned proto descriptors via `file_descriptor_set` — verified in `workflow/plugin/external/proto/plugin.proto` ContractRegistry message definition (lines 123-128).
- A3: `plugin.json.capabilities.cliCommands` supports nested subcommands (`wfctl plugin infra dns import` is a 3-level dispatch). Memory references workflow v0.62 shipped `wfctl plugin <name>` CLI dispatch; nested subcommand handling needs verification at PR 1 implementation start (open question O1).
- A4: Each provider's native SDK exposes account-level zone enumeration: cloudflare-go's `Zones().List`, godo's `Domains.List`, namecheap-go-sdk's `Domains.GetList`. Quick read of each library confirms; provider PR authors verify endpoint pagination behavior.
- A5: Live read-only API tokens can be issued per provider: CF Zone:Read, DO scope=read-only or domain-read scope, NC has only "API access" scope (no read-only distinction) — NC test must use a non-production account or accept production-cred risk under self-hosted runner egress.
- A6: workflow-plugin-infra/contracts/ module path is acceptable as the source-of-truth proto location; provider plugins importing it does not introduce a circular dependency because workflow-plugin-infra does not import any provider plugin (dispatcher is gRPC-based, runtime peer).

## Non-Goals

- Workflow-pipeline-step form of import (`infra.dns_import` typed-step factory). CLI surface suffices for v1; a typed-step shape can be added later by wrapping the same dispatcher.
- aws/azure/gcp/godaddy/r53 implementation. Slots exist in the contract; impl deferred to follow-up plans (one PR per provider, same shape as PR 2/3/4).
- Refactor of existing `infra.dns` typed-step driver paths inside provider plugins. They remain in place; the new `DNSProvider` gRPC service in each plugin is additive, served by the same plugin process.
- Cryptographic plugin-identity attestation for cross-plugin contract dispatch (belongs in workflow-plugin-supply-chain).
- Migrating non-libdns dependencies out of workflow-plugin-infra. Only the libdns-family deps go; everything else (modular, workflow SDK, dnsgate/dnsaudit dependencies) stays.
- Schema versioning for `provider_extras_json`. v1 is opaque map; if a provider needs schema discipline it ships a typed message in a v2 of the contract.

## Open Questions

- O1: Does `plugin.json.capabilities.cliCommands` support 3-level dispatch (`wfctl plugin <plugin> <command> <subcommand>`)? Inspection of existing admincli commands shows 2-level today (`wfctl plugin infra <command>`). PR 1 first task: verify, and if not, propose a single 2-level command (`dns-import` instead of `dns import`) — design tolerates either spelling.
- O2: Plugin process lifetime for CLI dispatch — when a user runs `wfctl plugin infra dns import --all`, are workflow-plugin-cloudflare, -namecheap, -digitalocean processes spawned on-demand or only if they were already running? Affects whether the dispatcher needs to "wake" peer plugins or just look them up. Most likely: wfctl spawns peers on-demand via the host registry — confirm at PR 1.
- O3: How does the dispatcher know which providers exist? Two options: (a) iterate every loaded plugin's `ContractRegistry` looking for `DNSProvider` advertisements; (b) honor a static list in workflow-plugin-infra config. Option (a) is correct (zero-config) but depends on host enumeration API — confirm presence + shape at PR 1.

## Top doubts (self-challenge)

1. **Peer-dispatch from CLI runtime is unverified**. The pattern exists at engine module-to-module level (proven by EngineCallbackService.GetService); whether wfctl's plugin-CLI dispatch surface plumbs the same callback channel into the plugin's CLI command handler is the load-bearing assumption. If it doesn't, PR 1 grows a workflow-SDK PR to plumb it — escalation cost is real. Mitigation: PR 1 starts with an e2e smoke test scaffolded before any rewrite work, so the assumption is verified on day one.
2. **Provider plugins double their gRPC surface area**. Each plugin now serves IaCProviderRequired (existing) + DNSProvider (new) inside one process. Risk: SDK pattern may not support arbitrary additional services without a sdk.ServeIaCPluginFull-style extension. Mitigation: check sdk.ServeIaCPlugin variants at PR 1; if no extension exists, a thin sdk patch ships ahead of PR 1 (and the four-PR plan grows to five with a workflow PR).
3. **Cred boundary semantics**. Dispatcher carries no creds; provider plugins are responsible for reading their own config block. Existing admincli commands pass creds inline (`--token` flag); the rewrite removes this. Operator workflow becomes: configure provider plugin once in the wfctl config file, then run admincli without per-command creds. This is the right shape but it IS a UX change for admincli users — note in PR 1 release notes.

## Change Log

| Date | Author | Change |
|---|---|---|
| 2026-05-26 | codingsloth@pm.me | Initial draft (cycle 1) — peer-contract design replacing v2 in-process libdns adapter pattern. |
