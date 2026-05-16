// Package openaicompat wires one operator-declared OpenAI-compatible backend binding to the
// selected concrete coding-client protocol adapter.
//
// It owns base URL, credential application, transport execution, and backend
// error origin preservation. Concrete protocol mapping and continuity-sensitive
// request/response semantics must stay in the protocol adapter packages and app seam.
package openaicompat
