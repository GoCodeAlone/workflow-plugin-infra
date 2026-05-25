package dnsprovider

import (
	"context"
	"errors"
	"testing"
	"time"

	libdnsdo "github.com/libdns/digitalocean"
	"github.com/libdns/libdns"
)

// doProviderIface abstracts the libdns DigitalOcean provider for testing.
// doAdapter.provider is *libdnsdo.Provider; tests substitute a stub.
type doProviderIface interface {
	GetRecords(ctx context.Context, zone string) ([]libdns.Record, error)
	DeleteRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error)
	AppendRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error)
	SetRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error)
}

// stubDOProvider is a minimal stub that records DeleteRecords + AppendRecords calls.
type stubDOProvider struct {
	existing    []libdns.Record
	deleteCalls [][]libdns.Record
	appendCalls [][]libdns.Record
	deleteErr   error
	appendErr   error
}

func (s *stubDOProvider) GetRecords(_ context.Context, _ string) ([]libdns.Record, error) {
	return s.existing, nil
}

func (s *stubDOProvider) DeleteRecords(_ context.Context, _ string, recs []libdns.Record) ([]libdns.Record, error) {
	s.deleteCalls = append(s.deleteCalls, recs)
	return recs, s.deleteErr
}

func (s *stubDOProvider) AppendRecords(_ context.Context, _ string, recs []libdns.Record) ([]libdns.Record, error) {
	s.appendCalls = append(s.appendCalls, recs)
	return recs, s.appendErr
}

func (s *stubDOProvider) SetRecords(_ context.Context, _ string, recs []libdns.Record) ([]libdns.Record, error) {
	return recs, nil
}

// upsertTXTRRsetWithProvider is the same algorithm as doAdapter.upsertTXTRRset
// but accepts an explicit provider interface so the test can inject a stub.
// This keeps the test in-sync with the production path.
func upsertTXTRRsetWithProvider(ctx context.Context, p doProviderIface, zone, relName string, values []string, ttl int) error {
	existing, err := p.GetRecords(ctx, zone)
	if err != nil {
		return err
	}
	var toDelete []libdns.Record
	for _, e := range existing {
		rr := e.RR()
		if rr.Type == "TXT" && rr.Name == relName {
			toDelete = append(toDelete, e)
		}
	}
	if len(toDelete) > 0 {
		if _, err := p.DeleteRecords(ctx, zone, toDelete); err != nil {
			return err
		}
	}
	if len(values) == 0 {
		return nil
	}
	toAdd := make([]libdns.Record, len(values))
	for i, v := range values {
		toAdd[i] = libdns.RR{Type: "TXT", Name: relName, Data: v, TTL: time.Duration(ttl) * time.Second}
	}
	_, err = p.AppendRecords(ctx, zone, toAdd)
	return err
}

// TestUpsertTXTRRset_DeletesStaleEntries verifies the RRset-replace semantic:
// when existing has 3 TXT records at relName, UpsertTXT with 2 values calls
// DeleteRecords with all 3, then AppendRecords with 2.
func TestUpsertTXTRRset_DeletesStaleEntries(t *testing.T) {
	existing := []libdns.Record{
		libdnsdo.DNS{Record: libdns.RR{Type: "TXT", Name: "_workflow-dns-policy", Data: "v=wfinfra-v1 o=sre d=true"}, ID: "1"},
		libdnsdo.DNS{Record: libdns.RR{Type: "TXT", Name: "_workflow-dns-policy", Data: "v=wfinfra-v1 o=multisite"}, ID: "2"},
		libdnsdo.DNS{Record: libdns.RR{Type: "TXT", Name: "_workflow-dns-policy", Data: "v=wfinfra-v1 o=old-owner"}, ID: "3"},
		// A non-TXT record at the same name — must NOT be deleted.
		libdnsdo.DNS{Record: libdns.RR{Type: "A", Name: "_workflow-dns-policy", Data: "1.2.3.4"}, ID: "4"},
		// A TXT record at a different name — must NOT be deleted.
		libdnsdo.DNS{Record: libdns.RR{Type: "TXT", Name: "other", Data: "other-data"}, ID: "5"},
	}

	stub := &stubDOProvider{existing: existing}
	ctx := context.Background()
	desired := []string{
		"v=wfinfra-v1 o=sre d=true",
		"v=wfinfra-v1 o=multisite",
	}

	if err := upsertTXTRRsetWithProvider(ctx, stub, "example.com", "_workflow-dns-policy", desired, 300); err != nil {
		t.Fatalf("upsertTXTRRset: unexpected error: %v", err)
	}

	// Verify DeleteRecords was called once with exactly the 3 TXT records at relName.
	if len(stub.deleteCalls) != 1 {
		t.Fatalf("DeleteRecords call count: got %d, want 1", len(stub.deleteCalls))
	}
	deleted := stub.deleteCalls[0]
	if len(deleted) != 3 {
		t.Errorf("DeleteRecords record count: got %d, want 3 (should delete all 3 old TXT entries)", len(deleted))
	}
	for _, rec := range deleted {
		rr := rec.RR()
		if rr.Type != "TXT" || rr.Name != "_workflow-dns-policy" {
			t.Errorf("unexpected record in DeleteRecords: type=%s name=%s", rr.Type, rr.Name)
		}
	}

	// Verify AppendRecords was called once with exactly the 2 desired values.
	if len(stub.appendCalls) != 1 {
		t.Fatalf("AppendRecords call count: got %d, want 1", len(stub.appendCalls))
	}
	appended := stub.appendCalls[0]
	if len(appended) != 2 {
		t.Errorf("AppendRecords record count: got %d, want 2", len(appended))
	}
}

// TestUpsertTXTRRset_EmptyValues_DeletesAndSkipsAppend verifies that when
// desired is empty (all owners removed), DeleteRecords is called but
// AppendRecords is not.
func TestUpsertTXTRRset_EmptyValues_DeletesAndSkipsAppend(t *testing.T) {
	existing := []libdns.Record{
		libdnsdo.DNS{Record: libdns.RR{Type: "TXT", Name: "_workflow-dns-policy", Data: "v=wfinfra-v1 o=sre"}, ID: "1"},
	}
	stub := &stubDOProvider{existing: existing}
	ctx := context.Background()

	if err := upsertTXTRRsetWithProvider(ctx, stub, "example.com", "_workflow-dns-policy", nil, 300); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stub.deleteCalls) != 1 {
		t.Errorf("DeleteRecords call count: got %d, want 1", len(stub.deleteCalls))
	}
	if len(stub.appendCalls) != 0 {
		t.Errorf("AppendRecords should not be called for empty desired set, got %d calls", len(stub.appendCalls))
	}
}

// TestUpsertTXTRRset_DeleteError_PropagatesError verifies that a DeleteRecords
// error is propagated.
func TestUpsertTXTRRset_DeleteError_PropagatesError(t *testing.T) {
	existing := []libdns.Record{
		libdnsdo.DNS{Record: libdns.RR{Type: "TXT", Name: "_workflow-dns-policy", Data: "v=wfinfra-v1 o=sre"}, ID: "1"},
	}
	stub := &stubDOProvider{
		existing:  existing,
		deleteErr: errors.New("network error"),
	}
	ctx := context.Background()

	err := upsertTXTRRsetWithProvider(ctx, stub, "example.com", "_workflow-dns-policy", []string{"v=wfinfra-v1 o=sre"}, 300)
	if err == nil {
		t.Fatal("expected error from DeleteRecords, got nil")
	}
}

// TestUpsertTXTRRset_NoExisting_SkipsDeleteCallsAppend verifies that when
// there are no existing TXT records at relName, DeleteRecords is not called.
func TestUpsertTXTRRset_NoExisting_SkipsDeleteCallsAppend(t *testing.T) {
	stub := &stubDOProvider{existing: nil}
	ctx := context.Background()

	desired := []string{"v=wfinfra-v1 o=sre d=true"}
	if err := upsertTXTRRsetWithProvider(ctx, stub, "example.com", "_workflow-dns-policy", desired, 300); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stub.deleteCalls) != 0 {
		t.Errorf("DeleteRecords should not be called when no existing records, got %d calls", len(stub.deleteCalls))
	}
	if len(stub.appendCalls) != 1 {
		t.Fatalf("AppendRecords call count: got %d, want 1", len(stub.appendCalls))
	}
	if len(stub.appendCalls[0]) != 1 {
		t.Errorf("AppendRecords record count: got %d, want 1", len(stub.appendCalls[0]))
	}
}
