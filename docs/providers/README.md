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

`UpsertRecord` accepts a `priority int32` arg. v1 adapters (DigitalOcean, Cloudflare) drop this for all record types using the same `libdns.RR{...}` shape — non-zero priority is silently ignored. v2 adapters preserve this v1 precedent (no behavior regression). MX/SRV typed-record dispatch (using `libdns.MX{Preference:...}` / `libdns.SRV{Priority:...}`) is a known v3 followup tracked in the design's "Followups" section. Wontfix in v2: matching v1 precedent avoids surprise behavior change for existing consumers.
