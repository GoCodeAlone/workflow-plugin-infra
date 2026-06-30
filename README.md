# workflow-plugin-infra

> ⚠️ **Experimental** — This plugin compiles and passes its unit tests but has not been validated in any active GoCodeAlone-internal production deployment. Use with caution. Please [open an issue](https://github.com/GoCodeAlone/workflow-plugin-infra/issues/new) if you adopt it so we can promote it to **verified** status.

## Release note — `minEngineVersion`

`plugin.json` declares `"minEngineVersion": "0.80.17"` — the first workflow release that frees both the `wfctl dns` namespace and the `wfctl dns-policy` compatibility alias for plugin-owned CLI dispatch while retaining the `providerclient.ResourceDriver` apply/create support this plugin's admin surface depends on at runtime.

## What this plugin provides

Abstract `infra.*` module types (13 total: `container_service`, `k8s_cluster`, `database`, `cache`, `vpc`, `load_balancer`, `dns`, `registry`, `api_gateway`, `firewall`, `iam_role`, `storage`, `certificate`) with `IaCProvider` delegation. The plugin itself defines no provider-specific behavior — module instances are resolved against the host's configured IaC provider (e.g. workflow-plugin-digitalocean, workflow-plugin-cloudflare).

## Secrets boundary

This plugin does not own secret storage, secret encryption, KMS integrations, or
generic secret lifecycle management. Those remain split intentionally:

- the workflow engine owns `secrets.vault`, `secrets.aws`,
  `step.secret_fetch`, `step.secret_set`, `step.secret_rotate`, and
  `step.iac_secret_reachability`;
- `workflow-plugin-security` owns cryptographic/security controls such as
  MFA, local encryption, AWS KMS, GCP KMS, and Vault Transit; and
- this plugin only exposes four in-process secret-admin steps through
  `NewInfraEnginePlugin`: `step.secret_list`, `step.secret_delete`,
  `step.secret_vault_status`, and `step.secret_vault_test`.

The secret-admin steps resolve an existing `workflow/secrets.Provider` from
the host application's service registry by `module:`. They list, delete, or
probe already-configured engine secret providers; they do not create backends,
store secret values themselves, implement AWS/GCP/Vault clients, or appear in
the external gRPC plugin manifest's `capabilities.stepTypes`.

## DNS handling (post-v1.0.0)

Per-provider DNS support lives in the respective provider plugins (workflow-plugin-{digitalocean,cloudflare,namecheap,hover,...}), each implementing `infra.dns` against its native SDK. Capability-scoped DNS orchestration is exposed by this plugin as a top-level wfctl command:

- `wfctl dns intent compile` — compile domain intent JSON plus DNS portfolio exports into generated `infra.dns` / `infra.dns_delegation` config and a report
- `wfctl dns intent reconcile` — compile, validate, optionally verify registrar and public DNS delegation, and run `wfctl infra plan` / `wfctl infra apply` for that generated config
- `wfctl dns stage cloudflare` — compile Cloudflare `infra.dns` staging config and an audit report directly from DNS portfolio exports
- `wfctl infra import-all --provider <m> --type infra.dns` — bulk import every zone an account holds, via the provider's `IaCProviderEnumerator`

Cross-provider DNS policy orchestration lives with this plugin:

- `wfctl dns policy show` — inspect `_workflow-dns-policy.<zone>` TXT policy from portfolio exports
- `wfctl dns policy check` — check generated `infra.dns` config against portfolio policy before apply
- `wfctl dns-policy show` — compatibility alias for existing operator workflows, dispatched through the plugin command registry

`wfctl dns intent reconcile --mode apply` runs the same policy check before it
calls generic `wfctl infra apply`. When `WORKFLOW_DNS_OWNER` is set, missing
policy fails closed. When it is unset, reconcile logs a warning and skips the
policy check, matching the old adoption behavior while keeping generic
`wfctl infra apply` free of DNS-specific lifecycle hooks.

The previous per-provider DNS adapter pages (`docs/providers/*.md`) and the `infra.dns_record` step type were removed in v1.0.0; the legacy step's peer-dispatch model was architecturally unsupported (see `docs/plans/2026-05-26-dns-provider-contract-design.md`).

Domain intent may also include `forward_to` for domains whose public web hosts
should redirect through Cloudflare. `forward_hosts` optionally scopes the
redirected hostnames and defaults to `@`; each value may be `@`, a relative
hostname such as `www`, or an in-zone FQDN such as `www.example.com`. The
compiler emits:

- a Cloudflare `infra.dns` resource that replaces existing `A`, `AAAA`, and
  `CNAME` records for only `forward_hosts` with originless proxied `A`
  placeholders pointing at `192.0.2.1`; and
- one Cloudflare `infra.http_redirect` resource per forwarded host targeting
  `forward_to`, preserving path and query string by default.

Hosts outside `forward_hosts` are preserved, so a zone can redirect apex/`www`
while leaving names such as `admin` or `*.preview` routed to their existing
origin.

Domain intent may include `web_target` for domains whose authoritative zone is
being moved to Cloudflare while web hosting is moving to another platform. When
set, the compiler preserves non-web records from the selected source snapshot,
removes existing `A`, `AAAA`, and `CNAME` records for `web_hosts` (default:
`@`, `www`), and emits proxied Cloudflare `CNAME` records to `web_target`.
Use this for cutovers such as moving a site to a shared multisite origin without
discarding mail, verification TXT, or other non-web DNS records.

Generated Cloudflare DNS resources default website-capable `A`, `AAAA`, and
`CNAME` records to `proxied: true` so moved zones keep Cloudflare edge features.
Mail/service hostnames such as `mail`, `smtp`, `imap`, `autodiscover`,
underscore-prefixed service records, and in-zone MX targets are kept DNS-only.
When Cloudflare portfolio imports include explicit proxy state, preserved
Cloudflare records keep that value. Domain intent can override individual hosts
with `dns_only_hosts` or `proxied_hosts`; entries may be relative labels such as
`www`, apex `@`, or fully qualified names such as `www.example.com`.
Known registrar parking web records, such as Hover's `216.40.34.41` parking
addresses and Namecheap's `parkingpage.namecheap.com`, are not preserved during
Cloudflare staging. If another provider snapshot contains non-parked web
records for the same zone, those records are carried forward instead while
mail, MX, TXT, and other non-web records remain intact.
Domain intent can set `manage_unlisted: true` for corrective cutovers where
stale provider records should be deleted after the intended record set is known.

Generated Cloudflare DNS resources include a TXT marker at
`_workflow-dns-managed.<zone>` with `heritage=wfinfra-v1`, `managed_by=wfctl`,
the generated state directory, and the resource name. This marker is not the
ownership-policy gate; it is a zone-visible breadcrumb that helps detect when a
zone may already be managed by another wfctl state surface.

Cloudflare staging uses committed DNS portfolio exports as input and emits
ordinary IaC resources; provider plugins still own the actual Cloudflare API
calls through `wfctl infra plan/apply`:

```sh
wfctl dns stage cloudflare \
  --portfolio 'zones/*.portfolio.json' \
  --scope safe \
  --output infra/cloudflare-staging.generated.wfctl.yaml \
  --report reports/cloudflare-staging-report.json
```

For registrar cutovers, use `wfctl dns intent reconcile` instead of repository
scripts. With `--verify-delegation`, apply mode imports registrar
`infra.dns_delegation` snapshots before apply, checks any
`expected_current_nameservers`, applies the generated resources, then imports
again and verifies the registrar API reports the desired nameservers.
Add `--verify-live-delegation` when the run should also wait for public DNS
lookups to return the desired nameservers after the registrar write:

```sh
wfctl dns intent reconcile \
  --intent domains.json \
  --portfolio 'zones/*.portfolio.json' \
  --domain example.com \
  --mode apply \
  --auto-approve \
  --verify-delegation \
  --verify-live-delegation \
  --live-delegation-timeout 10m \
  --live-delegation-interval 30s \
  --delegation-config-dir infra \
  --plugin-dir data/plugins
```
