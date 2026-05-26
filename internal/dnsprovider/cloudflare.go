package dnsprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	libdnscf "github.com/libdns/cloudflare"
	"github.com/libdns/libdns"
)

func init() { Register("cloudflare", newCloudflareAdapter) }

// Compile-time interface check.
var _ dnspolicy.Adapter = (*cfAdapter)(nil)

type cfAdapter struct {
	provider *libdnscf.Provider
}

func newCloudflareAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
	token := ExpandCredsMap(creds)["token"]
	if token == "" {
		return nil, fmt.Errorf("cloudflare: missing creds.token")
	}
	return &cfAdapter{provider: &libdnscf.Provider{APIToken: token}}, nil
}

// GetTXT fetches TXT values for name (FQDN of the policy record).
func (a *cfAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs, err := a.provider.GetRecords(ctx, zone)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: get records: %w (creds redacted)", err)
	}
	var out []string
	for _, r := range recs {
		rr := r.RR()
		if rr.Type == "TXT" && rr.Name == relName {
			out = append(out, rr.Data)
		}
	}
	return out, nil
}

// UpsertTXT writes TXT values to the policy name.
// Cloudflare's SetRecords handles upsert internally (doesn't require pre-fetched ID).
func (a *cfAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs := make([]libdns.Record, len(values))
	for i, v := range values {
		recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
	}
	_, err := a.provider.SetRecords(ctx, zone, recs)
	if err != nil {
		return fmt.Errorf("cloudflare: upsert TXT: %w (creds redacted)", err)
	}
	return nil
}

// UpsertRecord upserts an arbitrary DNS record via Cloudflare.
func (a *cfAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
	if priority < 0 {
		return "", fmt.Errorf("dnsprovider: priority must be >= 0, got %d", priority)
	}
	rr := libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}
	out, err := a.provider.SetRecords(ctx, zone, []libdns.Record{rr})
	if err != nil {
		return "", fmt.Errorf("cloudflare: upsert record: %w (creds redacted)", err)
	}
	if len(out) > 0 {
		return out[0].RR().Name, nil // Cloudflare returns records with internal ID embedded
	}
	return "", nil
}

// DeleteRecord deletes a DNS record from Cloudflare.
// Cloudflare's DeleteRecords can look up by name+type internally.
func (a *cfAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	existing, err := a.provider.GetRecords(ctx, zone)
	if err != nil {
		return fmt.Errorf("cloudflare: list records: %w (creds redacted)", err)
	}
	var toDelete []libdns.Record
	for _, e := range existing {
		rr := e.RR()
		if rr.Type == recordType && rr.Name == name {
			toDelete = append(toDelete, e)
		}
	}
	if len(toDelete) == 0 {
		return nil // idempotent
	}
	_, err = a.provider.DeleteRecords(ctx, zone, toDelete)
	if err != nil {
		return fmt.Errorf("cloudflare: delete record: %w (creds redacted)", err)
	}
	return nil
}
