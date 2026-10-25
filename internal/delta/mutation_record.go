package delta

// MutationRecord is the local wire-patch projection. It is deliberately
// independent of execution lifecycle evidence and can be extended without
// reopening the retired Traffic Evidence bounded context.
type MutationRecord struct {
	Stage         string   `json:"leg"`
	PatchID       string   `json:"patch_id"`
	Changed       bool     `json:"changed"`
	ChangedFields []string `json:"changed_fields,omitempty"`
}
