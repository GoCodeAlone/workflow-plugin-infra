package admincli

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// sha256Strings computes a deterministic SHA256 hex digest of a sorted slice of strings.
// Used for audit log prior_sha256/new_sha256.
func sha256Strings(ss []string) string {
	sorted := append([]string(nil), ss...)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return fmt.Sprintf("%x", h)
}
