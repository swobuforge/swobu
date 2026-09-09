// Package canonical defines Swobu request, response, tool, and content values.
//
// These values describe conversation content and execution intent, not live
// connections. Opaque schema occurrences carry a semantic contract profile and
// requested conformance strength; provider wire controls such as strict are
// resolved only at target lowering. Credentials, HTTP
// headers, and runtime sessions belong outside the canonical graph. Protocol
// DTOs and transport encoding remain in codecs. Backend errors distinguish
// recognized provider envelopes from opaque response bodies so diagnostics
// and client projection never grant arbitrary body text structured trust.
package canonical
