// Package mistral binds the direct Mistral Chat API to shared Chat
// Completions transport and grammar. It owns Mistral's documented ThinkChunk
// extraction and replay; its standard model catalog uses the shared
// authoring-only OpenAI catalog kernel. Model identity never selects request
// behavior.
package mistral
