package dnsprovider

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
)

// ErrUnknownProvider is returned when NewAdapter receives a provider name
// that has no registered adapter.
var ErrUnknownProvider = errors.New("dnsprovider: unknown provider")

// AdapterFactory constructs a dnspolicy.Adapter from a creds map.
type AdapterFactory func(creds map[string]string) (dnspolicy.Adapter, error)

var (
	factoriesMu sync.RWMutex
	factories   = map[string]AdapterFactory{}
)

// Register registers an adapter factory for a provider key (case-folded).
// Called from each provider file's init().
func Register(provider string, f AdapterFactory) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	factories[strings.ToLower(strings.TrimSpace(provider))] = f
}

// NewAdapter dispatches on provider name (case-folded) + creds map.
// Providers self-register via init(). Unknown providers return
// ErrUnknownProvider with the supported list.
func NewAdapter(provider string, creds map[string]string) (dnspolicy.Adapter, error) {
	key := strings.ToLower(strings.TrimSpace(provider))
	factoriesMu.RLock()
	f, ok := factories[key]
	factoriesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w %q (supported: %s)", ErrUnknownProvider, provider, supportedList())
	}
	return f(creds)
}

func supportedList() string {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	keys := make([]string, 0, len(factories))
	for k := range factories {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
