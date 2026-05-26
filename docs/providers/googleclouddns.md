# GCP Cloud DNS

Provider key: `googleclouddns`

## Cred keys

| key | required | description |
|---|---|---|
| `gcp_project` | yes | GCP project ID |
| `service_account_path` | optional | Path to service-account JSON file. Omit → libdns uses Application Default Credentials (ADC) |

## YAML example

```yaml
provider: googleclouddns
provider_creds:
  gcp_project: my-gcp-project
  service_account_path: /var/secrets/sa.json
```

ADC mode (GKE workload identity, GCE metadata server, or `GOOGLE_APPLICATION_CREDENTIALS` env):
```yaml
provider: googleclouddns
provider_creds:
  gcp_project: my-gcp-project
```

## Notes

- Inline JSON cred form deferred to v3.
- IAM role: `roles/dns.admin` for the target managed zone.
