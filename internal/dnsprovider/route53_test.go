package dnsprovider

import (
	"context"
	"strings"
	"testing"

	"github.com/libdns/libdns"
	libdnsr53 "github.com/libdns/route53"
)

func TestNewRoute53Adapter_RequiresRegion(t *testing.T) {
	_, err := newRoute53Adapter(map[string]string{
		"access_key_id":     "AKIA",
		"secret_access_key": "secret",
	})
	if err == nil || !strings.Contains(err.Error(), "creds.region") {
		t.Errorf("want missing-region error, got %v", err)
	}
}

func TestNewRoute53Adapter_OnlyRegionRequiredAtConstruction(t *testing.T) {
	// Adapter accepts region-only creds; libdns lazy-resolves AWS env/role at first API call.
	a, err := newRoute53Adapter(map[string]string{"region": "us-east-1"})
	if err != nil {
		t.Fatalf("region-only construction rejected: %v", err)
	}
	if a == nil {
		t.Fatal("nil adapter")
	}
}

func TestNewRoute53Adapter_MapsFieldsExact(t *testing.T) {
	// After cycle-4 refactor, a.provider is r53ProviderIface; type-assert to *libdnsr53.Provider for field inspection.
	a, err := newRoute53Adapter(map[string]string{
		"region": "us-east-1", "access_key_id": "AKIA",
		"secret_access_key": "secret", "session_token": "tok", "profile": "p",
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	ra := a.(*route53Adapter)
	p, ok := ra.provider.(*libdnsr53.Provider)
	if !ok {
		t.Fatalf("provider field is not *libdnsr53.Provider: %T", ra.provider)
	}
	want := map[string]string{
		"Region": p.Region, "AccessKeyId": p.AccessKeyId,
		"SecretAccessKey": p.SecretAccessKey, "SessionToken": p.SessionToken, "Profile": p.Profile,
	}
	expect := map[string]string{"Region": "us-east-1", "AccessKeyId": "AKIA", "SecretAccessKey": "secret", "SessionToken": "tok", "Profile": "p"}
	for k, v := range expect {
		if want[k] != v {
			t.Errorf("%s: got %q want %q", k, want[k], v)
		}
	}
}

func TestNewAdapter_Route53Dispatch(t *testing.T) {
	a, err := NewAdapter("route53", map[string]string{"region": "us-east-1"})
	if err != nil || a == nil {
		t.Fatalf("dispatch route53: %v / nil=%v", err, a == nil)
	}
	a2, err := NewAdapter("Route53", map[string]string{"region": "us-east-1"})
	if err != nil || a2 == nil {
		t.Fatalf("case-fold Route53: %v / nil=%v", err, a2 == nil)
	}
}

// Stub satisfies r53ProviderIface (defined in route53.go production file).
type stubR53Provider struct {
	existing []libdns.Record
	setCalls [][]libdns.Record
	delCalls [][]libdns.Record
}

func (s *stubR53Provider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) {
	return s.existing, nil
}
func (s *stubR53Provider) SetRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	s.setCalls = append(s.setCalls, r)
	return r, nil
}
func (s *stubR53Provider) DeleteRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	s.delCalls = append(s.delCalls, r)
	return r, nil
}
func (s *stubR53Provider) AppendRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	return r, nil
}

// Exercises the REAL adapter method (not a helper). zone-derivation, name-derivation,
// TTL conversion, error wrapping all execute against the stub.
func TestRoute53Adapter_UpsertTXT_ExercisesAdapter(t *testing.T) {
	stub := &stubR53Provider{}
	a := &route53Adapter{provider: stub}
	err := a.UpsertTXT(context.Background(), "_workflow-dns-policy.example.com", []string{"v=wfinfra-v1 o=sre"}, 300)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(stub.setCalls) != 1 {
		t.Fatalf("SetRecords calls: %d, want 1", len(stub.setCalls))
	}
	if len(stub.setCalls[0]) != 1 {
		t.Fatalf("payload len: %d, want 1", len(stub.setCalls[0]))
	}
	rr := stub.setCalls[0][0].RR()
	if rr.Type != "TXT" || rr.Data != "v=wfinfra-v1 o=sre" {
		t.Errorf("payload wrong: type=%s data=%s", rr.Type, rr.Data)
	}
}

// Closes plan-cycle-3 I3-6: priority dropped for non-MX/SRV; no error.
func TestRoute53Adapter_UpsertRecord_PriorityDroppedForA(t *testing.T) {
	stub := &stubR53Provider{}
	a := &route53Adapter{provider: stub}
	_, err := a.UpsertRecord(context.Background(), "example.com", "host", "A", "1.2.3.4", 300, 10)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(stub.setCalls) != 1 || len(stub.setCalls[0]) != 1 {
		t.Fatalf("calls: %+v", stub.setCalls)
	}
	rr := stub.setCalls[0][0].RR()
	if rr.Type != "A" || rr.Data != "1.2.3.4" {
		t.Errorf("payload: type=%s data=%s", rr.Type, rr.Data)
	}
}
