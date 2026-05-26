package dnsprovider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	"github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient"
)

var _ dnspolicy.Adapter = (*hoverAdapter)(nil)

func init() { Register("hover", newHoverAdapter) }

// hoverClientIface is the minimum pkg/hoverclient surface hoverAdapter consumes.
// Production holds *hoverclient.Client; tests inject stubs.
type hoverClientIface interface {
	GetDomain(ctx context.Context, domain string) (*hoverclient.Domain, error)
	ListRecords(ctx context.Context, domain string) ([]hoverclient.DNSRecord, error)
	CreateRecord(ctx context.Context, domainID string, rec hoverclient.DNSRecord) (*hoverclient.DNSRecord, error)
	UpdateRecord(ctx context.Context, recordID string, rec hoverclient.DNSRecord) error
	DeleteRecord(ctx context.Context, recordID string) error
}

var _ hoverClientIface = (*hoverclient.Client)(nil)

type hoverAdapter struct {
	client hoverClientIface
}

func newHoverAdapter(creds map[string]string) (dnspolicy.Adapter, error) {
	c := ExpandCredsMap(creds)
	username, password := c["username"], c["password"]
	if username == "" {
		return nil, fmt.Errorf("hover: missing creds.username (see docs/providers/hover.md)")
	}
	if password == "" {
		return nil, fmt.Errorf("hover: missing creds.password (see docs/providers/hover.md)")
	}
	hc := hoverclient.Credentials{Username: username, Password: password}
	if raw := c["totp_secret"]; raw != "" {
		ts, err := hoverclient.ParseBase32(raw)
		if err != nil {
			return nil, fmt.Errorf("hover: invalid creds.totp_secret: %w (creds redacted)", err)
		}
		hc.TOTPSecret = ts
	}
	client, err := hoverclient.NewClient(hc, (*http.Client)(nil))
	if err != nil {
		return nil, fmt.Errorf("hover: client init: %w (creds redacted)", err)
	}
	return &hoverAdapter{client: client}, nil
}

func (a *hoverAdapter) GetTXT(ctx context.Context, name string) ([]string, error) {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	recs, err := a.client.ListRecords(ctx, zone)
	if err != nil {
		return nil, fmt.Errorf("hover: list records: %w (creds redacted)", err)
	}
	var out []string
	for _, r := range recs {
		if r.Type == "TXT" && r.Name == relName {
			out = append(out, r.Content)
		}
	}
	return out, nil
}

// UpsertTXT emulates RRset-replace (Hover has no batch primitive):
// list → delete TXT@relName → create each desired value.
func (a *hoverAdapter) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
	zone := zoneFromPolicyName(name)
	relName := relativeNameFromFQDN(name, zone)
	dom, err := a.client.GetDomain(ctx, zone)
	if err != nil {
		return fmt.Errorf("hover: get domain: %w (creds redacted)", err)
	}
	existing, err := a.client.ListRecords(ctx, zone)
	if err != nil {
		return fmt.Errorf("hover: list records: %w (creds redacted)", err)
	}
	for _, r := range existing {
		if r.Type == "TXT" && r.Name == relName {
			if err := a.client.DeleteRecord(ctx, r.ID); err != nil {
				return fmt.Errorf("hover: delete stale TXT: %w (creds redacted)", err)
			}
		}
	}
	for _, v := range values {
		if _, err := a.client.CreateRecord(ctx, dom.ID, hoverclient.DNSRecord{
			Type: "TXT", Name: relName, Content: v, TTL: ttl,
		}); err != nil {
			return fmt.Errorf("hover: create TXT: %w (creds redacted)", err)
		}
	}
	return nil
}

func (a *hoverAdapter) UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (string, error) {
	if priority < 0 {
		return "", fmt.Errorf("hover: priority must be >= 0, got %d", priority)
	}
	// Note: priority dropped for non-MX/SRV (matches v1 precedent + Hover DNSRecord has no Priority field).
	dom, err := a.client.GetDomain(ctx, zone)
	if err != nil {
		return "", fmt.Errorf("hover: get domain: %w (creds redacted)", err)
	}
	rec, err := a.client.CreateRecord(ctx, dom.ID, hoverclient.DNSRecord{
		Type: recordType, Name: name, Content: data, TTL: int(ttl),
	})
	if err != nil {
		return "", fmt.Errorf("hover: upsert record: %w (creds redacted)", err)
	}
	return rec.ID, nil
}

func (a *hoverAdapter) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	existing, err := a.client.ListRecords(ctx, zone)
	if err != nil {
		return fmt.Errorf("hover: list records: %w (creds redacted)", err)
	}
	for _, r := range existing {
		if r.Type == recordType && r.Name == name {
			if err := a.client.DeleteRecord(ctx, r.ID); err != nil {
				return fmt.Errorf("hover: delete record: %w (creds redacted)", err)
			}
		}
	}
	return nil
}
