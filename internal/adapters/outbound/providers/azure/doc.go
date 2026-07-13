// Package azure wires the Azure provider ID to the shared OpenAI-compatible and
// Anthropic family kernels.
//
// It owns Azure project deployment discovery and adapter-edge protocol-family
// mapping from deployment metadata. The package must not infer provider family
// from SKU metadata or leak Azure-specific discovery rules into shared kernels.
package azure
