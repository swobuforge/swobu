package delta

type MutationPathKind string

const (
	PathKindJSONPointer MutationPathKind = "json_pointer"
	PathKindFramePath   MutationPathKind = "frame_path"
	PathKindEventPath   MutationPathKind = "event_path"
	PathKindSemantic    MutationPathKind = "semantic_path"
	PathKindState       MutationPathKind = "state_path"
)

type MutationPathRecord struct {
	Kind MutationPathKind `json:"kind"`
	Expr string           `json:"expr"`
}
