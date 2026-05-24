// Package bedrock wires AWS Bedrock runtime APIs behind one provider adapter
// edge.
//
// It owns AWS SigV4 signing, base URL and region resolution, native runtime
// operation routing (Converse and InvokeModel), transport execution, and
// backend error origin preservation.
package bedrock
