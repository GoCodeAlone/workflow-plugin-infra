package dnsprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	libdnsgd "github.com/libdns/godaddy"
	"github.com/libdns/libdns"
)

var _ dnspolicy.Adapter = (*godaddyAdapter)(nil)

func init() { Register("godaddy", newGoDaddyAdapter) }

type gdProviderIface interface {
	GetRecords(context.Context, string) ([]libdns.Record, error)
	SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
	AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
	DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}

var _ gdProviderIface = (*libdnsgd.Provider)(nil)

type godaddyAdapter struct {
	provider gdProviderIface
}

func newGoDaddyAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
	c := ExpandCredsMap(creds)
	token := c["api_token"]
	if token == "" {
		return nil, fmt.Errorf("godaddy: missing creds.api_token (format: \"<sso-key>:<sso-secret>\"; see docs/providers/godaddy.md)")
	}
	// Closes cycle-3 I3-5: strict split + both parts non-empty.
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("godaddy: creds.api_token must be \"<sso-key>:<sso-secret>\" (both parts non-empty); see docs/providers/godaddy.md")
	}
	return &godaddyAdapter{provider: &libdnsgd.Provider{APIToken: token}}, nil
}

func (a *godaddyAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs, err := a.provider.GetRecords(ctx, zone)
	if err != nil {
		return nil, fmt.Errorf("godaddy: get records: %w (creds redacted)", err)
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

func (a *godaddyAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs := make([]libdns.Record, len(values))
	for i, v := range values {
		recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
	}
	if _, err := a.provider.SetRecords(ctx, zone, recs); err != nil {
		return fmt.Errorf("godaddy: upsert TXT: %w (creds redacted)", err)
	}
	return nil
}

func (a *godaddyAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
	if priority < 0 {
		return "", fmt.Errorf("godaddy: priority must be >= 0, got %d", priority)
	}
	recs := []libdns.Record{libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}}
	res, err := a.provider.SetRecords(ctx, zone, recs)
	if err != nil {
		return "", fmt.Errorf("godaddy: upsert record: %w (creds redacted)", err)
	}
	if len(res) > 0 {
		return res[0].RR().Name, nil
	}
	return "", nil
}

func (a *godaddyAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	recs := []libdns.Record{libdns.RR{Type: recordType, Name: name}}
	if _, err := a.provider.DeleteRecords(ctx, zone, recs); err != nil {
		return fmt.Errorf("godaddy: delete record: %w (creds redacted)", err)
	}
	return nil
}
