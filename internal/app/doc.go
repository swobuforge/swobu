// Package app is the namespace root for Swobu application-layer use cases.
//
// The real ownership lives in child packages:
//
// - `app/requestpath`: one compatibility request lifecycle
// - `app/operator/endpoints`: daemon-owned endpoint-intent control use cases
// - `app/operator/modelcatalog`: daemon-owned operator model-catalog query
//
// Inbound adapters call these use-case packages. The daemon process hosts them.
// They are neither UI packages nor runtime-container packages.
package app
