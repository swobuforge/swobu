package canonical

// SchemaConformance records the requested enforcement strength for one schema
// occurrence.
type SchemaConformance uint8

const (
	SchemaConformanceDefault SchemaConformance = iota
	SchemaConformanceRelaxed
	SchemaConformanceEnforced
)

// SchemaProfile identifies the schema-contract family governing one occurrence.
// It is semantic input to lowering, not a record of which HTTP endpoint carried
// the schema. Unprofiled means no contract-family equivalence is established.
type SchemaProfile uint8

const (
	SchemaProfileUnspecified SchemaProfile = iota
	SchemaProfileOpenAI
	SchemaProfileAnthropic
	SchemaProfileUnprofiled
)

// SchemaContract qualifies schema bytes owned by one canonical occurrence.
type SchemaContract struct {
	Profile     SchemaProfile
	Conformance SchemaConformance
}
