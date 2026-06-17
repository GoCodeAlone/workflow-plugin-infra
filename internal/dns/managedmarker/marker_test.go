package managedmarker

import "testing"

func TestAppendReplacesExistingManagedMarkers(t *testing.T) {
	records := []map[string]any{
		{"type": "A", "name": "@", "data": "192.0.2.10", "ttl": 300},
		{"type": "TXT", "name": Name, "data": `"heritage=wfinfra-v1 managed_by=wfctl state_dir=.state/old/ resource=cf-old"`, "ttl": 300},
		{"type": "txt", "name": Name + ".", "data": `"heritage=wfinfra-v1 managed_by=wfctl state_dir=.state/other/ resource=cf-other"`, "ttl": 300},
		{"type": "TXT", "name": Name + ".example.com", "data": `"heritage=wfinfra-v1 managed_by=wfctl state_dir=.state/provider/ resource=cf-example-com"`, "ttl": 300},
	}

	out := Append(records, ".state/new/", "cf-example-com")
	if len(out) != 2 {
		t.Fatalf("records len = %d, want A record plus one managed marker: %#v", len(out), out)
	}
	marker := out[1]
	if marker["type"] != Type || marker["name"] != Name {
		t.Fatalf("marker = %#v, want generated marker", marker)
	}
	if marker["data"] != `"heritage=wfinfra-v1 managed_by=wfctl state_dir=.state/new/ resource=cf-example-com"` {
		t.Fatalf("marker data = %#v", marker["data"])
	}
}
