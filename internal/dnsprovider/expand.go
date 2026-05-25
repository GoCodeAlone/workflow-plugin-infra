package dnsprovider

import "os"

// ExpandCredsMap applies os.ExpandEnv to each value.
// Template-form ('{{ env "X" }}') is pre-resolved by the engine;
// this catches bare-shell form ('$X' or '${X}').
// EXPORTED: called from internal/plugin.go step handler.
func ExpandCredsMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = os.ExpandEnv(v)
	}
	return out
}
