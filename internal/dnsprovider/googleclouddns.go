package dnsprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	libdnsgcp "github.com/libdns/googleclouddns"
	"github.com/libdns/libdns"
)

var _ dnspolicy.Adapter = (*gcpAdapter)(nil)

func init() { Register("googleclouddns", newGoogleCloudDNSAdapter) }

type gcpProviderIface interface {
	GetRecords(context.Context, string) ([]libdns.Record, error)
	SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
	DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
	AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}

var _ gcpProviderIface = (*libdnsgcp.Provider)(nil)

type gcpAdapter struct {
	provider gcpProviderIface
}

func newGoogleCloudDNSAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
	c := ExpandCredsMap(creds)
	project := c["gcp_project"]
	if project == "" {
		return nil, fmt.Errorf("googleclouddns: missing creds.gcp_project (see docs/providers/googleclouddns.md)")
	}
	return &gcpAdapter{provider: &libdnsgcp.Provider{
		Project:            project,
		ServiceAccountJSON: c["service_account_path"],
	}}, nil
}

func (a *gcpAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs, err := a.provider.GetRecords(ctx, zone)
	if err != nil {
		return nil, fmt.Errorf("googleclouddns: get records: %w (creds redacted)", err)
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

func (a *gcpAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs := make([]libdns.Record, len(values))
	for i, v := range values {
		recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
	}
	if _, err := a.provider.SetRecords(ctx, zone, recs); err != nil {
		return fmt.Errorf("googleclouddns: upsert TXT: %w (creds redacted)", err)
	}
	return nil
}

func (a *gcpAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
	if priority < 0 {
		return "", fmt.Errorf("googleclouddns: priority must be >= 0, got %d", priority)
	}
	recs := []libdns.Record{libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}}
	res, err := a.provider.SetRecords(ctx, zone, recs)
	if err != nil {
		return "", fmt.Errorf("googleclouddns: upsert record: %w (creds redacted)", err)
	}
	if len(res) > 0 {
		return res[0].RR().Name, nil
	}
	return "", nil
}

func (a *gcpAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	recs := []libdns.Record{libdns.RR{Type: recordType, Name: name}}
	if _, err := a.provider.DeleteRecords(ctx, zone, recs); err != nil {
		return fmt.Errorf("googleclouddns: delete record: %w (creds redacted)", err)
	}
	return nil
}
