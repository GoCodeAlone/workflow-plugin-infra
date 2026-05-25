package dnsprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	libdnsdo "github.com/libdns/digitalocean"
	"github.com/libdns/libdns"
)

type doAdapter struct {
	provider *libdnsdo.Provider
}

func newDigitalOceanAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
	token := ExpandCredsMap(creds)["token"]
	if token == "" {
		return nil, fmt.Errorf("digitalocean: missing creds.token")
	}
	return &doAdapter{provider: &libdnsdo.Provider{APIToken: token}}, nil
}

// GetTXT fetches TXT values for name (FQDN of the policy record).
// name is the full policy TXT name, e.g. "_workflow-dns-policy.example.com".
// zone is derived from name by stripping the leading label.
func (a *doAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs, err := a.provider.GetRecords(ctx, zone)
	if err != nil {
		return nil, fmt.Errorf("digitalocean: get records: %w (creds redacted)", err)
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

// upsertRecords: DO-specific pattern (closes plan-cycle-1 C-4 + C-5).
// libdns/digitalocean SetRecords requires existing ID (via idFromRecord type-assert);
// passing a new Record without ID errors with strconv.Atoi failure.
// Use GET-then-AppendRecords (new) OR SetRecords-with-ID (existing) per-record.
func (a *doAdapter) upsertRecords(ctx context.Context, zone string, desired []libdns.RR) ([]libdns.Record, error) {
	existing, err := a.provider.GetRecords(ctx, zone)
	if err != nil {
		return nil, fmt.Errorf("digitalocean: list records: %w (creds redacted)", err)
	}
	// Match existing records by (Type, Name) and reuse their ID for SetRecords;
	// anything unmatched goes through AppendRecords (creates new).
	var updates, appends []libdns.Record
	for _, d := range desired {
		matched := false
		for _, e := range existing {
			errs := e.RR()
			if errs.Type == d.Type && errs.Name == d.Name && errs.Data == d.Data {
				matched = true // exact match — no-op (idempotent)
				break
			}
			if errs.Type == d.Type && errs.Name == d.Name {
				// Reuse the existing DNS record (with ID) but update Data+TTL
				updates = append(updates, libdnsdo.DNS{
					Record: libdns.RR{
						Name: d.Name,
						Type: d.Type,
						Data: d.Data,
						TTL:  d.TTL,
					},
					ID: e.(libdnsdo.DNS).ID,
				})
				matched = true
				break
			}
		}
		if !matched {
			appends = append(appends, d)
		}
	}
	var out []libdns.Record
	if len(updates) > 0 {
		u, err := a.provider.SetRecords(ctx, zone, updates)
		if err != nil {
			return nil, fmt.Errorf("digitalocean: update records: %w (creds redacted)", err)
		}
		out = append(out, u...)
	}
	if len(appends) > 0 {
		a2, err := a.provider.AppendRecords(ctx, zone, appends)
		if err != nil {
			return nil, fmt.Errorf("digitalocean: append records: %w (creds redacted)", err)
		}
		out = append(out, a2...)
	}
	return out, nil
}

// UpsertTXT writes TXT values to the policy name.
func (a *doAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs := make([]libdns.RR, len(values))
	for i, v := range values {
		recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
	}
	_, err := a.upsertRecords(ctx, zone, recs)
	return err
}

// UpsertRecord upserts an arbitrary DNS record.
func (a *doAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
	if priority < 0 {
		return "", fmt.Errorf("dnsprovider: priority must be >= 0, got %d", priority)
	}
	rr := libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}
	out, err := a.upsertRecords(ctx, zone, []libdns.RR{rr})
	if err != nil {
		return "", err
	}
	if len(out) > 0 {
		if dns, ok := out[0].(libdnsdo.DNS); ok {
			return dns.ID, nil
		}
	}
	return "", nil
}

// DeleteRecord: GET first to find ID, then DeleteRecords with ID.
// libdns/digitalocean DeleteRecords requires ID (closes plan-cycle-1 C-5).
func (a *doAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	existing, err := a.provider.GetRecords(ctx, zone)
	if err != nil {
		return fmt.Errorf("digitalocean: list records: %w (creds redacted)", err)
	}
	var toDelete []libdns.Record
	for _, e := range existing {
		rr := e.RR()
		if rr.Type == recordType && rr.Name == name {
			toDelete = append(toDelete, e) // preserves ID in the DNS struct
		}
	}
	if len(toDelete) == 0 {
		return nil // idempotent: nothing to delete
	}
	_, err = a.provider.DeleteRecords(ctx, zone, toDelete)
	if err != nil {
		return fmt.Errorf("digitalocean: delete records: %w (creds redacted)", err)
	}
	return nil
}

// zoneFromPolicyName extracts the zone from the policy TXT FQDN.
// For "_workflow-dns-policy.gocodealone.tech" → "gocodealone.tech".
func zoneFromPolicyName(fqdn string) string {
	const prefix = "_workflow-dns-policy."
	if len(fqdn) > len(prefix) && fqdn[:len(prefix)] == prefix {
		return fqdn[len(prefix):]
	}
	// Fallback: strip first label
	for i := 0; i < len(fqdn); i++ {
		if fqdn[i] == '.' {
			return fqdn[i+1:]
		}
	}
	return fqdn
}

// relativeNameFromFQDN returns the part of name before the trailing ".zone".
func relativeNameFromFQDN(name, zone string) string {
	suffix := "." + zone
	if len(name) > len(suffix) && name[len(name)-len(suffix):] == suffix {
		return name[:len(name)-len(suffix)]
	}
	return name
}
