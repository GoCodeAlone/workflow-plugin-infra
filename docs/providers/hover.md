# Hover

Provider key: `hover`

## Cred keys

| key | required | description |
|---|---|---|
| `username` | yes | Hover account username |
| `password` | yes | Hover account password |
| `totp_secret` | optional | TOTP shared secret (base32-encoded) for 2FA accounts |

## YAML example

```yaml
provider: hover
provider_creds:
  username: $HOVER_USERNAME
  password: $HOVER_PASSWORD
  totp_secret: $HOVER_TOTP_SECRET  # optional
```

## Notes

- Hover has no public DNS API. Implementation uses HTML-scrape client via [`pkg/hoverclient`](https://github.com/GoCodeAlone/workflow-plugin-hover/tree/main/pkg/hoverclient) (extracted in workflow-plugin-hover v0.3.0).
- Scraping fragile to Hover UI changes; report breakage at [workflow-plugin-hover issues](https://github.com/GoCodeAlone/workflow-plugin-hover/issues).
- Per-record CRUD only — no batch RRset primitive. `UpsertTXT` emulates RRset replace via list → delete same-name TXT → create each new value.
- `UpsertTXT` calls `GetDomain` to resolve domain ID before `CreateRecord` (Hover API takes domain ID, not zone name).
- TOTP secret must be base32-encoded (standard authenticator format). Invalid base32 → adapter rejects at construction.
- Adapter uses `Credentials.TOTPSecret` (typed); `totp_secret` cred string is parsed via `hoverclient.ParseBase32`.
- `priority` arg silently dropped (matches v1 precedent + `hoverclient.DNSRecord` has no Priority field).
