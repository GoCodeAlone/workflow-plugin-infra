# workflow-plugin-infra

> ⚠️ **Experimental** — This plugin compiles and passes its unit tests but has not been validated in any active GoCodeAlone-internal production deployment. Use with caution. Please [open an issue](https://github.com/GoCodeAlone/workflow-plugin-infra/issues/new) if you adopt it so we can promote it to **verified** status.

## Release note — `minEngineVersion`

`plugin.json` declares `"minEngineVersion": "0.74.0"` — the first workflow release that wires `providerclient.ResourceDriver` through the apply/create path this plugin's admin surface depends on at runtime.

## What this plugin provides

Abstract `infra.*` module types (13 total: `container_service`, `k8s_cluster`, `database`, `cache`, `vpc`, `load_balancer`, `dns`, `registry`, `api_gateway`, `firewall`, `iam_role`, `storage`, `certificate`) with `IaCProvider` delegation. The plugin itself defines no provider-specific behavior — module instances are resolved against the host's configured IaC provider (e.g. workflow-plugin-digitalocean, workflow-plugin-cloudflare).

## DNS handling (post-v1.0.0)

Per-provider DNS support lives in the respective provider plugins (workflow-plugin-{digitalocean,cloudflare,namecheap,hover,...}), each implementing `infra.dns` against its native SDK. Cross-provider DNS orchestration — ownership policy, ownership gates during apply, bulk import, audit trail — lives in `wfctl` directly:

- `wfctl infra import-all --provider <m> --type infra.dns` — bulk import every zone an account holds, via the provider's `IaCProviderEnumerator`
- `wfctl dns-policy {show,set,transfer-ownership,drift}` — manage the `_workflow-dns-policy.<zone>` TXT policy across any provider implementing `infra.dns`
- `wfctl infra apply` — enforces the DNS ownership gate as a pre-action hook for `infra.dns` resources

The previous per-provider DNS adapter pages (`docs/providers/*.md`) and the `infra.dns_record` step type were removed in v1.0.0; the legacy step's peer-dispatch model was architecturally unsupported (see `docs/plans/2026-05-26-dns-provider-contract-design.md`).
