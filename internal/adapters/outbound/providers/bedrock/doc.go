// Package bedrock wires the AWS Bedrock Mantle OpenAI-compatible endpoint
// behind one provider adapter edge.
//
// It owns AWS SigV4 signing, credential-ref
// authentication selection (explicit API-key credential ref or AWS identity),
// transport execution, and backend error origin preservation. Profile supplies
// required inference endpoint resolution and the independent regional catalog
// URL. The adapter consumes those facts and the authored signing region; it
// never maps model identity to a namespace or derives inference routing from
// catalog connectivity.
package bedrock
