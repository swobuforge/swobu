# Swobu

![Swobu README hero](./assets/readme/swobu-readme-hero.png)

**Use AI capacity across providers, regions, accounts, and local models.**

Swobu gives each AI client one local endpoint. You can route requests across providers, regions, accounts, and local models. You do not have to change the client integration.

> **The client names the route. Swobu selects the target.**

Swobu is beta. Behavior can change between releases.

---

## Install

Install the release:

```sh
curl -fsSL https://swobu.com/install.sh | sh
```

The installer provides release binaries for macOS on Intel and Apple silicon, and for Linux on AMD64 and ARM64. It downloads `checksums.txt` and checks the SHA-256 of the archive before it installs.

If a release binary is not available for your platform, install from source:

```sh
go install github.com/swobuforge/swobu/cmd/swobu@master
```

Open Cockpit:

```sh
swobu
```

`swobu` attaches to the local daemon, or starts it, and then opens Cockpit.

Check the daemon:

```sh
swobu status
```

Stop the daemon:

```sh
swobu daemon down
```

You can leave the daemon running between Cockpit sessions.

---

## First route in Cockpit

Open Cockpit, then:

1. Add or select a workspace.
2. Add one route. The first route becomes the default route.
3. Add one target to the route.
4. Select a provider for the target.
5. Set the connection, the model, and the credential reference.
6. Note the workspace endpoint that Cockpit shows.

Each workspace has one default route. The model value `default` selects it. A different non-empty model value that does not match a route also selects it. An empty model value causes an error.

---

## Connect a client

A client needs two Swobu values:

1. The workspace endpoint.
2. The route name, sent as `model`.

Cockpit shows the workspace endpoint:

```text
http://127.0.0.1:7926/c/<workspace>
```

OpenAI-family clients use the workspace endpoint with `/v1`:

```text
http://127.0.0.1:7926/c/dev/v1
```

Anthropic-family clients use the workspace endpoint without `/v1`. They send requests to `/messages` and include an `anthropic-version` header.

If a client needs an API key field, use a non-secret placeholder such as `swobu`. Provider credentials stay in Swobu. Do not put a real provider credential in the client configuration.

For client setup instructions, read [`docs/clients/`](./docs/clients/).

---

## Demo

![Swobu Cockpit demo](./assets/readme/swobu-cli-demo.gif)

*Swobu Cockpit in a terminal.*

---

## Built with Codex and GPT-5.6

Swobu was reimplemented from the ground up during OpenAI Build Week using Codex with GPT-5.6. An earlier prototype established the idea, but none of its implementation remains.

During the hackathon, I rebuilt the daemon and Cockpit around a new architecture and a different TUI framework, added tiered routing and capability-aware fallback, and strengthened testing, linting, and visual verification.

Codex and GPT-5.6 did much of the implementation. I defined the product semantics, set the architectural rails, argued through the system designs in RFCs, and kept the result from drifting into unnecessary complexity.

---

## Why Swobu exists

The client and the model provider are different choices. A direct connection joins these choices together.

A client that connects directly to one provider has these limits:

- The models that the provider supplies
- The quota for one region or account
- The credits and contract terms for that provider
- The outages and throttling for that provider
- The protocol that the provider accepts
- The credentials that the client must hold.

This connection can leave useful capacity unused. It also makes each provider change a client change.

---

## The Swobu routing model

```text
workspace
  route        client-visible model name
    tier       primary, or ordered fallback
      target   provider + connection + model + protocol + credential
```

A workspace gives one client context its own endpoint. A route is the model name that the client sends. A tier is a set of targets at the same priority. A target is one concrete backend.

Swobu does not make providers or deployments the same. Model behavior, protocol support, limits, and errors can differ between targets. Swobu controls these differences at one boundary. Swobu does not hide them.

---

## What works today

These surfaces are the current focus.

### Tested clients

- Claude Code
- Codex CLI
- Continue
- OpenAI-family clients
- Anthropic-family clients

### Supported backends

- OpenAI
- Anthropic
- Azure AI
- AWS Bedrock
- OpenRouter
- Ollama
- ChatGPT
- Custom endpoints

### Request families

- OpenAI: `/chat/completions` and `/responses`, with their `/v1` forms
- Anthropic: `/messages`, with its `/v1` form
- Discovery: `/models`, with its `/v1` form (GET only)

### Streaming

- Server-Sent Events
- WebSocket on `/responses` only

### Known gaps

- Behavior varies by client and backend.
- Some clients need specific environment variables.
- Token and cache fields that providers report are not uniform.
- The release installer covers Linux and macOS on AMD64 and ARM64 only.

---

## Cockpit

Swobu includes Cockpit, a local terminal surface for setup and operation. Use Cockpit to:

- add, rename, and delete workspaces;
- add routes and set the default route;
- add targets and select their providers;
- configure connections and credential references;
- read readiness and the latest traffic outcome.

Cockpit is the primary local operator surface.

---

## Command surface

```sh
swobu                  # open Cockpit
swobu daemon           # start the daemon in the foreground
swobu status           # print daemon health
swobu daemon down      # stop the daemon
swobu telemetry status # print the telemetry setting
swobu telemetry on     # enable telemetry
swobu telemetry off    # disable telemetry
swobu version          # print the version
```

Help:

```sh
swobu --help
swobu daemon --help
swobu status --help
```

---

## Daemon and health

Start the daemon without Cockpit:

```sh
swobu daemon
```

Use an explicit configuration file:

```sh
swobu daemon --config /path/to/swobu.yaml
```

Check health from a script or from CI:

```sh
swobu status
```

The exit code is a machine-readable health signal:

| Exit code | Meaning |
| --- | --- |
| `0` | healthy |
| `1` | uninitialized or degraded |
| `2` | not reachable |

Stop the daemon:

```sh
swobu daemon down
```

---

## Run from source

Run the current `master` branch without an install:

```sh
go run github.com/swobuforge/swobu/cmd/swobu@master --help
```

Use this when you want current development behavior, not a release.

---

## What Swobu is

Swobu is:

- a local exchange layer;
- a protocol boundary;
- a client-to-backend boundary;
- a local operator surface (Cockpit);
- a way to change the backend behind an existing AI client.

## What Swobu is not

Swobu is not, today:

- an SDK;
- a hosted model marketplace;
- a new AI client;
- an observability platform;
- a prompt management system;
- a managed enterprise gateway.

---

## Security and privacy

Swobu is local-first. By default, Swobu:

- binds to the loopback address;
- keeps control traffic on your machine;
- does not send prompts, completions, or auth material through default telemetry.

Local-first is not offline-only. If you route to a hosted backend, the request still goes to that backend.

Turn telemetry off:

```sh
swobu telemetry off
```

For telemetry details, read [`docs/telemetry.md`](./docs/telemetry.md).

---

## Roadmap

Near-term work:

- deeper client and backend profiles;
- better configuration generation;
- clearer exchange diagnostics and error translation;
- stronger streaming support;
- safer local defaults;
- easier backend changes.

The goal: make it routine to route any supported AI client to the backend you choose.

---

## Contributing

We welcome contributions. Read [`CONTRIBUTING.md`](./CONTRIBUTING.md) before you open a pull request.

Swobu uses a Contributor License Agreement. When you submit a contribution, you agree to the terms in [`CLA.md`](./CLA.md). The CLA allows Swobu to maintain, sublicense, dual-license, and relicense contributions in the future. Contributors keep ownership of their contributions.

Swobu uses the AGPL-3.0-only license. It can also offer commercial licenses for teams that cannot use AGPL software. The CLA keeps that option open while the public repository stays open.

---

## Security

Do not report security vulnerabilities in public issues. Read [`SECURITY.md`](./SECURITY.md) before you report one.

---

## Commercial licensing

For commercial licenses and additional permissions, write to `contact@swobu.com`.

---

## License

Swobu uses the AGPL-3.0-only license. Read [`LICENSE`](./LICENSE).
