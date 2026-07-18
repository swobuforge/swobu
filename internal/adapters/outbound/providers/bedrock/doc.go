// Package bedrock wires the AWS Bedrock Mantle OpenAI-compatible endpoint
// behind one provider adapter edge.
//
// It owns AWS SigV4 signing, base URL and region resolution, credential-ref
// authentication selection (explicit API-key credential ref or AWS identity),
// protocol-path routing
// on the Mantle surface, transport execution, and backend error origin
// preservation.
package bedrock
