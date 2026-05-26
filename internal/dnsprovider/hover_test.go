package dnsprovider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow-plugin-hover/pkg/hoverclient"
)

func TestNewHoverAdapter_RequiresUsername(t *testing.T) {
	_, err := newHoverAdapter(map[string]string{"password": "p"})
	if err == nil || !strings.Contains(err.Error(), "creds.username") {
		t.Errorf("want missing-username error, got %v", err)
	}
}

func TestNewHoverAdapter_RequiresPassword(t *testing.T) {
	_, err := newHoverAdapter(map[string]string{"username": "u"})
	if err == nil || !strings.Contains(err.Error(), "creds.password") {
		t.Errorf("want missing-password error, got %v", err)
	}
}

func TestNewHoverAdapter_AcceptsValidTOTP(t *testing.T) {
	a, err := newHoverAdapter(map[string]string{
		"username": "u", "password": "p", "totp_secret": "JBSWY3DPEHPK3PXP",
	})
	if err != nil {
		t.Fatalf("construct with TOTP: %v", err)
	}
	if a == nil {
		t.Fatal("nil adapter")
	}
}

func TestNewHoverAdapter_RejectsInvalidTOTP(t *testing.T) {
	_, err := newHoverAdapter(map[string]string{
		"username": "u", "password": "p", "totp_secret": "not-base32!",
	})
	if err == nil || !strings.Contains(err.Error(), "totp_secret") {
		t.Errorf("want invalid-totp error, got %v", err)
	}
}

func TestNewAdapter_HoverDispatch(t *testing.T) {
	a, err := NewAdapter("hover", map[string]string{"username": "u", "password": "p"})
	if err != nil || a == nil {
		t.Fatalf("dispatch hover: %v / nil=%v", err, a == nil)
	}
	a2, err := NewAdapter("Hover", map[string]string{"username": "u", "password": "p"})
	if err != nil || a2 == nil {
		t.Fatalf("case-fold Hover: %v / nil=%v", err, a2 == nil)
	}
}

// Stub satisfies hoverClientIface (defined in hover.go production file).
type stubHoverClient struct {
	domain          *hoverclient.Domain
	listResult      []hoverclient.DNSRecord
	listErr         error
	createCalls     []hoverclient.DNSRecord
	createDomainIDs []string
	createErr       error
	deleteIDs       []string
	deleteErr       error
}

func (s *stubHoverClient) GetDomain(_ context.Context, _ string) (*hoverclient.Domain, error) {
	if s.domain != nil {
		return s.domain, nil
	}
	return &hoverclient.Domain{ID: "dom123"}, nil
}

func (s *stubHoverClient) ListRecords(_ context.Context, _ string) ([]hoverclient.DNSRecord, error) {
	return s.listResult, s.listErr
}

func (s *stubHoverClient) CreateRecord(_ context.Context, domainID string, rec hoverclient.DNSRecord) (*hoverclient.DNSRecord, error) {
	s.createDomainIDs = append(s.createDomainIDs, domainID)
	s.createCalls = append(s.createCalls, rec)
	if s.createErr != nil {
		return nil, s.createErr
	}
	out := rec
	out.ID = "rec123"
	return &out, nil
}

func (s *stubHoverClient) UpdateRecord(_ context.Context, _ string, _ hoverclient.DNSRecord) error {
	return nil
}

func (s *stubHoverClient) DeleteRecord(_ context.Context, recordID string) error {
	s.deleteIDs = append(s.deleteIDs, recordID)
	return s.deleteErr
}

// Exercises REAL adapter method: list → delete stale TXT → create new values.
func TestHoverAdapter_UpsertTXT_ExercisesAdapter(t *testing.T) {
	stub := &stubHoverClient{
		listResult: []hoverclient.DNSRecord{
			{ID: "old1", Type: "TXT", Name: "_workflow-dns-policy", Content: "stale1"},
			{ID: "old2", Type: "TXT", Name: "_workflow-dns-policy", Content: "stale2"},
			{ID: "foreign", Type: "A", Name: "host", Content: "1.2.3.4"},
		},
	}
	a := &hoverAdapter{client: stub}
	err := a.UpsertTXT(context.Background(), "_workflow-dns-policy.example.com", []string{"v=wfinfra-v1 o=sre"}, 300)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(stub.deleteIDs) != 2 || stub.deleteIDs[0] != "old1" || stub.deleteIDs[1] != "old2" {
		t.Errorf("deleteIDs: %+v (want [old1, old2] — foreign A record must survive)", stub.deleteIDs)
	}
	if len(stub.createCalls) != 1 {
		t.Fatalf("create calls: %d, want 1", len(stub.createCalls))
	}
	got := stub.createCalls[0]
	if got.Type != "TXT" || got.Content != "v=wfinfra-v1 o=sre" {
		t.Errorf("created record wrong: %+v", got)
	}
	if stub.createDomainIDs[0] != "dom123" {
		t.Errorf("CreateRecord called with domainID=%q, want dom123 (from GetDomain)", stub.createDomainIDs[0])
	}
}

func TestHoverAdapter_GetTXT(t *testing.T) {
	stub := &stubHoverClient{
		listResult: []hoverclient.DNSRecord{
			{Type: "TXT", Name: "_workflow-dns-policy", Content: "v=wfinfra-v1 o=sre"},
			{Type: "TXT", Name: "_workflow-dns-policy", Content: "v=wfinfra-v1 o=multisite"},
			{Type: "TXT", Name: "other", Content: "should-not-appear"},
			{Type: "A", Name: "_workflow-dns-policy", Content: "1.2.3.4"},
		},
	}
	a := &hoverAdapter{client: stub}
	got, err := a.GetTXT(context.Background(), "_workflow-dns-policy.example.com")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 || got[0] != "v=wfinfra-v1 o=sre" || got[1] != "v=wfinfra-v1 o=multisite" {
		t.Errorf("GetTXT: %+v (want 2 policy TXT entries; non-TXT + other-name filtered)", got)
	}
}

func TestHoverAdapter_UpsertRecord_PriorityDroppedForA(t *testing.T) {
	stub := &stubHoverClient{}
	a := &hoverAdapter{client: stub}
	id, err := a.UpsertRecord(context.Background(), "example.com", "host", "A", "1.2.3.4", 300, 10)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if id != "rec123" {
		t.Errorf("returned ID: %q, want rec123", id)
	}
	if len(stub.createCalls) != 1 || stub.createCalls[0].Type != "A" || stub.createCalls[0].Content != "1.2.3.4" {
		t.Errorf("create: %+v", stub.createCalls)
	}
}

func TestHoverAdapter_UpsertRecord_RejectsNegativePriority(t *testing.T) {
	stub := &stubHoverClient{}
	a := &hoverAdapter{client: stub}
	_, err := a.UpsertRecord(context.Background(), "example.com", "host", "A", "1.2.3.4", 300, -1)
	if err == nil || !strings.Contains(err.Error(), "priority must be >= 0") {
		t.Errorf("want negative-priority error, got %v", err)
	}
}

func TestHoverAdapter_DeleteRecord(t *testing.T) {
	stub := &stubHoverClient{
		listResult: []hoverclient.DNSRecord{
			{ID: "del1", Type: "A", Name: "host", Content: "1.2.3.4"},
			{ID: "keep1", Type: "A", Name: "other", Content: "5.6.7.8"},
			{ID: "keep2", Type: "TXT", Name: "host", Content: "txt"},
		},
	}
	a := &hoverAdapter{client: stub}
	if err := a.DeleteRecord(context.Background(), "example.com", "host", "A"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(stub.deleteIDs) != 1 || stub.deleteIDs[0] != "del1" {
		t.Errorf("deleteIDs: %+v (want [del1] only — name+type match)", stub.deleteIDs)
	}
}

func TestHoverAdapter_DeleteRecord_PropagatesListError(t *testing.T) {
	stub := &stubHoverClient{listErr: errors.New("network unreachable")}
	a := &hoverAdapter{client: stub}
	err := a.DeleteRecord(context.Background(), "example.com", "host", "A")
	if err == nil || !strings.Contains(err.Error(), "list records") {
		t.Errorf("want list-records error, got %v", err)
	}
	if strings.Contains(err.Error(), "creds redacted") == false {
		t.Errorf("want (creds redacted) suffix, got %v", err)
	}
}
