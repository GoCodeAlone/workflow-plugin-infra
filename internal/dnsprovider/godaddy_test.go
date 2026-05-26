package dnsprovider

import (
	"context"
	"strings"
	"testing"

	libdnsgd "github.com/libdns/godaddy"
	"github.com/libdns/libdns"
)

func TestNewGoDaddyAdapter_RequiresToken(t *testing.T) {
	_, err := newGoDaddyAdapter(map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "creds.api_token") {
		t.Errorf("want missing-api_token, got %v", err)
	}
}

func TestNewGoDaddyAdapter_RequiresColonFormatAndBothParts(t *testing.T) {
	// Closes cycle-3 I3-5: strengthen validation beyond colon-presence.
	cases := []struct{ in, want string }{
		{"bare-sso-key-no-colon", "<sso-key>:<sso-secret>"},
		{":", "<sso-key>:<sso-secret>"},
		{":foo", "<sso-key>:<sso-secret>"},
		{"foo:", "<sso-key>:<sso-secret>"},
	}
	for _, tc := range cases {
		_, err := newGoDaddyAdapter(map[string]string{"api_token": tc.in})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("token=%q want %q in error, got %v", tc.in, tc.want, err)
		}
	}
}

func TestNewGoDaddyAdapter_AcceptsConcatenatedToken(t *testing.T) {
	a, err := newGoDaddyAdapter(map[string]string{"api_token": "ssokey:ssosecret"})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	g := a.(*godaddyAdapter)
	p, ok := g.provider.(*libdnsgd.Provider)
	if !ok {
		t.Fatalf("provider field is not *libdnsgd.Provider: %T", g.provider)
	}
	if p.APIToken != "ssokey:ssosecret" {
		t.Errorf("APIToken: %q", p.APIToken)
	}
}

func TestNewAdapter_GoDaddyDispatch(t *testing.T) {
	a, err := NewAdapter("godaddy", map[string]string{"api_token": "k:s"})
	if err != nil || a == nil {
		t.Fatalf("dispatch godaddy: %v / nil=%v", err, a == nil)
	}
}

// Stub satisfies gdProviderIface (defined in godaddy.go production file).
type stubGDProvider struct{ setCalls [][]libdns.Record }

func (s *stubGDProvider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) {
	return nil, nil
}
func (s *stubGDProvider) SetRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	s.setCalls = append(s.setCalls, r)
	return r, nil
}
func (s *stubGDProvider) AppendRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	return r, nil
}
func (s *stubGDProvider) DeleteRecords(_ context.Context, _ string, r []libdns.Record) ([]libdns.Record, error) {
	return r, nil
}

func TestGoDaddyAdapter_UpsertTXT_ExercisesAdapter(t *testing.T) {
	stub := &stubGDProvider{}
	a := &godaddyAdapter{provider: stub}
	if err := a.UpsertTXT(context.Background(), "_workflow-dns-policy.example.com", []string{"v=wfinfra-v1 o=sre"}, 300); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(stub.setCalls) != 1 {
		t.Errorf("SetRecords calls: %d, want 1", len(stub.setCalls))
	}
}

func TestGoDaddyAdapter_UpsertRecord_PriorityDroppedForA(t *testing.T) {
	stub := &stubGDProvider{}
	a := &godaddyAdapter{provider: stub}
	if _, err := a.UpsertRecord(context.Background(), "example.com", "host", "A", "1.2.3.4", 300, 10); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(stub.setCalls) != 1 || stub.setCalls[0][0].RR().Type != "A" {
		t.Errorf("calls: %+v", stub.setCalls)
	}
}
