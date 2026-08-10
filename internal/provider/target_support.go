package provider

import "github.com/swobuforge/swobu/internal/domain/canonical"

// Support states what positive evidence establishes for one exact target.
// Supported means the target can honor the semantic. Unsupported means it
// cannot. Unknown means neither proposition has been established and is
// intentionally the zero value.
type Support uint8

const (
	SupportUnknown Support = iota
	SupportSupported
	SupportUnsupported
)

// TargetSupport is an immutable attempt-scoped snapshot of target knowledge.
// It contains knowledge, not preference, encoder availability, or what Swobu
// plans to try. It says whether a semantic can be honored, never how a codec
// represents it.
// Swobu's inability or decision not to use a native representation is not
// Unsupported; representation remains codec and recovery policy.
// NewTargetSupport copies its input so later evidence updates cannot mutate an
// in-flight provider request.
type TargetSupport struct {
	byCapability map[canonical.CapabilityPath]Support
}

func NewTargetSupport(values map[canonical.CapabilityPath]Support) TargetSupport {
	known := make(map[canonical.CapabilityPath]Support, len(values))
	for capability, support := range values {
		switch support {
		case SupportSupported, SupportUnsupported:
			known[capability] = support
		}
	}
	return TargetSupport{byCapability: known}
}

func (s TargetSupport) Get(capability canonical.CapabilityPath) Support {
	return s.byCapability[capability]
}

// TargetSupportResolver reads the current support snapshot for one exact
// target without network I/O. Active probing and evidence persistence happen
// outside the inference request path.
type TargetSupportResolver interface {
	ResolveTargetSupport(TargetSnapshot) TargetSupport
}

// TargetSupportFunc adapts provider-owned static contract knowledge to the
// resolver facet without introducing a shared capability matrix.
type TargetSupportFunc func(TargetSnapshot) TargetSupport

func (f TargetSupportFunc) ResolveTargetSupport(target TargetSnapshot) TargetSupport {
	return f(target)
}

// UnknownTargetSupport preserves absence of evidence for provider runtimes
// that establish no static target-support facts.
func UnknownTargetSupport(TargetSnapshot) TargetSupport { return TargetSupport{} }
