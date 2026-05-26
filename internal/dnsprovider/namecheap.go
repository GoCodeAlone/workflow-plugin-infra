package dnsprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	"github.com/libdns/libdns"
	libdnsnc "github.com/libdns/namecheap"
)

var _ dnspolicy.Adapter = (*namecheapAdapter)(nil)

func init() { Register("namecheap", newNamecheapAdapter) }

type ncProviderIface interface {
	GetRecords(context.Context, string) ([]libdns.Record, error)
	SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
	AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
	DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}

var _ ncProviderIface = (*libdnsnc.Provider)(nil)

type namecheapAdapter struct {
	provider ncProviderIface
}

func newNamecheapAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
	c := ExpandCredsMap(creds)
	apiKey, user, clientIP := c["api_key"], c["user"], c["client_ip"]
	if apiKey == "" {
		return nil, fmt.Errorf("namecheap: missing creds.api_key (see docs/providers/namecheap.md)")
	}
	if user == "" {
		return nil, fmt.Errorf("namecheap: missing creds.user (see docs/providers/namecheap.md)")
	}
	if clientIP == "" {
		return nil, fmt.Errorf("namecheap: missing creds.client_ip (strict — whitelisted IP required; see docs/providers/namecheap.md)")
	}
	return &namecheapAdapter{provider: &libdnsnc.Provider{
		APIKey: apiKey, User: user, ClientIP: clientIP, APIEndpoint: c["api_endpoint"],
	}}, nil
}

func (a *namecheapAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs, err := a.provider.GetRecords(ctx, zone)
	if err != nil {
		return nil, fmt.Errorf("namecheap: get records: %w (creds redacted)", err)
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

func (a *namecheapAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs := make([]libdns.Record, len(values))
	for i, v := range values {
		recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
	}
	if _, err := a.provider.SetRecords(ctx, zone, recs); err != nil {
		return fmt.Errorf("namecheap: upsert TXT: %w (creds redacted)", err)
	}
	return nil
}

func (a *namecheapAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
	if priority < 0 {
		return "", fmt.Errorf("namecheap: priority must be >= 0, got %d", priority)
	}
	recs := []libdns.Record{libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}}
	res, err := a.provider.SetRecords(ctx, zone, recs)
	if err != nil {
		return "", fmt.Errorf("namecheap: upsert record: %w (creds redacted)", err)
	}
	if len(res) > 0 {
		return res[0].RR().Name, nil
	}
	return "", nil
}

func (a *namecheapAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	recs := []libdns.Record{libdns.RR{Type: recordType, Name: name}}
	if _, err := a.provider.DeleteRecords(ctx, zone, recs); err != nil {
		return fmt.Errorf("namecheap: delete record: %w (creds redacted)", err)
	}
	return nil
}
