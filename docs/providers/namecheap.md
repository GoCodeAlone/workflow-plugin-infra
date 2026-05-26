# Namecheap

Provider key: `namecheap`

## Cred keys

| key | required | description |
|---|---|---|
| `api_key` | yes | Namecheap API key (enable + whitelist IPs at namecheap.com → Profile → Tools → API Access) |
| `user` | yes | Namecheap API user (usually account username) |
| `client_ip` | **yes (strict)** | Whitelisted public IP of the calling machine. NO discovery fallback |
| `api_endpoint` | optional | API endpoint URL (defaults to production; use `https://api.sandbox.namecheap.com/xml.response` for sandbox) |

## YAML example

```yaml
provider: namecheap
provider_creds:
  api_key: $NAMECHEAP_API_KEY
  user: my-namecheap-username
  client_ip: 203.0.113.42  # whitelisted in Namecheap console
```

## Notes

- IP whitelist: enroll the calling machine's egress IP in the Namecheap API Access console before first call.
- Self-hosted runner egress IPs MUST be allocated/static and whitelisted.
- Upstream `libdns/namecheap.SetRecords` safely replaces records per (name,type) (verified via source spike 2026-05-26 — Get-merge-Set internally). Foreign-(name,type) records preserved.
