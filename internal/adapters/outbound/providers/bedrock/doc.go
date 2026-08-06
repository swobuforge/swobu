// Package bedrock wires the AWS Bedrock Mantle OpenAI-compatible endpoint
// behind one provider adapter edge.
//
// It owns AWS SigV4 signing, credential-ref
// authentication selection (explicit API-key credential ref or AWS identity),
// transport execution, and backend error origin preservation. Profile supplies
// the single parsed endpoint resolution: normalized API base, exact request URL,
// and catalog URL. The adapter consumes that result and the independently
// authored signing region; it never reparses endpoint namespaces to choose a
// request path or infer signing scope.
package bedrock
