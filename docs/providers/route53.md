# Route53 (AWS)

Provider key: `route53`

## Cred keys

| key | required | description |
|---|---|---|
| `region` | yes | AWS region (e.g. `us-east-1`) |
| `access_key_id` | optional* | AWS access key ID |
| `secret_access_key` | optional* | AWS secret access key |
| `session_token` | optional | AWS session token (for STS temp creds) |
| `profile` | optional | AWS profile name (alternative to access_key_id) |

*If `access_key_id`, `secret_access_key`, and `profile` are all empty, libdns falls back to AWS env vars (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_PROFILE`) or ambient instance/role creds.

## YAML example

```yaml
provider: route53
provider_creds:
  region: us-east-1
  access_key_id: $AWS_ACCESS_KEY_ID
  secret_access_key: $AWS_SECRET_ACCESS_KEY
```

## Notes

- AWS `assume_role_arn` deferred to v3 (requires aws-sdk-go-v2 STS chain).
- IAM policy must include `route53:ChangeResourceRecordSets`, `route53:ListResourceRecordSets`, `route53:GetChange` at minimum, scoped to the target hosted zone.
