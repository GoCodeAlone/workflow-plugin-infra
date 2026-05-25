package dnsgate

import (
	"context"
	"fmt"
	"sync"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
)

// PolicyName returns the TXT name where policy lives for a zone.
func PolicyName(zone string) string { return "_workflow-dns-policy." + zone }

// policyCache holds per-zone parsed policies for the lifetime of one Gate
// holder (e.g. one wfctl apply invocation = one *Gate instance).
// Closes design "Open questions §3" + alignment-check missing-item (per-zone cache).
type policyCache struct {
	mu    sync.RWMutex
	zones map[string]*dnspolicy.Policy
}

// CachingGate is a Gate-call wrapper with per-zone caching.
// One *CachingGate per wfctl apply invocation; releases at end of invocation (no TTL).
type CachingGate struct{ c *policyCache }

// NewCachingGate returns a new CachingGate.
func NewCachingGate() *CachingGate {
	return &CachingGate{c: &policyCache{zones: map[string]*dnspolicy.Policy{}}}
}

// Check is the cached entry point used by infra.dns_record step handlers
// processing multiple records in one apply (single GetTXT per zone, not per record).
func (g *CachingGate) Check(ctx context.Context, reader dnspolicy.DNSPolicyReader, zone, name, recordType, owner string) error {
	g.c.mu.RLock()
	cached, ok := g.c.zones[zone]
	g.c.mu.RUnlock()
	if !ok {
		rrs, err := reader.GetTXT(ctx, PolicyName(zone))
		if err != nil {
			return fmt.Errorf("dnsgate: fetch policy: %w", err)
		}
		cached, err = dnspolicy.Parse(zone, rrs)
		if err != nil {
			return err
		}
		if len(cached.Entries) == 0 {
			return fmt.Errorf("dnsgate: fail-closed — no policy found at %s", PolicyName(zone))
		}
		g.c.mu.Lock()
		g.c.zones[zone] = cached
		g.c.mu.Unlock()
	}
	return cached.CheckAllowed(name, recordType, owner)
}

// Gate is the uncached entry point (one GetTXT per call). Use this for
// one-off invocations (CLI commands, integration tests). For step handlers
// processing many records in one apply, use NewCachingGate + Check.
func Gate(ctx context.Context, reader dnspolicy.DNSPolicyReader, zone, name, recordType, owner string) error {
	return NewCachingGate().Check(ctx, reader, zone, name, recordType, owner)
}
