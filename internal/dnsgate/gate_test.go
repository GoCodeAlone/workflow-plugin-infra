package dnsgate

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
)

type fakeReader struct {
	txtRRs []string
	err    error
}

func (f *fakeReader) GetTXT(_ context.Context, _ string) ([]string, error) {
	return f.txtRRs, f.err
}
func (f *fakeReader) UpsertTXT(_ context.Context, _ string, _ []string, _ int) error { return nil }

func TestGate_Allowed(t *testing.T) {
	reader := &fakeReader{txtRRs: []string{
		`heritage=wfinfra-v1 o=sre d=true`,
		`heritage=wfinfra-v1 o=multisite p=www,admin`,
	}}
	if err := Gate(context.Background(), reader, "z.com", "www", "A", "multisite"); err != nil {
		t.Errorf("expected pass, got %v", err)
	}
}

func TestGate_Denied(t *testing.T) {
	reader := &fakeReader{txtRRs: []string{
		`heritage=wfinfra-v1 o=sre d=true`,
		`heritage=wfinfra-v1 o=multisite p=www`,
	}}
	err := Gate(context.Background(), reader, "z.com", "bandname", "A", "multisite")
	if err == nil {
		t.Errorf("expected denial")
	}
}

func TestGate_FailClosedOnEmptyPolicy(t *testing.T) {
	reader := &fakeReader{txtRRs: []string{}}
	err := Gate(context.Background(), reader, "z.com", "www", "A", "anyone")
	if err == nil {
		t.Errorf("expected fail-closed when no policy exists")
	}
}

func TestGate_PropagatesParseError(t *testing.T) {
	reader := &fakeReader{txtRRs: []string{
		`heritage=wfinfra-v1 o=sre d=true`,
		`heritage=wfinfra-v1 o=multisite d=true p=www`, // two defaults
	}}
	err := Gate(context.Background(), reader, "z.com", "www", "A", "sre")
	if !errors.Is(err, dnspolicy.ErrMultipleDefaults) {
		t.Errorf("want ErrMultipleDefaults, got %v", err)
	}
}

type countingReader struct {
	txtRRs      []string
	callCounter *int
}

func (c *countingReader) GetTXT(_ context.Context, _ string) ([]string, error) {
	*c.callCounter++
	return c.txtRRs, nil
}
func (c *countingReader) UpsertTXT(_ context.Context, _ string, _ []string, _ int) error { return nil }

func TestCachingGate_OneGetTXTPerZone(t *testing.T) {
	calls := 0
	reader := &countingReader{txtRRs: []string{`heritage=wfinfra-v1 o=sre d=true`}, callCounter: &calls}
	g := NewCachingGate()
	for i := 0; i < 10; i++ {
		if err := g.Check(context.Background(), reader, "z.com", fmt.Sprintf("name%d", i), "A", "sre"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if calls != 1 {
		t.Errorf("want 1 GetTXT call across 10 Check invocations; got %d", calls)
	}
}
