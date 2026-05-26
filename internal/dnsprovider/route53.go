package dnsprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	"github.com/libdns/libdns"
	libdnsr53 "github.com/libdns/route53"
)

var _ dnspolicy.Adapter = (*route53Adapter)(nil)

func init() { Register("route53", newRoute53Adapter) }

// r53ProviderIface is the minimum upstream surface route53Adapter consumes.
// Production holds *libdnsr53.Provider; tests inject stubs (closes cycle-3 C3-2).
type r53ProviderIface interface {
	GetRecords(context.Context, string) ([]libdns.Record, error)
	SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
	DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
	AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}

var _ r53ProviderIface = (*libdnsr53.Provider)(nil)

type route53Adapter struct {
	provider r53ProviderIface
}

func newRoute53Adapter(creds map[string]string) (dnspolicy.Adapter, error) {
	c := ExpandCredsMap(creds)
	region := c["region"]
	if region == "" {
		return nil, fmt.Errorf("route53: missing creds.region (see docs/providers/route53.md)")
	}
	return &route53Adapter{provider: &libdnsr53.Provider{
		Region:          region,
		AccessKeyId:     c["access_key_id"],
		SecretAccessKey: c["secret_access_key"],
		SessionToken:    c["session_token"],
		Profile:         c["profile"],
	}}, nil
}

func (a *route53Adapter) GetTXT(ctx context.Context, name string) ([]string, error) {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs, err := a.provider.GetRecords(ctx, zone)
	if err != nil {
		return nil, fmt.Errorf("route53: get records: %w (creds redacted)", err)
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

func (a *route53Adapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs := make([]libdns.Record, len(values))
	for i, v := range values {
		recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
	}
	if _, err := a.provider.SetRecords(ctx, zone, recs); err != nil {
		return fmt.Errorf("route53: upsert TXT: %w (creds redacted)", err)
	}
	return nil
}

func (a *route53Adapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
	if priority < 0 {
		return "", fmt.Errorf("route53: priority must be >= 0, got %d", priority)
	}
	// Note: priority is currently dropped for non-MX/SRV records (matches v1 precedent).
	recs := []libdns.Record{libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}}
	res, err := a.provider.SetRecords(ctx, zone, recs)
	if err != nil {
		return "", fmt.Errorf("route53: upsert record: %w (creds redacted)", err)
	}
	if len(res) > 0 {
		return res[0].RR().Name, nil
	}
	return "", nil
}

func (a *route53Adapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	recs := []libdns.Record{libdns.RR{Type: recordType, Name: name}}
	if _, err := a.provider.DeleteRecords(ctx, zone, recs); err != nil {
		return fmt.Errorf("route53: delete record: %w (creds redacted)", err)
	}
	return nil
}
