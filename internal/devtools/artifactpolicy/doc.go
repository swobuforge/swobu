// Package artifactpolicy defines Swobu's artifact-reference policy checker.
//
// It enforces that every wireframe artifact fixture in
// test/compatibility/surface/tui/testdata/wireframes is referenced by at least one
// non-artifact repo file, so fixture files cannot silently drift into dead or
// orphaned ground-truth lanes.
//
// It also enforces provenance for provider-response replay fixtures under
// test/compatibility/runtime/*/testdata and test/fixtures/responses_continuity:
// each fixture must have a sidecar provenance file that points to a mined
// live-matrix record field and must match that field verbatim.
package artifactpolicy
