package dnsprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	libdnsazure "github.com/libdns/azure"
	"github.com/libdns/libdns"
)

var _ dnspolicy.Adapter = (*azureAdapter)(nil)

func init() { Register("azuredns", newAzureAdapter) }

type azProviderIface interface {
	GetRecords(context.Context, string) ([]libdns.Record, error)
	SetRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
	DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
	AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error)
}

var _ azProviderIface = (*libdnsazure.Provider)(nil)

type azureAdapter struct {
	provider azProviderIface
}

func newAzureAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
	c := ExpandCredsMap(creds)
	sub := c["subscription_id"]
	rg := c["resource_group_name"]
	if sub == "" {
		return nil, fmt.Errorf("azuredns: missing creds.subscription_id (see docs/providers/azuredns.md)")
	}
	if rg == "" {
		return nil, fmt.Errorf("azuredns: missing creds.resource_group_name (see docs/providers/azuredns.md)")
	}
	tenant, client, secret := c["tenant_id"], c["client_id"], c["client_secret"]
	setCount := 0
	for _, v := range []string{tenant, client, secret} {
		if v != "" {
			setCount++
		}
	}
	if setCount != 0 && setCount != 3 {
		var missing []string
		if tenant == "" {
			missing = append(missing, "tenant_id")
		}
		if client == "" {
			missing = append(missing, "client_id")
		}
		if secret == "" {
			missing = append(missing, "client_secret")
		}
		return nil, fmt.Errorf("azuredns: tenant_id/client_id/client_secret must all be set (service-principal) or all empty (managed-identity); missing: %s", strings.Join(missing, ","))
	}
	return &azureAdapter{provider: &libdnsazure.Provider{
		SubscriptionId:    sub,
		ResourceGroupName: rg,
		TenantId:          tenant,
		ClientId:          client,
		ClientSecret:      secret,
	}}, nil
}

func (a *azureAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs, err := a.provider.GetRecords(ctx, zone)
	if err != nil {
		return nil, fmt.Errorf("azuredns: get records: %w (creds redacted)", err)
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

func (a *azureAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs := make([]libdns.Record, len(values))
	for i, v := range values {
		recs[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
	}
	if _, err := a.provider.SetRecords(ctx, zone, recs); err != nil {
		return fmt.Errorf("azuredns: upsert TXT: %w (creds redacted)", err)
	}
	return nil
}

func (a *azureAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
	if priority < 0 {
		return "", fmt.Errorf("azuredns: priority must be >= 0, got %d", priority)
	}
	recs := []libdns.Record{libdns.RR{Type: recordType, Name: name, Data: data, TTL: time.Duration(ttl) * time.Second}}
	res, err := a.provider.SetRecords(ctx, zone, recs)
	if err != nil {
		return "", fmt.Errorf("azuredns: upsert record: %w (creds redacted)", err)
	}
	if len(res) > 0 {
		return res[0].RR().Name, nil
	}
	return "", nil
}

func (a *azureAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	recs := []libdns.Record{libdns.RR{Type: recordType, Name: name}}
	if _, err := a.provider.DeleteRecords(ctx, zone, recs); err != nil {
		return fmt.Errorf("azuredns: delete record: %w (creds redacted)", err)
	}
	return nil
}
