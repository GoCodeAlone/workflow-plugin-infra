package dnsprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	libdnsdo "github.com/libdns/digitalocean"
	"github.com/libdns/libdns"
)

func init() { Register("digitalocean", newDigitalOceanAdapter) }

// Compile-time interface check.
var _ dnspolicy.Adapter = (*doAdapter)(nil)

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

// upsertTXTRRset replaces the entire TXT RRset at (relName) atomically:
// delete all existing TXT records at that name, then append all desired values.
// This matches Cloudflare's SetRecords semantic and avoids stale entries when
// the desired set shrinks (e.g. removing one owner from a 3-owner policy).
func (a *doAdapter) upsertTXTRRset(ctx context.Context, zone, relName string, values []string, ttl int) error {
	existing, err := a.provider.GetRecords(ctx, zone)
	if err != nil {
		return fmt.Errorf("digitalocean: list records: %w (creds redacted)", err)
	}
	var toDelete []libdns.Record
	for _, e := range existing {
		rr := e.RR()
		if rr.Type == "TXT" && rr.Name == relName {
			toDelete = append(toDelete, e)
		}
	}
	if len(toDelete) > 0 {
		if _, err := a.provider.DeleteRecords(ctx, zone, toDelete); err != nil {
			return fmt.Errorf("digitalocean: delete stale TXT: %w (creds redacted)", err)
		}
	}
	if len(values) == 0 {
		return nil
	}
	toAdd := make([]libdns.Record, len(values))
	for i, v := range values {
		toAdd[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
	}
	if _, err := a.provider.AppendRecords(ctx, zone, toAdd); err != nil {
		return fmt.Errorf("digitalocean: append TXT: %w (creds redacted)", err)
	}
	return nil
}

// upsertRecords: DO-specific pattern for non-TXT single-record upserts
// (closes plan-cycle-1 C-4 + C-5).
// libdns/digitalocean SetRecords requires existing ID (via idFromRecord type-assert);
// passing a new Record without ID errors with strconv.Atoi failure.
// Use GET-then-AppendRecords (new) OR SetRecords-with-ID (existing) per-record.
// For TXT RRset replacement use upsertTXTRRset instead.
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
				// Reuse the existing DNS record (with ID) but update Data+TTL.
				// Guard the type-assert: if the provider returns an unexpected
				// concrete type we cannot extract the ID, so return an error
				// rather than panic.
				dns, ok := e.(libdnsdo.DNS)
				if !ok {
					return nil, fmt.Errorf("digitalocean: unexpected record type %T, cannot extract ID", e)
				}
				updates = append(updates, libdnsdo.DNS{
					Record: libdns.RR{
						Name: d.Name,
						Type: d.Type,
						Data: d.Data,
						TTL:  d.TTL,
					},
					ID: dns.ID,
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

// UpsertTXT writes TXT values to the policy name using RRset-replace semantics:
// all existing TXT records at the policy name are deleted, then the desired
// values are appended. This prevents stale TXT entries when owners are removed.
func (a *doAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	return a.upsertTXTRRset(ctx, zone, relName, values, ttl)
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
