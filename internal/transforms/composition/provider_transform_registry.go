package composition

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/transform"
	"github.com/swobuforge/swobu/internal/transforms/documents/cacheaffinity"
	"github.com/swobuforge/swobu/internal/transforms/documents/strictjson"
	"github.com/swobuforge/swobu/internal/transforms/documents/tools"
	grammar "github.com/swobuforge/swobu/internal/transforms/events/grammar"
	usageevents "github.com/swobuforge/swobu/internal/transforms/streams/usage"
)

func NewProviderTransformRegistry(facts ProviderTransformFactRecord) transform.Registry {
	documentTransforms := make([]transform.DocumentTransform, 0, 3)
	if facts.CacheAffinityKey != "" || facts.CacheAffinityRetention != "" {
		documentTransforms = append(documentTransforms, cacheAffinityDocumentTransform{key: facts.CacheAffinityKey, retention: facts.CacheAffinityRetention})
	}
	if facts.NormalizeToolDeclarations {
		documentTransforms = append(documentTransforms, normalizeToolDeclarationsDocumentTransform{})
	}
	if len(facts.StrictJSONSupportedRequestField) > 0 {
		documentTransforms = append(documentTransforms, removeUnsupportedFieldsDocumentTransform{supported: facts.StrictJSONSupportedRequestField})
	}
	streamTransforms := make([]transform.EventStreamTransform, 0, 1)
	streamTransforms = append(streamTransforms, validateEnvelopeGrammarEventStreamTransform{})
	if facts.ReduceDuplicateUsageEvents {
		streamTransforms = append(streamTransforms, reduceDuplicateUsageReportsEventStreamTransform{})
	}
	return transform.NewRegistry(documentTransforms, streamTransforms)
}

type cacheAffinityDocumentTransform struct {
	key       string
	retention string
}

func (cacheAffinityDocumentTransform) ID() string             { return "documents.cache_affinity.apply" }
func (cacheAffinityDocumentTransform) Stage() transform.Stage { return transform.StageProviderWireOut }
func (cacheAffinityDocumentTransform) Match(transform.Context, carrier.WireDocument) bool {
	return true
}
func (t cacheAffinityDocumentTransform) Apply(_ transform.Context, doc carrier.WireDocument) (carrier.WireDocument, transform.Report, error) {
	next, result, err := cacheaffinity.Apply(doc, t.key, t.retention)
	if err != nil {
		return carrier.WireDocument{}, transform.Report{}, err
	}
	return next, transform.Report{Mutated: result.Mutated, Losses: result.Losses}, nil
}

type normalizeToolDeclarationsDocumentTransform struct{}

func (normalizeToolDeclarationsDocumentTransform) ID() string {
	return "documents.tools.normalize_declarations"
}
func (normalizeToolDeclarationsDocumentTransform) Stage() transform.Stage {
	return transform.StageProviderWireOut
}
func (normalizeToolDeclarationsDocumentTransform) Match(transform.Context, carrier.WireDocument) bool {
	return true
}
func (normalizeToolDeclarationsDocumentTransform) Apply(_ transform.Context, doc carrier.WireDocument) (carrier.WireDocument, transform.Report, error) {
	next, result, err := tools.NormalizeDeclarations(doc)
	if err != nil {
		return carrier.WireDocument{}, transform.Report{}, err
	}
	return next, transform.Report{Mutated: result.Mutated, Losses: result.Losses}, nil
}

type removeUnsupportedFieldsDocumentTransform struct{ supported map[string]struct{} }

func (removeUnsupportedFieldsDocumentTransform) ID() string {
	return "documents.strictjson.remove_unsupported_fields"
}
func (removeUnsupportedFieldsDocumentTransform) Stage() transform.Stage {
	return transform.StageProviderWireOut
}
func (t removeUnsupportedFieldsDocumentTransform) Match(transform.Context, carrier.WireDocument) bool {
	return len(t.supported) > 0
}
func (t removeUnsupportedFieldsDocumentTransform) Apply(_ transform.Context, doc carrier.WireDocument) (carrier.WireDocument, transform.Report, error) {
	next, result, err := strictjson.RemoveUnsupportedFields(doc, t.supported)
	if err != nil {
		return carrier.WireDocument{}, transform.Report{}, err
	}
	return next, transform.Report{Mutated: result.Mutated, Losses: result.Losses}, nil
}

type reduceDuplicateUsageReportsEventStreamTransform struct{}

func (reduceDuplicateUsageReportsEventStreamTransform) ID() string {
	return "streams.usage.reduce_duplicate_reports"
}
func (reduceDuplicateUsageReportsEventStreamTransform) Stage() transform.Stage {
	return transform.StageSemanticEvents
}
func (reduceDuplicateUsageReportsEventStreamTransform) Match(transform.Context, canonical.EventReader) bool {
	return true
}
func (reduceDuplicateUsageReportsEventStreamTransform) Wrap(_ transform.Context, reader canonical.EventReader) (canonical.EventReader, transform.Report, error) {
	return usageevents.Wrap(reader), transform.Report{Mutated: true}, nil
}

type validateEnvelopeGrammarEventStreamTransform struct{}

func (validateEnvelopeGrammarEventStreamTransform) ID() string {
	return "events.grammar.validate_envelope"
}
func (validateEnvelopeGrammarEventStreamTransform) Stage() transform.Stage {
	return transform.StageSemanticEvents
}
func (validateEnvelopeGrammarEventStreamTransform) Match(transform.Context, canonical.EventReader) bool {
	return true
}
func (validateEnvelopeGrammarEventStreamTransform) Wrap(_ transform.Context, reader canonical.EventReader) (canonical.EventReader, transform.Report, error) {
	return grammar.Wrap(reader), transform.Report{Mutated: false}, nil
}
