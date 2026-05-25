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

Before any `infra.dns_record` step can apply, the zone must have a policy TXT
record at `_workflow-dns-policy.<zone>`. Use `wfctl infra-dns set-policy` to
bootstrap the ownership policy:

```
# Mark the sre team as the default (catch-all) owner:
wfctl infra-dns set-policy example.com --owner sre --default --token "$DO_TOKEN"

# Add a non-default owner with specific name patterns:
wfctl infra-dns set-policy example.com --owner multisite --patterns www,admin --token "$DO_TOKEN"
```

See the design doc at `docs/plans/2026-05-25-dns-ownership-policy-design.md`
for policy format details and the `_workflow-dns-policy` TXT schema.

## Future: file-input + bootstrap modes

The v1 `set-policy` command takes individual owner entries via flags
(`--owner`, `--patterns`, `--types`, `--default`, `--token`, `--ttl`).
A future release will add `-f <yaml-file>` for bulk policy import and
`--bootstrap` / `--overwrite-existing` flags for safer first-write guards
(per the design's 6-case flag table).

## SOA/NS records

The gate refuses to mutate SOA/NS records for **any** owner (including the
default owner) unless the owner entry explicitly lists `SOA` or `NS` in its
`Types` field. Zone-level records should be managed via your DNS provider's
console or zone transfer tooling, not through automation.
