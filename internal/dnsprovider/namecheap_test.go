package dnsprovider

import (
	"context"
	"strings"
	"testing"

	"github.com/libdns/libdns"
	libdnsnc "github.com/libdns/namecheap"
)

func TestNewNamecheapAdapter_RequiresKeys(t *testing.T) {
	cases := []struct {
		in   map[string]string
		want string
	}{
		{map[string]string{"user": "u", "client_ip": "1.2.3.4"}, "creds.api_key"},
		{map[string]string{"api_key": "k", "client_ip": "1.2.3.4"}, "creds.user"},
		{map[string]string{"api_key": "k", "user": "u"}, "creds.client_ip"},
	}
	for _, tc := range cases {
		_, err := newNamecheapAdapter(tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("input=%v want %q in error, got %v", tc.in, tc.want, err)
		}
	}
}

func TestNewNamecheapAdapter_MapsFieldsExact(t *testing.T) {
	a, err := newNamecheapAdapter(map[string]string{
		"api_key": "k", "user": "u", "client_ip": "1.2.3.4",
		"api_endpoint": "https://api.sandbox.namecheap.com/xml.response",
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	n := a.(*namecheapAdapter)
	p, ok := n.provider.(*libdnsnc.Provider)
	if !ok {
		t.Fatalf("provider field is not *libdnsnc.Provider: %T", n.provider)
	}
	if p.APIKey != "k" || p.User != "u" || p.ClientIP != "1.2.3.4" ||
		p.APIEndpoint != "https://api.sandbox.namecheap.com/xml.response" {
		t.Errorf("fields: %+v", p)
	}
}

func TestNewAdapter_NamecheapDispatch(t *testing.T) {
	a, err := NewAdapter("namecheap", map[string]string{"api_key": "k", "user": "u", "client_ip": "1.2.3.4"})
	if err != nil || a == nil {
		t.Fatalf("dispatch namecheap: %v / nil=%v", err, a == nil)
	}
}

// Stub satisfies ncProviderIface (defined in namecheap.go production file).
type stubNCProvider struct{ setCalls [][]libdns.Record }

func (s *stubNCProvider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) {
	return nil, nil
}
func (s *stubNCProvider) SetRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	s.setCalls = append(s.setCalls, r)
	return r, nil
}
func (s *stubNCProvider) AppendRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	return r, nil
}
func (s *stubNCProvider) DeleteRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	return r, nil
}

func TestNamecheapAdapter_UpsertTXT_ExercisesAdapter(t *testing.T) {
	stub := &stubNCProvider{}
	a := &namecheapAdapter{provider: stub}
	if err := a.UpsertTXT(context.Background(), "_workflow-dns-policy.example.com", []string{"v=wfinfra-v1 o=sre"}, 300); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(stub.setCalls) != 1 {
		t.Errorf("SetRecords calls: %d, want 1", len(stub.setCalls))
	}
}

func TestNamecheapAdapter_UpsertRecord_PriorityDroppedForA(t *testing.T) {
	stub := &stubNCProvider{}
	a := &namecheapAdapter{provider: stub}
	if _, err := a.UpsertRecord(context.Background(), "example.com", "host", "A", "1.2.3.4", 300, 10); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(stub.setCalls) != 1 || stub.setCalls[0][0].RR().Type != "A" {
		t.Errorf("calls: %+v", stub.setCalls)
	}
}
