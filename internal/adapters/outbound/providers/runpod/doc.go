// Package runpod binds Runpod OpenAI-compatible inference to the shared
// OpenAI-family transport. Endpoint normalization and typed durable connection
// construction live at the profile/routing boundary; this package owns no
// Runpod control-plane or model-capability taxonomy.
package runpod
