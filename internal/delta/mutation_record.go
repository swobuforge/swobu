package delta

type MutationRecord struct {
	Stage         string   `json:"leg"`
	Transform     string   `json:"transform"`
	Changed       bool     `json:"changed"`
	ChangedFields []string `json:"changed_fields,omitempty"`
}
