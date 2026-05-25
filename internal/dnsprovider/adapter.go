package dnsprovider

import (
	"errors"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
)

// ErrUnknownProvider is returned when NewAdapter receives a provider name
// that has no registered adapter.
var ErrUnknownProvider = errors.New("dnsprovider: unknown provider")

// NewAdapter dispatches on provider name (case-folded) + creds map.
// v1 supports digitalocean + cloudflare. Unknown providers return
// ErrUnknownProvider with the supported list.
func NewAdapter(provider string, creds map[string]string) (dnspolicy.Adapter, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "digitalocean":
		return newDigitalOceanAdapter(creds)
	case "cloudflare":
		return newCloudflareAdapter(creds)
	default:
		return nil, fmt.Errorf("%w %q (supported: digitalocean, cloudflare)", ErrUnknownProvider, provider)
	}
}
