// Package openrouter binds the OpenRouter provider ID to the shared OpenAI-family
// transport kernel and owns the provider's exact request dialects. Its codecs
// lower the reasoning object, replace the protocol web-search marker with
// openrouter:web_search, preserve reasoning_details as one opaque replay unit,
// and emit completed reasoning atomically without teaching shared protocol
// packages about OpenRouter.
package openrouter
