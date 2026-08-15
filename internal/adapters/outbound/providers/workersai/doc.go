// Package workersai binds Cloudflare-hosted Workers AI to shared Chat and
// Responses codecs. It owns the required default-gateway request header and
// the stable @cf/ provider-product identity boundary; it does not model
// per-model capabilities or Cloudflare AI Gateway Unified Billing.
package workersai
