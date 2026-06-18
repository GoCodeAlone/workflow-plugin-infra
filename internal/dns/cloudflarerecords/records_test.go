package cloudflarerecords

import (
	"testing"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/record"
)

func TestWebRecordKeyExcludesDNSOnlyAndUnderscoreNames(t *testing.T) {
	for _, rec := range []record.Record{
		{Type: "CNAME", Name: "mail", Value: "mail.example.net"},
		{Type: "CNAME", Name: "_acme-challenge", Value: "token.example.net"},
		{Type: "CNAME", Name: "protonmail._domainkey", Value: "key.example.net"},
	} {
		if key, ok := WebRecordKey("example.com", rec); ok {
			t.Fatalf("WebRecordKey(%+v) = (%q, true), want excluded", rec, key)
		}
	}
	if key, ok := WebRecordKey("example.com", record.Record{Type: "A", Name: "@", Value: "203.0.113.10"}); !ok || key == "" {
		t.Fatalf("apex A WebRecordKey = (%q, %v), want included", key, ok)
	}
}

func TestStripParkedWebRecordsKeepsMailRecords(t *testing.T) {
	records := StripParkedWebRecords("example.com", []record.Record{
		{Type: "A", Name: "@", Value: "216.40.34.41"},
		{Type: "CNAME", Name: "www", Value: "parkingpage.namecheap.com"},
		{Type: "CNAME", Name: "mail", Value: "mail.hover.com.cust.hostedemail.com"},
		{Type: "MX", Name: "@", Value: "10 mx.hover.com.cust.hostedemail.com"},
	})
	if len(records) != 2 {
		t.Fatalf("records = %#v, want only mail CNAME and MX", records)
	}
	for _, rec := range records {
		if rec.Type == "A" || rec.Name == "www" {
			t.Fatalf("parked web record kept: %#v", records)
		}
	}
}
