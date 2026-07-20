// Package openrouter binds the OpenRouter provider ID to the shared OpenAI-family
// transport kernel and owns the provider's reasoning dialect. Its codec wrapper
// lowers the reasoning object, preserves reasoning_details as one opaque replay
// unit, and emits completed reasoning atomically without teaching the standard
// Chat Completions package about OpenRouter.
package openrouter
