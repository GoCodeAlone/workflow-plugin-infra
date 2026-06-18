package cloudflarerecords

import (
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/record"
)

// EffectiveRecordSource strips known registrar parking web records from the
// selected source and backfills non-parked web records from alternate snapshots.
func EffectiveRecordSource(domain string, group []record.Snapshot, selected record.Snapshot, selectedRecords []record.Record, rank func(record.Snapshot) int) []record.Record {
	records := StripParkedWebRecords(domain, selectedRecords)
	if len(records) == len(selectedRecords) {
		return records
	}
	webKeys := map[string]bool{}
	for _, rec := range records {
		if key, ok := WebRecordKey(domain, rec); ok {
			webKeys[key] = true
		}
	}
	alternates := append([]record.Snapshot(nil), group...)
	sort.SliceStable(alternates, func(i, j int) bool {
		return rank(alternates[i]) < rank(alternates[j])
	})
	for _, snapshot := range alternates {
		if snapshot.Provider == selected.Provider && snapshot.ID == selected.ID {
			continue
		}
		for _, rec := range snapshot.Records {
			key, ok := WebRecordKey(domain, rec)
			if !ok || webKeys[key] || IsParkedWebRecord(domain, rec) {
				continue
			}
			records = append(records, rec)
			webKeys[key] = true
		}
	}
	return records
}

func StripParkedWebRecords(domain string, records []record.Record) []record.Record {
	out := make([]record.Record, 0, len(records))
	for _, rec := range records {
		if IsParkedWebRecord(domain, rec) {
			continue
		}
		out = append(out, rec)
	}
	return out
}

func IsParkedWebRecord(domain string, rec record.Record) bool {
	recType := strings.ToUpper(rec.Type)
	name := CloudflareName(domain, rec.Name)
	value := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(rec.Value)), ".")
	switch recType {
	case "A":
		return (name == "@" || name == "*") && value == "216.40.34.41"
	case "CNAME":
		return (name == "@" || name == "www") && value == "parkingpage.namecheap.com"
	default:
		return false
	}
}

func WebRecordKey(domain string, rec record.Record) (string, bool) {
	recType := strings.ToUpper(rec.Type)
	switch recType {
	case "A", "AAAA", "CNAME":
	default:
		return "", false
	}
	name := CloudflareName(domain, rec.Name)
	if name == "" || IsDefaultDNSOnlyName(name) || ContainsUnderscoreLabel(name) {
		return "", false
	}
	return recType + "\x00" + name, true
}

func IsDefaultDNSOnlyName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "autoconfig", "autodiscover", "email", "imap", "mail", "pop", "pop3", "smtp", "webmail":
		return true
	default:
		return false
	}
}

func ContainsUnderscoreLabel(name string) bool {
	for _, label := range strings.Split(name, ".") {
		if strings.HasPrefix(label, "_") {
			return true
		}
	}
	return false
}

func CloudflareName(domain, name string) string {
	name = strings.TrimSuffix(strings.TrimSpace(defaultName(name)), ".")
	if name == "@" {
		return "@"
	}
	normalizedName := normalizeDomain(name)
	normalizedDomain := normalizeDomain(domain)
	if normalizedName == normalizedDomain {
		return "@"
	}
	if strings.HasSuffix(normalizedName, "."+normalizedDomain) {
		return strings.TrimSuffix(normalizedName, "."+normalizedDomain)
	}
	return name
}

func defaultName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "@"
	}
	return name
}

func normalizeDomain(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}
