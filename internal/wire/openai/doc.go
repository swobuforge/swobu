// Package openaicompat owns shared OpenAI-style wire compatibility helpers.
//
// It centralizes content-part decoding, usage-compatibility emission, and
// other protocol-shape normalizers so family adapters can share one walker
// without collapsing family-specific error semantics.
package openai
