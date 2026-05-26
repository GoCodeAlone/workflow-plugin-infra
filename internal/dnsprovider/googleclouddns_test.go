package dnsprovider

import (
	"context"
	"strings"
	"testing"

	libdnsgcp "github.com/libdns/googleclouddns"
	"github.com/libdns/libdns"
)

func TestNewGCPAdapter_RequiresProject(t *testing.T) {
	_, err := newGoogleCloudDNSAdapter(map[string]string{"service_account_path": "/tmp/sa.json"})
	if err == nil || !strings.Contains(err.Error(), "creds.gcp_project") {
		t.Errorf("want missing-project error, got %v", err)
	}
}

func TestNewGCPAdapter_OnlyProjectRequiredAtConstruction(t *testing.T) {
	a, err := newGoogleCloudDNSAdapter(map[string]string{"gcp_project": "proj-x"})
	if err != nil {
		t.Fatalf("project-only construction rejected: %v", err)
	}
	if a == nil {
		t.Fatal("nil adapter")
	}
}

func TestNewGCPAdapter_MapsFieldsExact(t *testing.T) {
	a, err := newGoogleCloudDNSAdapter(map[string]string{
		"gcp_project": "proj-x", "service_account_path": "/etc/secrets/sa.json",
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	ga := a.(*gcpAdapter)
	p, ok := ga.provider.(*libdnsgcp.Provider)
	if !ok {
		t.Fatalf("provider field is not *libdnsgcp.Provider: %T", ga.provider)
	}
	if p.Project != "proj-x" {
		t.Errorf("Project: %q", p.Project)
	}
	if p.ServiceAccountJSON != "/etc/secrets/sa.json" {
		t.Errorf("ServiceAccountJSON: %q", p.ServiceAccountJSON)
	}
}

func TestNewAdapter_GCPDispatch(t *testing.T) {
	a, err := NewAdapter("googleclouddns", map[string]string{"gcp_project": "p"})
	if err != nil || a == nil {
		t.Fatalf("dispatch gcp: %v / nil=%v", err, a == nil)
	}
}

// Stub satisfies gcpProviderIface (defined in googleclouddns.go production file).
type stubGCPProvider struct{ setCalls [][]libdns.Record }

func (s *stubGCPProvider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) {
	return nil, nil
}
func (s *stubGCPProvider) SetRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	s.setCalls = append(s.setCalls, r)
	return r, nil
}
func (s *stubGCPProvider) DeleteRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	return r, nil
}
func (s *stubGCPProvider) AppendRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	return r, nil
}

func TestGCPAdapter_UpsertTXT_ExercisesAdapter(t *testing.T) {
	stub := &stubGCPProvider{}
	a := &gcpAdapter{provider: stub}
	if err := a.UpsertTXT(context.Background(), "_workflow-dns-policy.example.com", []string{"v=wfinfra-v1 o=sre"}, 300); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(stub.setCalls) != 1 {
		t.Errorf("SetRecords calls: %d, want 1", len(stub.setCalls))
	}
}

func TestGCPAdapter_UpsertRecord_PriorityDroppedForA(t *testing.T) {
	stub := &stubGCPProvider{}
	a := &gcpAdapter{provider: stub}
	if _, err := a.UpsertRecord(context.Background(), "example.com", "host", "A", "1.2.3.4", 300, 10); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(stub.setCalls) != 1 || stub.setCalls[0][0].RR().Type != "A" {
		t.Errorf("calls: %+v", stub.setCalls)
	}
}
