# GoDaddy

Provider key: `godaddy`

## Cred keys

| key | required | description |
|---|---|---|
| `api_token` | yes | Format: `<sso-key>:<sso-secret>` (concatenated with colon). Generate at developer.godaddy.com → API Keys |

## ⚠ API access restriction

GoDaddy revoked public DNS API access for accounts with **fewer than 50 domains** (reported 2024-Q1, unresolved as of `libdns/godaddy v1.1.0` release Aug 2025). API returns 403 unauthorized for small-account holders. Test with your account before pinning to production.

## YAML example

```yaml
provider: godaddy
provider_creds:
  api_token: $GODADDY_SSO_KEY:$GODADDY_SSO_SECRET
```

## Notes

- Adapter validates colon-format at construction; runtime 403 from API surfaces as standard provider error.
- No live CI verification (per workspace cost discipline + user "unit tests only" choice).
