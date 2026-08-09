# Ollama backend

Run Ollama locally. Swobu defaults the Ollama connection to
`http://127.0.0.1:11434/v1` and supports Responses, Chat Completions, and
Messages in buffered and streaming forms. The complete six-variant contract
requires Ollama 0.14.0 or later; earlier releases do not provide the Messages
compatibility endpoint.

For compatibility issues, record model ID, request family, and streaming mode.
