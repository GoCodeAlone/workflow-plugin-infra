# Azure DNS

Provider key: `azuredns`

## Cred keys

| key | required | description |
|---|---|---|
| `subscription_id` | yes | Azure subscription ID |
| `resource_group_name` | yes | Resource group containing the DNS zone |
| `tenant_id` | SP only | Entra ID tenant — required for service-principal auth |
| `client_id` | SP only | App registration client ID |
| `client_secret` | SP only | App registration client secret |

## Auth modes

- **Service principal**: ALL of `tenant_id` + `client_id` + `client_secret` set.
- **Managed identity**: ALL three empty (ambient Azure managed identity, e.g. AKS workload identity).
- Mixed (1 or 2 set) → adapter rejects at construction.

## YAML examples

Service principal:
```yaml
provider: azuredns
provider_creds:
  subscription_id: $AZ_SUBSCRIPTION_ID
  resource_group_name: dns-rg
  tenant_id: $AZ_TENANT_ID
  client_id: $AZ_CLIENT_ID
  client_secret: $AZ_CLIENT_SECRET
```

Managed identity:
```yaml
provider: azuredns
provider_creds:
  subscription_id: $AZ_SUBSCRIPTION_ID
  resource_group_name: dns-rg
```

## Notes

- IAM role: "DNS Zone Contributor" on the resource group at minimum.
