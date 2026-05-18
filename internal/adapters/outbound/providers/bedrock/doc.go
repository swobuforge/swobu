// Package bedrock wires AWS Bedrock OpenAI-compatible endpoints behind one
// provider adapter edge.
//
// It owns AWS SigV4 signing, base URL and region resolution, transport
// execution, and backend error origin preservation. Core compatibility
// semantics and protocol wire encoding/decoding remain in shared protocol
// adapter packages.
package bedrock
