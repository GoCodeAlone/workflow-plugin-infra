package managedmarker

import (
	"fmt"
	"strings"
)

const (
	Name = "_workflow-dns-managed"
	Type = "TXT"
	TTL  = 300
)

func Append(records []map[string]any, stateDir, resource string) []map[string]any {
	out := make([]map[string]any, 0, len(records)+1)
	for _, record := range records {
		if isMarker(record) {
			continue
		}
		out = append(out, record)
	}
	out = append(out, Record(stateDir, resource))
	return out
}

func Record(stateDir, resource string) map[string]any {
	return map[string]any{
		"type": Type,
		"name": Name,
		"data": Data(stateDir, resource),
		"ttl":  TTL,
	}
}

func Data(stateDir, resource string) string {
	return fmt.Sprintf(`"heritage=wfinfra-v1 managed_by=wfctl state_dir=%s resource=%s"`, field(stateDir), field(resource))
}

func field(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, `"`, "")
	return value
}

func isMarker(record map[string]any) bool {
	recordType, _ := record["type"].(string)
	name, _ := record["name"].(string)
	if !strings.EqualFold(recordType, Type) {
		return false
	}
	normalizedName := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	return normalizedName == Name || strings.HasPrefix(normalizedName, Name+".")
}
