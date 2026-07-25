// Package producttelemetry owns local operator-facing product-telemetry state, the
// first-run notice, and the closed product-report path: the client and provider
// classifiers, the report reducer, and the JSON uploader.
//
// It defines enablement semantics for the CLI (`swobu telemetry status|on|off`)
// plus the DO_NOT_TRACK override and the pseudonymous installation id. It does
// not own traffic-evidence truth, request-path behavior, or provider
// adaptation. The uploader accepts only the closed ProductReport type — never
// raw evidence or arbitrary attributes (see product-telemetry.md).
package producttelemetry
