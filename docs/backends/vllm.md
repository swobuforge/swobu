# vLLM backend

Start a generative vLLM inference server, then select vLLM in Cockpit. Swobu
uses `http://127.0.0.1:8000/v1` by default.

Choose a discovered generative model and an advertised protocol. Authentication
is optional; when configured, Swobu sends the credential as an
`Authorization: Bearer` token. Secure the vLLM server itself according to its
deployment documentation.
