package dnsprovider

import (
	"context"
	"strings"
	"testing"

	libdnsazure "github.com/libdns/azure"
	"github.com/libdns/libdns"
)

func TestNewAzureAdapter_RequiresSubscription(t *testing.T) {
	_, err := newAzureAdapter(map[string]string{"resource_group_name": "rg"})
	if err == nil || !strings.Contains(err.Error(), "creds.subscription_id") {
		t.Errorf("want missing-subscription_id, got %v", err)
	}
}

func TestNewAzureAdapter_RequiresResourceGroup(t *testing.T) {
	_, err := newAzureAdapter(map[string]string{"subscription_id": "sub"})
	if err == nil || !strings.Contains(err.Error(), "creds.resource_group_name") {
		t.Errorf("want missing-resource_group_name, got %v", err)
	}
}

func TestNewAzureAdapter_OnlySubAndRGRequiredAtConstruction(t *testing.T) {
	// Empty service-principal triple → libdns resolves managed-identity at first API call.
	a, err := newAzureAdapter(map[string]string{"subscription_id": "sub", "resource_group_name": "rg"})
	if err != nil {
		t.Fatalf("sub+rg-only construction rejected: %v", err)
	}
	if a == nil {
		t.Fatal("nil adapter")
	}
}

func TestNewAzureAdapter_ServicePrincipalMode(t *testing.T) {
	a, err := newAzureAdapter(map[string]string{
		"subscription_id": "sub", "resource_group_name": "rg",
		"tenant_id": "t", "client_id": "c", "client_secret": "s",
	})
	if err != nil {
		t.Fatalf("SP mode: %v", err)
	}
	az := a.(*azureAdapter)
	p, ok := az.provider.(*libdnsazure.Provider)
	if !ok {
		t.Fatalf("provider field is not *libdnsazure.Provider: %T", az.provider)
	}
	if p.TenantId != "t" || p.ClientId != "c" || p.ClientSecret != "s" {
		t.Errorf("SP fields wrong: %+v", p)
	}
}

func TestNewAzureAdapter_PartialSPRejected(t *testing.T) {
	_, err := newAzureAdapter(map[string]string{
		"subscription_id": "sub", "resource_group_name": "rg",
		"tenant_id": "t", "client_id": "c", // client_secret missing
	})
	if err == nil || !strings.Contains(err.Error(), "client_secret") {
		t.Errorf("want partial-SP rejection naming client_secret, got %v", err)
	}
}

func TestNewAdapter_AzureDispatch(t *testing.T) {
	a, err := NewAdapter("azuredns", map[string]string{"subscription_id": "s", "resource_group_name": "rg"})
	if err != nil || a == nil {
		t.Fatalf("dispatch azure: %v / nil=%v", err, a == nil)
	}
}

// Stub satisfies azProviderIface (defined in azure.go production file).
type stubAzProvider struct{ setCalls [][]libdns.Record }

func (s *stubAzProvider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) {
	return nil, nil
}
func (s *stubAzProvider) SetRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	s.setCalls = append(s.setCalls, r)
	return r, nil
}
func (s *stubAzProvider) DeleteRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	return r, nil
}
func (s *stubAzProvider) AppendRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	return r, nil
}

func TestAzureAdapter_UpsertTXT_ExercisesAdapter(t *testing.T) {
	stub := &stubAzProvider{}
	a := &azureAdapter{provider: stub}
	if err := a.UpsertTXT(context.Background(), "_workflow-dns-policy.example.com", []string{"v=wfinfra-v1 o=sre"}, 300); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(stub.setCalls) != 1 {
		t.Errorf("SetRecords calls: %d", len(stub.setCalls))
	}
}

func TestAzureAdapter_UpsertRecord_PriorityDroppedForA(t *testing.T) {
	stub := &stubAzProvider{}
	a := &azureAdapter{provider: stub}
	if _, err := a.UpsertRecord(context.Background(), "example.com", "host", "A", "1.2.3.4", 300, 10); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(stub.setCalls) != 1 || stub.setCalls[0][0].RR().Type != "A" {
		t.Errorf("calls: %+v", stub.setCalls)
	}
}
