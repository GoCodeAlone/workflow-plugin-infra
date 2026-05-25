package dnsprovider

import (
	"errors"
	"os"
	"testing"
)

func TestNewAdapter_UnknownProvider(t *testing.T) {
	_, err := NewAdapter("unknown", map[string]string{})
	if !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("want ErrUnknownProvider, got %v", err)
	}
}

func TestNewAdapter_CaseFold(t *testing.T) {
	a1, err1 := NewAdapter("DigitalOcean", map[string]string{"token": "x"})
	a2, err2 := NewAdapter("digitalocean", map[string]string{"token": "x"})
	if err1 != nil || err2 != nil {
		t.Fatalf("case-fold errors: %v / %v", err1, err2)
	}
	if a1 == nil || a2 == nil {
		t.Errorf("nil adapters")
	}
}

func TestExpandCredsMap(t *testing.T) {
	os.Setenv("DNS_TEST_TOKEN", "expanded-value")
	defer os.Unsetenv("DNS_TEST_TOKEN")
	in := map[string]string{"token": "$DNS_TEST_TOKEN", "literal": "raw"}
	out := ExpandCredsMap(in)
	if out["token"] != "expanded-value" {
		t.Errorf("got %q", out["token"])
	}
	if out["literal"] != "raw" {
		t.Errorf("got %q", out["literal"])
	}
}
