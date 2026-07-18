# Custom Endpoint

Custom Endpoint routes requests to a backend URL supplied by the operator.
Choose one concrete wire protocol for the target: Responses, Chat Completions,
or Messages, including the corresponding streaming variant when needed.

Model discovery is best-effort. Arbitrary endpoints are not required to expose
a compatible model-catalog operation, so configure the model explicitly when
discovery is unavailable.

The Custom Endpoint identity describes user-selected HTTP routing. It does not
promise that the backend is OpenAI-compatible.
