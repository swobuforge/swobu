// Package bedrock wires the AWS Bedrock Mantle OpenAI-compatible endpoint
// behind one provider adapter edge.
//
// It owns AWS SigV4 signing, base URL and region resolution, credential-ref
// auth mode selection (AWS profile or bearer-token env), protocol-path routing
// on the Mantle surface, transport execution, and backend error origin
// preservation.
package bedrock
