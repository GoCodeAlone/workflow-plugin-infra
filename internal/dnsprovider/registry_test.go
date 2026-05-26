package dnsprovider

import (
	"errors"
	"strings"
	"testing"
)

func TestNewAdapter_UnknownProviderListsSupported(t *testing.T) {
	_, err := NewAdapter("nonexistent", map[string]string{})
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("want ErrUnknownProvider, got %v", err)
	}
	// After PR 1 the supported list must include the v1 providers we re-registered.
	msg := err.Error()
	for _, want := range []string{"digitalocean", "cloudflare", "route53"} {
		if !strings.Contains(msg, want) {
			t.Errorf("supported list missing %q in error: %s", want, msg)
		}
	}
}

func TestNewAdapter_RegistryIsSorted(t *testing.T) {
	// supportedList sorts keys for deterministic error messages.
	list := supportedList()
	parts := strings.Split(list, ", ")
	if len(parts) < 2 {
		t.Fatalf("registry empty or single-entry (sort test is meaningless): %v", parts)
	}
	for i := 1; i < len(parts); i++ {
		if parts[i-1] >= parts[i] {
			t.Errorf("supported list not sorted: %v", parts)
			break
		}
	}
}
