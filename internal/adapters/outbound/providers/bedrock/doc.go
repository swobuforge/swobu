// Package bedrock implements the AWS Bedrock provider adapter and SigV4 edge.
// Connection catalog checks omit STS enrichment, route credential-chain HTTP
// through the supplied guarded client, and bound selected credential_process
// output while suppressing stderr. SDK selection and source-profile role options
// are retained. Normal request authentication policy is unchanged.
package bedrock
