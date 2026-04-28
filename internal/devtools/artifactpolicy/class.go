package artifactpolicy

// Class declares one artifact family to enforce for reference reachability.
type Class struct {
	Name              string
	Root              string
	Extensions        []string
	PathContains      string
	ExcludeFileSuffix []string
	RequireProvenance bool
}

// DefaultClasses is the repository-default artifact policy scope.
func DefaultClasses() []Class {
	return []Class{
		{
			Name:       "wireframe artifact",
			Root:       wireframeRoot,
			Extensions: []string{".txt"},
		},
		{
			Name:              "provider response replay artifact",
			Root:              runtimeCompatibilityRoot,
			Extensions:        []string{".json", ".sse"},
			PathContains:      "/testdata/",
			ExcludeFileSuffix: []string{".provenance.json"},
			RequireProvenance: true,
		},
		{
			Name:              "provider continuity fixture artifact",
			Root:              responsesContinuityRoot,
			Extensions:        []string{".json", ".sse"},
			ExcludeFileSuffix: []string{".provenance.json"},
			RequireProvenance: true,
		},
	}
}
