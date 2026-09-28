package observation

import domain "github.com/swobuforge/swobu/internal/domain/executionevidence"

// ExecutionEvidenceSink receives immutable lifecycle facts. Implementations
// must remain bounded and must not be allowed to influence request execution.
type ExecutionEvidenceSink interface {
	AppendExecutionEvidence(domain.Event) error
}
