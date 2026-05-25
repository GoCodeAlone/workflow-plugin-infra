package dnsprovider

import (
	"context"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/contracts"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
)

// Apply performs the actual DNS record mutation post-gate-pass.
// Returns a typed step result with status="ok"|"provider-error".
func Apply(ctx context.Context, a dnspolicy.Adapter, cfg *contracts.DNSRecordStepConfig, input *contracts.DNSRecordStepInput) (*sdk.TypedStepResult[*contracts.DNSRecordStepOutput], error) {
	var recordID string
	var err error
	op := input.Operation
	if op == "" {
		op = "upsert"
	}
	switch op {
	case "upsert":
		recordID, err = a.UpsertRecord(ctx, cfg.Zone, input.Name, input.RecordType, input.Data, input.Ttl, input.Priority)
	case "delete":
		err = a.DeleteRecord(ctx, cfg.Zone, input.Name, input.RecordType)
	default:
		return &sdk.TypedStepResult[*contracts.DNSRecordStepOutput]{
			Output: &contracts.DNSRecordStepOutput{Status: "provider-error", DenialReason: "unknown operation: " + op},
		}, nil
	}
	if err != nil {
		return &sdk.TypedStepResult[*contracts.DNSRecordStepOutput]{
			Output: &contracts.DNSRecordStepOutput{Status: "provider-error", DenialReason: err.Error()},
		}, nil
	}
	return &sdk.TypedStepResult[*contracts.DNSRecordStepOutput]{
		Output: &contracts.DNSRecordStepOutput{Status: "ok", RecordId: recordID},
	}, nil
}
