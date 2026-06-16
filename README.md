# workflow-plugin-infra

> ⚠️ **Experimental** — This plugin compiles and passes its unit tests but has not been validated in any active GoCodeAlone-internal production deployment. Use with caution. Please [open an issue](https://github.com/GoCodeAlone/workflow-plugin-infra/issues/new) if you adopt it so we can promote it to **verified** status.

## Release note — `minEngineVersion`

`plugin.json` declares `"minEngineVersion": "0.80.14"` — the first workflow release that frees the `wfctl dns` namespace for plugin-owned CLI dispatch while retaining the `providerclient.ResourceDriver` apply/create support this plugin's admin surface depends on at runtime.

## What this plugin provides

Abstract `infra.*` module types (13 total: `container_service`, `k8s_cluster`, `database`, `cache`, `vpc`, `load_balancer`, `dns`, `registry`, `api_gateway`, `firewall`, `iam_role`, `storage`, `certificate`) with `IaCProvider` delegation. The plugin itself defines no provider-specific behavior — module instances are resolved against the host's configured IaC provider (e.g. workflow-plugin-digitalocean, workflow-plugin-cloudflare).

## DNS handling (post-v1.0.0)

Per-provider DNS support lives in the respective provider plugins (workflow-plugin-{digitalocean,cloudflare,namecheap,hover,...}), each implementing `infra.dns` against its native SDK. Capability-scoped DNS orchestration is exposed by this plugin as a top-level wfctl command:

- `wfctl dns intent compile` — compile domain intent JSON plus DNS portfolio exports into generated `infra.dns` / `infra.dns_delegation` config and a report
- `wfctl dns intent reconcile` — compile, validate, optionally verify live registrar delegation, and run `wfctl infra plan` / `wfctl infra apply` for that generated config
- `wfctl dns stage cloudflare` — compile Cloudflare `infra.dns` staging config and an audit report directly from DNS portfolio exports
- `wfctl infra import-all --provider <m> --type infra.dns` — bulk import every zone an account holds, via the provider's `IaCProviderEnumerator`

Cross-cutting DNS policy and apply gates remain in wfctl core because `wfctl infra apply` owns that lifecycle hook:

- `wfctl dns-policy {show,set,transfer-ownership,drift}` — manage the `_workflow-dns-policy.<zone>` TXT policy across any provider implementing `infra.dns`
- `wfctl infra apply` — enforces the DNS ownership gate as a pre-action hook for `infra.dns` resources

The previous per-provider DNS adapter pages (`docs/providers/*.md`) and the `infra.dns_record` step type were removed in v1.0.0; the legacy step's peer-dispatch model was architecturally unsupported (see `docs/plans/2026-05-26-dns-provider-contract-design.md`).

Domain intent may also include `forward_to` for redirect-only domains moving to
Cloudflare DNS. The compiler emits:

- a Cloudflare `infra.dns` resource containing an originless proxied `A @
  192.0.2.1` placeholder when `records_policy: discard_parked` is used; and
- a Cloudflare `infra.http_redirect` resource targeting `forward_to`, preserving
  path and query string by default.

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
again and verifies the desired nameservers:

```sh
wfctl dns intent reconcile \
  --intent domains.json \
  --portfolio 'zones/*.portfolio.json' \
  --domain example.com \
  --mode apply \
  --auto-approve \
  --verify-delegation \
  --delegation-config-dir infra \
  --plugin-dir data/plugins
```
