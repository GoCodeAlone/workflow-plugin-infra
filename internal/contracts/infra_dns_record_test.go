package contracts

import "testing"

func TestDNSRecordStepInputProtoExists(t *testing.T) {
	var _ = &DNSRecordStepInput{Name: "www", RecordType: "A", Owner: "multisite"}
	var _ = &DNSRecordStepConfig{Provider: "digitalocean", Zone: "z", ProviderCreds: map[string]string{"token": "x"}}
	var _ = &DNSRecordStepOutput{Status: "ok"}
}
